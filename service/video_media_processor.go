package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

var (
	ErrVideoMediaInvalid            = errors.New("invalid video media")
	ErrVideoMediaProcessingFailed   = errors.New("video media processing failed")
	ErrVideoMediaToolsNotConfigured = errors.New("video media tools are not pinned correctly")
)

type permanentVideoMediaError struct {
	err error
}

func (err permanentVideoMediaError) Error() string {
	return err.err.Error()
}

func (err permanentVideoMediaError) Unwrap() error {
	return err.err
}

func markPermanentVideoMediaError(err error) error {
	if err == nil {
		return nil
	}
	return permanentVideoMediaError{err: err}
}

func isPermanentVideoMediaError(err error) bool {
	var permanent permanentVideoMediaError
	return errors.As(err, &permanent)
}

const (
	videoFFmpegPathEnvironment          = "VIDEO_STUDIO_FFMPEG_PATH"
	videoFFprobePathEnvironment         = "VIDEO_STUDIO_FFPROBE_PATH"
	videoMediaVersionEnvironment        = "VIDEO_STUDIO_FFMPEG_VERSION"
	videoFFmpegDigestEnvironment        = "VIDEO_STUDIO_FFMPEG_SHA256"
	videoFFprobeDigestEnvironment       = "VIDEO_STUDIO_FFPROBE_SHA256"
	videoPosterMaximumBytes       int64 = 120 * 1024
	videoPreviewMaximumBytes      int64 = 20 << 20
	videoMediaMaximumStreams            = 8
	videoMediaMaximumDimension          = 8192
	videoMediaMaximumPixels             = 40_000_000
	videoMediaInspectTimeout            = 15 * time.Second
	videoMediaPosterTimeout             = 30 * time.Second
	videoMediaPreviewTimeout            = 60 * time.Second
	videoMediaVerificationTimeout       = 10 * time.Second
)

const videoMediaProtocolBlacklist = "http,https,tcp,tls,udp,rtp,concat,concatf,subfile,crypto,data,ftp,gopher,srt,rist,unix"
const videoMediaFormatWhitelist = "mov,mp4,m4a,3gp,3g2,mj2,matroska,webm,image2"

type VideoMediaMetadata struct {
	MIMEType        string
	Width           int
	Height          int
	DurationSeconds float64
	Codec           string
}

type VideoMediaProcessor interface {
	Inspect(context.Context, string) (VideoMediaMetadata, error)
	CreatePoster(context.Context, string, string, int64) error
	CreatePreview(context.Context, string, string) error
}

type videoMediaCommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execVideoMediaCommandRunner struct{}

type FFmpegVideoMediaProcessor struct {
	ffmpegPath     string
	ffprobePath    string
	runner         videoMediaCommandRunner
	inspectTimeout time.Duration
	posterTimeout  time.Duration
	previewTimeout time.Duration
}

func NewPinnedFFmpegVideoMediaProcessorFromEnvironment(ctx context.Context) (*FFmpegVideoMediaProcessor, error) {
	ffmpegPath, err := resolveVideoMediaTool(os.Getenv(videoFFmpegPathEnvironment), "ffmpeg")
	if err != nil {
		return nil, err
	}
	ffprobePath, err := resolveVideoMediaTool(os.Getenv(videoFFprobePathEnvironment), "ffprobe")
	if err != nil {
		return nil, err
	}
	version := strings.TrimSpace(os.Getenv(videoMediaVersionEnvironment))
	ffmpegDigest := strings.ToLower(strings.TrimSpace(os.Getenv(videoFFmpegDigestEnvironment)))
	ffprobeDigest := strings.ToLower(strings.TrimSpace(os.Getenv(videoFFprobeDigestEnvironment)))
	if version == "" || !validSHA256Hex(ffmpegDigest) || !validSHA256Hex(ffprobeDigest) {
		return nil, ErrVideoMediaToolsNotConfigured
	}
	runner := execVideoMediaCommandRunner{}
	if err := verifyPinnedVideoMediaTool(ctx, runner, ffmpegPath, version, ffmpegDigest); err != nil {
		return nil, err
	}
	if err := verifyPinnedVideoMediaTool(ctx, runner, ffprobePath, version, ffprobeDigest); err != nil {
		return nil, err
	}
	return &FFmpegVideoMediaProcessor{ffmpegPath: ffmpegPath, ffprobePath: ffprobePath, runner: runner}, nil
}

func (processor *FFmpegVideoMediaProcessor) Inspect(ctx context.Context, inputPath string) (VideoMediaMetadata, error) {
	if processor == nil || processor.runner == nil || processor.ffprobePath == "" || strings.TrimSpace(inputPath) == "" {
		return VideoMediaMetadata{}, ErrVideoMediaInvalid
	}
	inputPath, err := validateVideoMediaInputPath(inputPath)
	if err != nil {
		return VideoMediaMetadata{}, err
	}
	metadata, isRasterImage, err := inspectRasterVideoMedia(inputPath)
	if err != nil {
		return VideoMediaMetadata{}, err
	}
	if isRasterImage {
		return metadata, nil
	}
	commandContext, cancel := context.WithTimeout(ctx, videoMediaTimeout(processor.inspectTimeout, videoMediaInspectTimeout))
	defer cancel()
	arguments := []string{"-v", "error", "-show_streams", "-show_format", "-of", "json"}
	arguments = append(arguments, videoMediaInputArguments(inputPath)...)
	output, err := processor.runner.Run(commandContext, processor.ffprobePath, arguments...)
	if err != nil {
		return VideoMediaMetadata{}, videoMediaCommandError(commandContext, "ffprobe", err)
	}
	metadata, err = parseVideoProbeOutput(output)
	if err != nil {
		return VideoMediaMetadata{}, err
	}
	return metadata, nil
}

func (processor *FFmpegVideoMediaProcessor) CreatePoster(ctx context.Context, inputPath string, outputPath string, maxBytes int64) error {
	if processor == nil || processor.runner == nil || processor.ffmpegPath == "" || processor.ffprobePath == "" ||
		inputPath == "" || outputPath == "" || maxBytes <= 0 {
		return ErrVideoMediaInvalid
	}
	inputPath, outputPath, err := validateVideoMediaPaths(inputPath, outputPath)
	if err != nil {
		return err
	}
	attempts := []struct {
		width   int
		quality int
	}{{width: 960, quality: 5}, {width: 720, quality: 7}, {width: 480, quality: 9}}
	var lastCommandErr error
	for _, attempt := range attempts {
		_ = os.Remove(outputPath)
		commandContext, cancel := context.WithTimeout(ctx, videoMediaTimeout(processor.posterTimeout, videoMediaPosterTimeout))
		arguments := []string{"-y", "-nostdin", "-ss", "0.1"}
		arguments = append(arguments, videoMediaInputArguments(inputPath)...)
		arguments = append(arguments, "-map", "0:v:0", "-frames:v", "1", "-an", "-sn", "-dn", "-threads", "2",
			"-vf", fmt.Sprintf("scale='min(%d,iw)':-2", attempt.width),
			"-q:v", strconv.Itoa(attempt.quality), "-fs", strconv.FormatInt(maxBytes+1, 10), "-f", "image2", outputPath)
		_, err := processor.runner.Run(commandContext, processor.ffmpegPath, arguments...)
		commandErr := videoMediaCommandError(commandContext, "ffmpeg", err)
		commandContextErr := commandContext.Err()
		cancel()
		if err != nil {
			_ = os.Remove(outputPath)
			if commandContextErr != nil {
				return commandErr
			}
			lastCommandErr = commandErr
			continue
		}
		info, err := os.Stat(outputPath)
		if err != nil {
			_ = os.Remove(outputPath)
			return fmt.Errorf("inspect generated video poster: %w", err)
		}
		if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxBytes {
			continue
		}
		metadata, err := processor.Inspect(ctx, outputPath)
		if err != nil {
			if errors.Is(err, ErrVideoMediaInvalid) || isPermanentVideoMediaError(err) {
				continue
			}
			return err
		}
		if metadata.MIMEType == "image/jpeg" && metadata.Width <= attempt.width {
			return nil
		}
	}
	_ = os.Remove(outputPath)
	if lastCommandErr != nil {
		return lastCommandErr
	}
	return markPermanentVideoMediaError(fmt.Errorf("%w: generated poster violates media constraints", ErrVideoMediaProcessingFailed))
}

func (processor *FFmpegVideoMediaProcessor) CreatePreview(ctx context.Context, inputPath string, outputPath string) error {
	if processor == nil || processor.runner == nil || processor.ffmpegPath == "" || processor.ffprobePath == "" || inputPath == "" || outputPath == "" {
		return ErrVideoMediaInvalid
	}
	inputPath, outputPath, err := validateVideoMediaPaths(inputPath, outputPath)
	if err != nil {
		return err
	}
	_ = os.Remove(outputPath)
	commandContext, cancel := context.WithTimeout(ctx, videoMediaTimeout(processor.previewTimeout, videoMediaPreviewTimeout))
	defer cancel()
	arguments := []string{"-y", "-nostdin"}
	arguments = append(arguments, videoMediaInputArguments(inputPath)...)
	arguments = append(arguments, "-map", "0:v:0", "-t", "4", "-an", "-sn", "-dn", "-threads", "2",
		"-vf", "scale=640:640:force_original_aspect_ratio=decrease:force_divisible_by=2",
		"-r", "12", "-c:v", "libx264", "-preset", "veryfast", "-crf", "32",
		"-movflags", "+faststart", "-pix_fmt", "yuv420p", "-fs", strconv.FormatInt(videoPreviewMaximumBytes+1, 10),
		"-f", "mp4", outputPath)
	_, err = processor.runner.Run(commandContext, processor.ffmpegPath, arguments...)
	if err != nil {
		_ = os.Remove(outputPath)
		return videoMediaCommandError(commandContext, "ffmpeg", err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		_ = os.Remove(outputPath)
		return fmt.Errorf("inspect generated video preview: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > videoPreviewMaximumBytes {
		_ = os.Remove(outputPath)
		return markPermanentVideoMediaError(fmt.Errorf("%w: generated preview violates size constraints", ErrVideoMediaProcessingFailed))
	}
	metadata, err := processor.Inspect(ctx, outputPath)
	if err != nil {
		_ = os.Remove(outputPath)
		return err
	}
	if metadata.MIMEType != "video/mp4" || metadata.Codec != "h264" ||
		metadata.Width > 640 || metadata.Height > 640 || metadata.DurationSeconds > 4.25 {
		_ = os.Remove(outputPath)
		return markPermanentVideoMediaError(fmt.Errorf("%w: generated preview violates media constraints", ErrVideoMediaProcessingFailed))
	}
	return nil
}

func videoMediaCommandError(commandContext context.Context, tool string, err error) error {
	if err == nil {
		return nil
	}
	if contextErr := commandContext.Err(); contextErr != nil {
		return contextErr
	}
	return fmt.Errorf("%w: %s execution failed: %w", ErrVideoMediaProcessingFailed, tool, err)
}

type videoProbeOutput struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		Duration  string `json:"duration"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
	} `json:"format"`
}

func parseVideoProbeOutput(output []byte) (VideoMediaMetadata, error) {
	var probe videoProbeOutput
	if len(output) == 0 || common.Unmarshal(output, &probe) != nil || len(probe.Streams) == 0 || len(probe.Streams) > videoMediaMaximumStreams {
		return VideoMediaMetadata{}, ErrVideoMediaInvalid
	}
	videoStreams := 0
	audioStreams := 0
	var selected *struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		Duration  string `json:"duration"`
	}
	for index := range probe.Streams {
		stream := &probe.Streams[index]
		switch stream.CodecType {
		case "video":
			videoStreams++
			selected = stream
		case "audio":
			if !allowedVideoAudioCodec(stream.CodecName) {
				return VideoMediaMetadata{}, ErrVideoMediaInvalid
			}
			audioStreams++
		default:
			return VideoMediaMetadata{}, ErrVideoMediaInvalid
		}
	}
	if videoStreams != 1 || audioStreams > 1 || selected == nil {
		return VideoMediaMetadata{}, ErrVideoMediaInvalid
	}
	stream := *selected
	if !validVideoMediaDimensions(stream.Width, stream.Height) {
		return VideoMediaMetadata{}, ErrVideoMediaInvalid
	}
	duration := parseVideoMediaDuration(stream.Duration)
	if duration <= 0 {
		duration = parseVideoMediaDuration(probe.Format.Duration)
	}
	mimeType := videoMediaMIMEType(probe.Format.FormatName, stream.CodecName, duration)
	if mimeType == "" {
		return VideoMediaMetadata{}, ErrVideoMediaInvalid
	}
	if strings.HasPrefix(mimeType, "image/") {
		duration = 0
	}
	if strings.HasPrefix(mimeType, "video/") && (duration <= 0 || duration > relaycommon.MaxTaskDurationSeconds) {
		return VideoMediaMetadata{}, ErrVideoMediaInvalid
	}
	return VideoMediaMetadata{
		MIMEType: mimeType, Width: stream.Width, Height: stream.Height,
		DurationSeconds: duration, Codec: stream.CodecName,
	}, nil
}

func validVideoMediaDimensions(width int, height int) bool {
	if width <= 0 || height <= 0 || width > videoMediaMaximumDimension || height > videoMediaMaximumDimension {
		return false
	}
	return int64(width) <= videoMediaMaximumPixels/int64(height)
}

func parseVideoMediaDuration(value string) float64 {
	duration, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || !isFinite(duration) || duration < 0 {
		return 0
	}
	return duration
}

func videoMediaMIMEType(formatName string, codec string, duration float64) string {
	formatName = strings.ToLower(strings.TrimSpace(formatName))
	codec = strings.ToLower(codec)
	if formatName == "image2" {
		switch codec {
		case "mjpeg", "jpeg":
			return "image/jpeg"
		case "png":
			return "image/png"
		case "webp":
			return "image/webp"
		default:
			return ""
		}
	}
	if duration <= 0 {
		return ""
	}
	if formatName == "matroska,webm" || formatName == "webm,matroska" {
		switch codec {
		case "vp8", "vp9", "av1":
			return "video/webm"
		default:
			return ""
		}
	}
	if formatName == "mov,mp4,m4a,3gp,3g2,mj2" {
		switch codec {
		case "h264", "hevc", "av1", "mpeg4", "prores":
			return "video/mp4"
		}
	}
	return ""
}

func allowedVideoAudioCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "aac", "opus", "vorbis", "mp3":
		return true
	default:
		return false
	}
}

func validateVideoMediaInputPath(inputPath string) (string, error) {
	inputPath = filepath.Clean(strings.TrimSpace(inputPath))
	info, err := os.Lstat(inputPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return "", ErrVideoMediaInvalid
	}
	return inputPath, nil
}

func validateVideoMediaPaths(inputPath string, outputPath string) (string, string, error) {
	inputPath, err := validateVideoMediaInputPath(inputPath)
	if err != nil {
		return "", "", err
	}
	outputPath = filepath.Clean(strings.TrimSpace(outputPath))
	if outputPath == "." || outputPath == inputPath {
		return "", "", ErrVideoMediaInvalid
	}
	parent, err := os.Stat(filepath.Dir(outputPath))
	if err != nil || !parent.IsDir() {
		return "", "", ErrVideoMediaInvalid
	}
	if info, err := os.Lstat(outputPath); err == nil && !info.Mode().IsRegular() {
		return "", "", ErrVideoMediaInvalid
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", ErrVideoMediaInvalid
	}
	return inputPath, outputPath, nil
}

func videoMediaInputArguments(inputPath string) []string {
	return []string{
		"-protocol_whitelist", "file", "-protocol_blacklist", videoMediaProtocolBlacklist,
		"-format_whitelist", videoMediaFormatWhitelist, "-probesize", "16777216", "-analyzeduration", "10000000",
		"-i", inputPath,
	}
}

func videoMediaTimeout(configured time.Duration, fallback time.Duration) time.Duration {
	if configured > 0 {
		return configured
	}
	return fallback
}

func resolveVideoMediaTool(configured string, fallback string) (string, error) {
	path := strings.TrimSpace(configured)
	if path == "" {
		path = fallback
	}
	resolved, err := exec.LookPath(path)
	if err != nil {
		return "", ErrVideoMediaToolsNotConfigured
	}
	return filepath.Clean(resolved), nil
}

func verifyPinnedVideoMediaTool(ctx context.Context, runner videoMediaCommandRunner, path string, version string, expectedDigest string) error {
	digest, err := fileSHA256(path)
	if err != nil || !strings.EqualFold(digest, expectedDigest) {
		return ErrVideoMediaToolsNotConfigured
	}
	versionContext, cancelVersion := context.WithTimeout(ctx, videoMediaVerificationTimeout)
	versionOutput, err := runner.Run(versionContext, path, "-version")
	cancelVersion()
	if err != nil || !strings.Contains(string(versionOutput), version) {
		return ErrVideoMediaToolsNotConfigured
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func validSHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
