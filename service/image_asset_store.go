package service

import (
	"context"
	"io"
	"time"
)

// ImageAssetObject is an alias because Image Studio and Video Studio share the
// same private object store. Keeping the image interface narrow prevents either
// studio from depending on the other's business pipeline.
type ImageAssetObject = VideoAssetObject

type ImageAssetStore interface {
	PresignDownload(context.Context, string, string, bool, time.Duration) (string, error)
	Get(context.Context, string) (ImageAssetObject, error)
	Put(context.Context, string, string, io.Reader, int64) error
	Delete(context.Context, []string) error
}

func ImageStudioR2AssetStore(ctx context.Context) (ImageAssetStore, error) {
	return VideoStudioR2AssetStore(ctx)
}
