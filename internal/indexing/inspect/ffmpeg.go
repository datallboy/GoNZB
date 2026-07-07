package inspect

import (
	"context"
	"fmt"
	"io"
	"strings"
)

func RunFFmpegThumbnail(ctx context.Context, runner CommandRunner, ffmpegPath, targetPath string) ([]byte, error) {
	if runner == nil {
		return nil, fmt.Errorf("ffmpeg runner is required")
	}
	ffmpegPath = strings.TrimSpace(ffmpegPath)
	if ffmpegPath == "" {
		return nil, fmt.Errorf("ffmpeg path is required")
	}
	return runner.Run(
		ctx,
		ffmpegPath,
		"-v", "error",
		"-ss", "00:00:01",
		"-i", targetPath,
		"-frames:v", "1",
		"-vf", "scale=640:-2:force_original_aspect_ratio=decrease",
		"-f", "image2pipe",
		"-vcodec", "mjpeg",
		"pipe:1",
	)
}

func RunFFmpegThumbnailInput(ctx context.Context, runner CommandRunner, ffmpegPath string, input io.Reader) ([]byte, error) {
	if runner == nil {
		return nil, fmt.Errorf("ffmpeg runner is required")
	}
	ffmpegPath = strings.TrimSpace(ffmpegPath)
	if ffmpegPath == "" {
		return nil, fmt.Errorf("ffmpeg path is required")
	}
	return runner.RunInput(
		ctx,
		input,
		ffmpegPath,
		"-v", "error",
		"-i", "pipe:0",
		"-frames:v", "1",
		"-vf", "scale=640:-2:force_original_aspect_ratio=decrease",
		"-f", "image2pipe",
		"-vcodec", "mjpeg",
		"pipe:1",
	)
}
