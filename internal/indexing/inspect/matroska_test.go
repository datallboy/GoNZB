package inspect

import (
	"bytes"
	"testing"

	"github.com/at-wat/ebml-go"
)

func TestParseMatroskaHeaderReturnsInfoAndTracksWithoutPayload(t *testing.T) {
	document := matroskaHeaderDocument{
		EBML: matroskaEBMLHeader{DocType: "matroska"},
		Segment: matroskaHeaderSegment{
			Info: matroskaHeaderInfo{
				TimecodeScale: 1_000_000,
				Duration:      1_420_017,
				Title:         "Example Feature",
			},
			Tracks: matroskaHeaderTracks{Entries: []matroskaHeaderTrack{
				{
					TrackNumber: 1,
					TrackType:   1,
					CodecID:     "V_MPEGH/ISO/HEVC",
					FlagDefault: 1,
					Language:    "jpn",
					Video:       &matroskaHeaderVideo{PixelWidth: 3840, PixelHeight: 2160},
				},
				{
					TrackNumber: 2,
					TrackType:   2,
					CodecID:     "A_OPUS",
					FlagDefault: 1,
					Language:    "eng",
					Audio:       &matroskaHeaderAudio{SamplingFrequency: 48000, Channels: 2},
				},
				{
					TrackNumber: 3,
					TrackType:   17,
					CodecID:     "S_TEXT/ASS",
					FlagForced:  1,
					Language:    "spa",
				},
			}},
		},
	}
	var prefix bytes.Buffer
	if err := ebml.Marshal(&document, &prefix); err != nil {
		t.Fatalf("marshal matroska header: %v", err)
	}

	result, err := ParseMatroskaHeader(prefix.Bytes(), 976_610_572)
	if err != nil {
		t.Fatalf("parse matroska header: %v", err)
	}
	if result.Format.Duration != "1420.017000" || result.Format.Tags["title"] != "Example Feature" {
		t.Fatalf("unexpected format facts: %+v", result.Format)
	}
	if len(result.Streams) != 3 {
		t.Fatalf("expected three tracks, got %+v", result.Streams)
	}
	video, audio, subtitle := result.Streams[0], result.Streams[1], result.Streams[2]
	if video.CodecType != "video" || video.CodecName != "hevc" || video.Width != 3840 || video.Height != 2160 || video.Disposition.Default != 1 {
		t.Fatalf("unexpected video track: %+v", video)
	}
	if audio.CodecType != "audio" || audio.CodecName != "opus" || audio.Channels != 2 || audio.SampleRate != "48000" || audio.Tags["language"] != "eng" {
		t.Fatalf("unexpected audio track: %+v", audio)
	}
	if subtitle.CodecType != "subtitle" || subtitle.CodecName != "ass" || subtitle.Tags["language"] != "spa" || subtitle.Disposition.Forced != 1 {
		t.Fatalf("unexpected subtitle track: %+v", subtitle)
	}
}

func TestParseMatroskaHeaderReaderStopsBeforeMediaPayload(t *testing.T) {
	document := matroskaHeaderDocument{
		EBML: matroskaEBMLHeader{DocType: "matroska"},
		Segment: matroskaHeaderSegment{
			Info: matroskaHeaderInfo{TimecodeScale: 1_000_000, Duration: 1_000},
			Tracks: matroskaHeaderTracks{Entries: []matroskaHeaderTrack{{
				TrackNumber: 1,
				TrackType:   1,
				CodecID:     "V_MPEG4/ISO/AVC",
				Video:       &matroskaHeaderVideo{PixelWidth: 1920, PixelHeight: 1080},
			}}},
		},
	}
	var header bytes.Buffer
	if err := ebml.Marshal(&document, &header); err != nil {
		t.Fatalf("marshal matroska header: %v", err)
	}
	payload := bytes.Repeat([]byte{0x55}, 1024*1024)
	input := append(append([]byte(nil), header.Bytes()...), payload...)
	reader := bytes.NewReader(input)

	if _, err := ParseMatroskaHeaderReader(reader, 0); err != nil {
		t.Fatalf("parse streaming matroska header: %v", err)
	}
	if consumed := len(input) - reader.Len(); consumed >= len(input) || reader.Len() < len(payload) {
		t.Fatalf("expected parser to stop before payload, consumed=%d remaining=%d", consumed, reader.Len())
	}
}

func TestParseMatroskaHeaderRejectsPrefixWithoutTracks(t *testing.T) {
	var prefix bytes.Buffer
	document := struct {
		Segment struct {
			Info matroskaHeaderInfo `ebml:"Info"`
		} `ebml:"Segment"`
	}{}
	if err := ebml.Marshal(&document, &prefix); err != nil {
		t.Fatalf("marshal trackless matroska header: %v", err)
	}
	if _, err := ParseMatroskaHeader(prefix.Bytes(), 1024); err == nil {
		t.Fatalf("expected missing-track error")
	}
}
