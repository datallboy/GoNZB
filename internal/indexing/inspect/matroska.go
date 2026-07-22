package inspect

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/at-wat/ebml-go"
)

const defaultMatroskaTimecodeScale = uint64(1_000_000)

type matroskaHeaderDocument struct {
	EBML    matroskaEBMLHeader    `ebml:"EBML"`
	Segment matroskaHeaderSegment `ebml:"Segment"`
}

type matroskaEBMLHeader struct {
	DocType string `ebml:"EBMLDocType,omitempty"`
}

type matroskaHeaderSegment struct {
	Info   matroskaHeaderInfo   `ebml:"Info"`
	Tracks matroskaHeaderTracks `ebml:"Tracks,stop"`
}

type matroskaHeaderInfo struct {
	TimecodeScale uint64  `ebml:"TimecodeScale,omitempty"`
	Duration      float64 `ebml:"Duration,omitempty"`
	Title         string  `ebml:"Title,omitempty"`
}

type matroskaHeaderTracks struct {
	Entries []matroskaHeaderTrack `ebml:"TrackEntry"`
}

type matroskaHeaderTrack struct {
	TrackNumber  uint64               `ebml:"TrackNumber"`
	TrackType    uint64               `ebml:"TrackType"`
	Name         string               `ebml:"Name,omitempty"`
	Language     string               `ebml:"Language,omitempty"`
	LanguageIETF string               `ebml:"LanguageIETF,omitempty"`
	CodecID      string               `ebml:"CodecID"`
	CodecName    string               `ebml:"CodecName,omitempty"`
	FlagDefault  uint64               `ebml:"FlagDefault,omitempty"`
	FlagForced   uint64               `ebml:"FlagForced,omitempty"`
	Video        *matroskaHeaderVideo `ebml:"Video,omitempty"`
	Audio        *matroskaHeaderAudio `ebml:"Audio,omitempty"`
}

type matroskaHeaderVideo struct {
	PixelWidth    uint64 `ebml:"PixelWidth"`
	PixelHeight   uint64 `ebml:"PixelHeight"`
	DisplayWidth  uint64 `ebml:"DisplayWidth,omitempty"`
	DisplayHeight uint64 `ebml:"DisplayHeight,omitempty"`
}

type matroskaHeaderAudio struct {
	SamplingFrequency       float64 `ebml:"SamplingFrequency,omitempty"`
	OutputSamplingFrequency float64 `ebml:"OutputSamplingFrequency,omitempty"`
	Channels                uint64  `ebml:"Channels,omitempty"`
	BitDepth                uint64  `ebml:"BitDepth,omitempty"`
}

// ParseMatroskaHeader reads only the EBML header, segment info, and track
// definitions. The Tracks stop tag prevents payload clusters from being read.
func ParseMatroskaHeader(prefix []byte, exactSize int64) (*FFProbeResult, error) {
	if len(prefix) == 0 {
		return nil, fmt.Errorf("matroska prefix is empty")
	}

	var document matroskaHeaderDocument
	err := ebml.Unmarshal(bytes.NewReader(prefix), &document, ebml.WithIgnoreUnknown(true))
	if err != nil && !errors.Is(err, ebml.ErrReadStopped) {
		return nil, fmt.Errorf("parse matroska header: %w", err)
	}
	if len(document.Segment.Tracks.Entries) == 0 {
		return nil, fmt.Errorf("matroska prefix contains no track definitions")
	}

	info := document.Segment.Info
	timecodeScale := info.TimecodeScale
	if timecodeScale == 0 {
		timecodeScale = defaultMatroskaTimecodeScale
	}
	durationSeconds := info.Duration * float64(timecodeScale) / 1_000_000_000
	duration := ""
	if durationSeconds > 0 {
		duration = strconv.FormatFloat(durationSeconds, 'f', 6, 64)
	}

	result := &FFProbeResult{
		Format: FFProbeFormat{
			FormatName:     "matroska,webm",
			FormatLongName: "Matroska / WebM",
			Duration:       duration,
			ProbeScore:     100,
		},
		Streams: make([]FFProbeStream, 0, len(document.Segment.Tracks.Entries)),
	}
	if exactSize > 0 {
		result.Format.Size = strconv.FormatInt(exactSize, 10)
	}
	if durationSeconds > 0 && exactSize > 0 {
		result.Format.BitRate = strconv.FormatInt(int64(float64(exactSize*8)/durationSeconds), 10)
	}
	if title := strings.TrimSpace(info.Title); title != "" {
		result.Format.Tags = map[string]string{"title": title}
	}

	for index, track := range document.Segment.Tracks.Entries {
		stream := FFProbeStream{
			Index:     index,
			CodecType: matroskaTrackType(track.TrackType),
			CodecName: matroskaCodecName(track.CodecID),
			CodecLong: firstNonEmptyString(strings.TrimSpace(track.CodecName), strings.TrimSpace(track.CodecID)),
			Duration:  duration,
		}
		stream.Disposition.Default = int(track.FlagDefault)
		stream.Disposition.Forced = int(track.FlagForced)
		language := strings.TrimSpace(firstNonEmptyString(track.LanguageIETF, track.Language))
		if language != "" && !strings.EqualFold(language, "und") {
			stream.Tags = map[string]string{"language": language}
		}
		if name := strings.TrimSpace(track.Name); name != "" {
			if stream.Tags == nil {
				stream.Tags = make(map[string]string)
			}
			stream.Tags["title"] = name
		}
		if track.Video != nil {
			stream.Width = int(track.Video.PixelWidth)
			stream.Height = int(track.Video.PixelHeight)
		}
		if track.Audio != nil {
			stream.Channels = int(track.Audio.Channels)
			stream.SampleRate = strconv.FormatFloat(track.Audio.SamplingFrequency, 'f', -1, 64)
		}
		result.Streams = append(result.Streams, stream)
	}

	return result, nil
}

func matroskaTrackType(trackType uint64) string {
	switch trackType {
	case 1:
		return "video"
	case 2:
		return "audio"
	case 17:
		return "subtitle"
	default:
		return "data"
	}
}

func matroskaCodecName(codecID string) string {
	switch strings.ToUpper(strings.TrimSpace(codecID)) {
	case "V_MPEGH/ISO/HEVC":
		return "hevc"
	case "V_MPEG4/ISO/AVC":
		return "h264"
	case "V_AV1":
		return "av1"
	case "V_VP9":
		return "vp9"
	case "V_VP8":
		return "vp8"
	case "A_OPUS":
		return "opus"
	case "A_AAC":
		return "aac"
	case "A_AC3":
		return "ac3"
	case "A_EAC3":
		return "eac3"
	case "A_DTS":
		return "dts"
	case "A_FLAC":
		return "flac"
	case "A_VORBIS":
		return "vorbis"
	case "S_TEXT/ASS", "S_TEXT/SSA":
		return "ass"
	case "S_TEXT/UTF8":
		return "subrip"
	case "S_HDMV/PGS":
		return "hdmv_pgs_subtitle"
	default:
		return strings.ToLower(strings.TrimSpace(codecID))
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
