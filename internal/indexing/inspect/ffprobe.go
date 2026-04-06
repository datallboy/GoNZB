package inspect

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type FFProbeResult struct {
	Format  FFProbeFormat   `json:"format"`
	Streams []FFProbeStream `json:"streams"`
}

type FFProbeFormat struct {
	Filename       string            `json:"filename"`
	FormatName     string            `json:"format_name"`
	FormatLongName string            `json:"format_long_name"`
	Duration       string            `json:"duration"`
	BitRate        string            `json:"bit_rate"`
	Size           string            `json:"size"`
	ProbeScore     int               `json:"probe_score"`
	Tags           map[string]string `json:"tags"`
}

type FFProbeStream struct {
	Index              int               `json:"index"`
	CodecType          string            `json:"codec_type"`
	CodecName          string            `json:"codec_name"`
	CodecLong          string            `json:"codec_long_name"`
	CodecTagString     string            `json:"codec_tag_string"`
	CodecTag           string            `json:"codec_tag"`
	Profile            string            `json:"profile"`
	Width              int               `json:"width"`
	Height             int               `json:"height"`
	Channels           int               `json:"channels"`
	SampleRate         string            `json:"sample_rate"`
	ChannelLayout      string            `json:"channel_layout"`
	SampleFormat       string            `json:"sample_fmt"`
	BitsPerSample      int               `json:"bits_per_sample"`
	PixFmt             string            `json:"pix_fmt"`
	DisplayAspectRatio string            `json:"display_aspect_ratio"`
	RFrameRate         string            `json:"r_frame_rate"`
	AvgFrameRate       string            `json:"avg_frame_rate"`
	Duration           string            `json:"duration"`
	BitRate            string            `json:"bit_rate"`
	Tags               map[string]string `json:"tags"`
	Disposition        struct {
		Default int `json:"default"`
		Forced  int `json:"forced"`
	} `json:"disposition"`
}

func RunFFProbe(ctx context.Context, runner CommandRunner, ffprobePath, targetPath string) (*FFProbeResult, []byte, error) {
	if runner == nil {
		return nil, nil, fmt.Errorf("ffprobe runner is required")
	}
	ffprobePath = strings.TrimSpace(ffprobePath)
	if ffprobePath == "" {
		return nil, nil, fmt.Errorf("ffprobe path is required")
	}

	output, err := runner.Run(
		ctx,
		ffprobePath,
		"-v", "error",
		"-show_streams",
		"-show_format",
		"-print_format", "json",
		targetPath,
	)
	var parsed FFProbeResult
	if payload := extractFFProbeJSON(output); len(payload) > 0 {
		if parseErr := json.Unmarshal(payload, &parsed); parseErr == nil {
			return &parsed, output, err
		}
	}
	if err != nil {
		return nil, output, err
	}
	if err := json.Unmarshal(output, &parsed); err != nil {
		return nil, output, fmt.Errorf("decode ffprobe output: %w", err)
	}
	return &parsed, output, nil
}

func extractFFProbeJSON(output []byte) []byte {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return nil
	}
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start < 0 || end < start {
		return nil
	}
	return []byte(text[start : end+1])
}

func ParseSeconds(value string) float64 {
	if strings.TrimSpace(value) == "" {
		return 0
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0
	}
	return f
}

func ParseInt64(value string) int64 {
	if strings.TrimSpace(value) == "" {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
