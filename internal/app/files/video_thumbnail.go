package files

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	videoThumbnailTimeout       = 5 * time.Second
	videoThumbnailMaxInputBytes = 200 << 20 // 200MB；更大文件本阶段跳过 fallback，避免阻塞发送。
	videoThumbnailMaxConcurrent = 2
	documentVideoThumbMaxEdge   = 320
)

// VideoThumbnailer 从视频字节中抽取静态缩略图。实现必须可失败降级，不影响原发送流程。
// maxEdge 是输出最长边的像素上限；≤0 时按 documentVideoThumbMaxEdge 处理。
type VideoThumbnailer interface {
	Extract(ctx context.Context, data []byte, mimeType string, maxEdge int) ([]byte, error)
}

// VideoDimensionProber 可选能力：探测视频原始宽高（用于 animated profile video 元数据）。
type VideoDimensionProber interface {
	ProbeDimensions(ctx context.Context, data []byte, mimeType string) (w, h int, err error)
}

// FFmpegVideoThumbnailer 使用本机 ffmpeg 抽取第一帧 JPEG。
type FFmpegVideoThumbnailer struct {
	path    string
	ffprobe string
	timeout time.Duration
	slots   chan struct{}
}

// NewFFmpegVideoThumbnailer 返回基于 PATH 中 ffmpeg/ffprobe 的抽帧器。
func NewFFmpegVideoThumbnailer() (*FFmpegVideoThumbnailer, error) {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, err
	}
	ffprobeName := "ffprobe"
	if ext := filepath.Ext(path); ext != "" {
		ffprobeName += ext
	}
	ffprobe := filepath.Join(filepath.Dir(path), ffprobeName)
	if _, err := os.Stat(ffprobe); err != nil {
		ffprobe, err = exec.LookPath("ffprobe")
		if err != nil {
			return nil, err
		}
	}
	return &FFmpegVideoThumbnailer{
		path:    path,
		ffprobe: ffprobe,
		timeout: videoThumbnailTimeout,
		slots:   make(chan struct{}, videoThumbnailMaxConcurrent),
	}, nil
}

// Extract 抽取第一帧并输出 JPEG bytes。
func (t *FFmpegVideoThumbnailer) Extract(ctx context.Context, data []byte, mimeType string, maxEdge int) ([]byte, error) {
	if t == nil || t.path == "" {
		return nil, fmt.Errorf("ffmpeg thumbnailer unavailable")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty video data")
	}
	if len(data) > videoThumbnailMaxInputBytes {
		return nil, fmt.Errorf("video too large for thumbnail fallback: %d bytes", len(data))
	}
	if maxEdge <= 0 {
		maxEdge = documentVideoThumbMaxEdge
	}
	select {
	case t.slots <- struct{}{}:
		defer func() { <-t.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	runCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	inputPath, cleanup, err := writeVideoTempFile(data, mimeType)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	output, err := os.CreateTemp("", "telesrv-video-thumb-*.jpg")
	if err != nil {
		return nil, fmt.Errorf("create temp thumbnail: %w", err)
	}
	outputPath := output.Name()
	output.Close()
	defer os.Remove(outputPath)

	scale := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease", maxEdge, maxEdge)
	cmd := exec.CommandContext(
		runCtx,
		t.path,
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", inputPath,
		"-map", "0:v:0",
		"-frames:v", "1",
		"-an",
		"-vf", scale,
		"-q:v", "3",
		outputPath,
	)
	stderr, err := cmd.CombinedOutput()
	if runCtx.Err() != nil {
		return nil, runCtx.Err()
	}
	if err != nil {
		msg := strings.TrimSpace(string(stderr))
		if msg != "" {
			return nil, fmt.Errorf("ffmpeg extract thumbnail: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("ffmpeg extract thumbnail: %w", err)
	}
	thumb, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("read thumbnail: %w", err)
	}
	if len(thumb) == 0 {
		return nil, fmt.Errorf("ffmpeg produced empty thumbnail")
	}
	return thumb, nil
}

// ProbeDimensions 返回视频第一路视频流的原始宽高。
func (t *FFmpegVideoThumbnailer) ProbeDimensions(ctx context.Context, data []byte, mimeType string) (int, int, error) {
	if t == nil || t.ffprobe == "" {
		return 0, 0, fmt.Errorf("ffprobe unavailable")
	}
	if len(data) == 0 {
		return 0, 0, fmt.Errorf("empty video data")
	}
	if len(data) > videoThumbnailMaxInputBytes {
		return 0, 0, fmt.Errorf("video too large for dimension probe: %d bytes", len(data))
	}
	select {
	case t.slots <- struct{}{}:
		defer func() { <-t.slots }()
	case <-ctx.Done():
		return 0, 0, ctx.Err()
	}

	runCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	inputPath, cleanup, err := writeVideoTempFile(data, mimeType)
	if err != nil {
		return 0, 0, err
	}
	defer cleanup()

	cmd := exec.CommandContext(runCtx, t.ffprobe,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "json",
		inputPath,
	)
	out, err := cmd.CombinedOutput()
	if runCtx.Err() != nil {
		return 0, 0, runCtx.Err()
	}
	if err != nil {
		return 0, 0, commandError("ffprobe video dimensions", err, out)
	}
	var result struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &result); err != nil || len(result.Streams) != 1 {
		return 0, 0, fmt.Errorf("invalid ffprobe video metadata")
	}
	stream := result.Streams[0]
	if stream.Width <= 0 || stream.Height <= 0 {
		return 0, 0, fmt.Errorf("incomplete video dimensions")
	}
	return stream.Width, stream.Height, nil
}

func writeVideoTempFile(data []byte, mimeType string) (path string, cleanup func(), err error) {
	input, err := os.CreateTemp("", "telesrv-video-*"+videoTempExt(mimeType))
	if err != nil {
		return "", nil, fmt.Errorf("create temp video: %w", err)
	}
	inputPath := input.Name()
	if _, err := input.Write(data); err != nil {
		input.Close()
		os.Remove(inputPath)
		return "", nil, fmt.Errorf("write temp video: %w", err)
	}
	if err := input.Close(); err != nil {
		os.Remove(inputPath)
		return "", nil, fmt.Errorf("close temp video: %w", err)
	}
	return inputPath, func() { os.Remove(inputPath) }, nil
}

func videoTempExt(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "video/mp4":
		return ".mp4"
	case "video/quicktime":
		return ".mov"
	case "video/webm":
		return ".webm"
	default:
		return ".bin"
	}
}

func parsePositiveFloat(value string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || v <= 0 {
		return 0
	}
	return v
}
