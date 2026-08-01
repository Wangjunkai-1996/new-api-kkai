package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type videoMediaRunner func(context.Context, string, ...string) ([]byte, error)

func (runner videoMediaRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return runner(ctx, name, args...)
}

func TestFFmpegVideoMediaProcessorInspectsVideoMetadata(t *testing.T) {
	input := filepath.Join(t.TempDir(), "source.mp4")
	require.NoError(t, os.WriteFile(input, []byte("not-a-real-video"), 0o600))
	processor := &FFmpegVideoMediaProcessor{
		ffprobePath: "ffprobe",
		runner: videoMediaRunner(func(_ context.Context, name string, _ ...string) ([]byte, error) {
			require.Equal(t, "ffprobe", name)
			return []byte(`{
				"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"duration":"5.25"}],
				"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"5.25"}
			}`), nil
		}),
	}

	metadata, err := processor.Inspect(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, "video/mp4", metadata.MIMEType)
	require.Equal(t, 1920, metadata.Width)
	require.Equal(t, 1080, metadata.Height)
	require.Equal(t, "h264", metadata.Codec)
	require.Equal(t, 5.25, metadata.DurationSeconds)
}

func TestFFmpegVideoMediaProcessorRestrictsProbeInputAndSetsDeadline(t *testing.T) {
	input := filepath.Join(t.TempDir(), "source.mp4")
	require.NoError(t, os.WriteFile(input, []byte("video"), 0o600))
	var arguments []string
	hasDeadline := false
	processor := &FFmpegVideoMediaProcessor{
		ffprobePath: "ffprobe",
		runner: videoMediaRunner(func(ctx context.Context, _ string, args ...string) ([]byte, error) {
			_, hasDeadline = ctx.Deadline()
			arguments = append([]string(nil), args...)
			return validVideoProbeOutput(), nil
		}),
	}

	_, err := processor.Inspect(context.Background(), input)
	require.NoError(t, err)
	require.True(t, hasDeadline)
	joined := strings.Join(arguments, " ")
	require.NotContains(t, joined, "-nostdin")
	require.Contains(t, joined, "-protocol_whitelist file")
	require.Contains(t, joined, "-protocol_blacklist")
	require.Contains(t, joined, "http")
	require.Contains(t, joined, "concat")
	require.Contains(t, joined, "-format_whitelist")
	require.NotContains(t, joined, "hls,")
}

func TestFFmpegVideoMediaProcessorKeepsFFmpegNonInteractive(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "source.mp4")
	output := filepath.Join(tempDir, "preview.mp4")
	require.NoError(t, os.WriteFile(input, []byte("source"), 0o600))
	processor := &FFmpegVideoMediaProcessor{
		ffmpegPath: "ffmpeg", ffprobePath: "ffprobe",
		runner: videoMediaRunner(func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "ffprobe" {
				return []byte(`{"streams":[{"codec_type":"video","codec_name":"h264","width":640,"height":360,"duration":"4"}],"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"4"}}`), nil
			}
			require.Contains(t, args, "-nostdin")
			return nil, os.WriteFile(args[len(args)-1], []byte("preview"), 0o600)
		}),
	}

	require.NoError(t, processor.CreatePreview(context.Background(), input, output))
}

func TestFFmpegVideoMediaProcessorRejectsSymlinkInputBeforeCommand(t *testing.T) {
	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "target.mp4")
	link := filepath.Join(tempDir, "link.mp4")
	require.NoError(t, os.WriteFile(target, []byte("video"), 0o600))
	require.NoError(t, os.Symlink(target, link))
	called := false
	processor := &FFmpegVideoMediaProcessor{
		ffprobePath: "ffprobe",
		runner: videoMediaRunner(func(context.Context, string, ...string) ([]byte, error) {
			called = true
			return validVideoProbeOutput(), nil
		}),
	}

	_, err := processor.Inspect(context.Background(), link)
	require.ErrorIs(t, err, ErrVideoMediaInvalid)
	require.False(t, called)
}

func TestParseVideoProbeOutputRejectsResourceAndFormatAbuse(t *testing.T) {
	tests := []string{
		`{"streams":[{"codec_type":"video","codec_name":"h264","width":9000,"height":1080,"duration":"5"}],"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"5"}}`,
		`{"streams":[{"codec_type":"video","codec_name":"h264","width":8192,"height":8192,"duration":"5"}],"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"5"}}`,
		`{"streams":[{"codec_type":"video","codec_name":"h264","width":1280,"height":720,"duration":"5"},{"codec_type":"video","codec_name":"h264","width":1280,"height":720,"duration":"5"}],"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"5"}}`,
		`{"streams":[{"codec_type":"video","codec_name":"h264","width":1280,"height":720,"duration":"5"},{"codec_type":"audio"},{"codec_type":"audio"},{"codec_type":"audio"},{"codec_type":"audio"},{"codec_type":"audio"},{"codec_type":"audio"},{"codec_type":"audio"},{"codec_type":"audio"}],"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"5"}}`,
		`{"streams":[{"codec_type":"video","codec_name":"mpeg2video","width":1280,"height":720,"duration":"5"}],"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"5"}}`,
		`{"streams":[{"codec_type":"video","codec_name":"h264","width":1280,"height":720,"duration":"5"},{"codec_type":"audio","codec_name":"unknown"}],"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"5"}}`,
		`{"streams":[{"codec_type":"video","codec_name":"h264","width":1280,"height":720,"duration":"5"}],"format":{"format_name":"hls","duration":"5"}}`,
	}
	for _, output := range tests {
		_, err := parseVideoProbeOutput([]byte(output))
		require.ErrorIs(t, err, ErrVideoMediaInvalid)
	}
}

func TestParseVideoProbeOutputAcceptsSingleImageFrameDuration(t *testing.T) {
	metadata, err := parseVideoProbeOutput([]byte(`{
		"streams":[{"codec_type":"video","codec_name":"mjpeg","width":960,"height":540,"duration":"0.040000"}],
		"format":{"format_name":"image2","duration":"0.040000"}
	}`))

	require.NoError(t, err)
	require.Equal(t, "image/jpeg", metadata.MIMEType)
	require.Equal(t, "mjpeg", metadata.Codec)
	require.Equal(t, 960, metadata.Width)
	require.Equal(t, 540, metadata.Height)
	require.Zero(t, metadata.DurationSeconds)
}

func TestFFmpegVideoMediaProcessorCommandDeadlineStopsHungRunner(t *testing.T) {
	input := filepath.Join(t.TempDir(), "source.mp4")
	require.NoError(t, os.WriteFile(input, []byte("video"), 0o600))
	processor := &FFmpegVideoMediaProcessor{
		ffprobePath:    "ffprobe",
		inspectTimeout: 25 * time.Millisecond,
		runner: videoMediaRunner(func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}),
	}
	started := time.Now()
	_, err := processor.Inspect(context.Background(), input)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, isPermanentVideoAssetError(err))
	require.Less(t, time.Since(started), time.Second)
}

func TestFFmpegVideoMediaProcessorPreservesRetryableCommandFailures(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "source.mp4")
	require.NoError(t, os.WriteFile(input, []byte("source"), 0o600))
	commandErr := errors.New("temporary process failure")
	processor := &FFmpegVideoMediaProcessor{
		ffmpegPath: "ffmpeg", ffprobePath: "ffprobe",
		runner: videoMediaRunner(func(context.Context, string, ...string) ([]byte, error) {
			return nil, commandErr
		}),
	}
	tests := []struct {
		name string
		run  func() error
	}{
		{name: "inspect", run: func() error {
			_, err := processor.Inspect(context.Background(), input)
			return err
		}},
		{name: "poster", run: func() error {
			return processor.CreatePoster(context.Background(), input, filepath.Join(tempDir, "poster.jpg"), videoPosterMaximumBytes)
		}},
		{name: "preview", run: func() error {
			return processor.CreatePreview(context.Background(), input, filepath.Join(tempDir, "preview.mp4"))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.run()
			require.ErrorIs(t, err, commandErr)
			require.ErrorIs(t, err, ErrVideoMediaProcessingFailed)
			require.False(t, isPermanentVideoAssetError(err))
		})
	}
}

func TestFFmpegVideoMediaProcessorMarksInvalidGeneratedPreviewPermanent(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "source.mp4")
	output := filepath.Join(tempDir, "preview.mp4")
	require.NoError(t, os.WriteFile(input, []byte("source"), 0o600))
	processor := &FFmpegVideoMediaProcessor{
		ffmpegPath: "ffmpeg", ffprobePath: "ffprobe",
		runner: videoMediaRunner(func(_ context.Context, name string, args ...string) ([]byte, error) {
			require.Equal(t, "ffmpeg", name)
			return nil, os.WriteFile(args[len(args)-1], nil, 0o600)
		}),
	}

	err := processor.CreatePreview(context.Background(), input, output)
	require.ErrorIs(t, err, ErrVideoMediaProcessingFailed)
	require.True(t, isPermanentVideoMediaError(err))
	require.True(t, isPermanentVideoAssetError(err))
}

func TestFFmpegVideoMediaProcessorKeepsPosterWithinLimit(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "source.mp4")
	output := filepath.Join(tempDir, "poster.jpg")
	require.NoError(t, os.WriteFile(input, []byte("source"), 0o600))
	attempts := 0
	processor := &FFmpegVideoMediaProcessor{
		ffmpegPath: "ffmpeg", ffprobePath: "ffprobe",
		runner: videoMediaRunner(func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "ffprobe" {
				return []byte(`{"streams":[{"codec_type":"video","codec_name":"mjpeg","width":720,"height":405,"duration":"0.040000"}],"format":{"format_name":"image2","duration":"0.040000"}}`), nil
			}
			attempts++
			target := args[len(args)-1]
			size := 150 * 1024
			if attempts == 2 {
				size = 100 * 1024
			}
			return nil, os.WriteFile(target, []byte(strings.Repeat("x", size)), 0o600)
		}),
	}

	err := processor.CreatePoster(context.Background(), input, output, 120*1024)
	require.NoError(t, err)
	require.Equal(t, 2, attempts)
	info, err := os.Stat(output)
	require.NoError(t, err)
	require.LessOrEqual(t, info.Size(), int64(120*1024))
}

func TestFFmpegVideoMediaProcessorCreatesLowRatePreview(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "source.mp4")
	output := filepath.Join(tempDir, "preview.mp4")
	require.NoError(t, os.WriteFile(input, []byte("source"), 0o600))
	processor := &FFmpegVideoMediaProcessor{
		ffmpegPath: "ffmpeg", ffprobePath: "ffprobe",
		runner: videoMediaRunner(func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "ffprobe" {
				return []byte(`{"streams":[{"codec_type":"video","codec_name":"h264","width":640,"height":360,"duration":"4"}],"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"4"}}`), nil
			}
			joined := strings.Join(args, " ")
			require.Contains(t, joined, "-an")
			require.Contains(t, joined, "-crf 32")
			require.Contains(t, joined, "-t 4")
			require.Contains(t, joined, "force_original_aspect_ratio=decrease:force_divisible_by=2")
			return nil, os.WriteFile(args[len(args)-1], []byte("preview"), 0o600)
		}),
	}

	require.NoError(t, processor.CreatePreview(context.Background(), input, output))
	info, err := os.Stat(output)
	require.NoError(t, err)
	require.Positive(t, info.Size())
}

func TestVerifyPinnedVideoMediaToolUsesDigestAndVersionOnly(t *testing.T) {
	toolPath := filepath.Join(t.TempDir(), "ffmpeg")
	require.NoError(t, os.WriteFile(toolPath, []byte("pinned-binary"), 0o700))
	digest, err := fileSHA256(toolPath)
	require.NoError(t, err)
	var commands [][]string
	runner := videoMediaRunner(func(_ context.Context, name string, args ...string) ([]byte, error) {
		require.Equal(t, toolPath, name)
		commands = append(commands, append([]string(nil), args...))
		return []byte("ffmpeg version 7.1.1"), nil
	})

	err = verifyPinnedVideoMediaTool(context.Background(), runner, toolPath, "7.1.1", digest)
	require.NoError(t, err)
	require.Equal(t, [][]string{{"-version"}}, commands)
}

func validVideoProbeOutput() []byte {
	return []byte(`{
		"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"duration":"5.25"}],
		"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"5.25"}
	}`)
}
