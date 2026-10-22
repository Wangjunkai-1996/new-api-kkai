package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"
	"gorm.io/gorm"
)

var (
	ErrImageOutputEditSourceUnavailable = errors.New("image output edit source is temporarily unavailable")
	ErrImageOutputEditSourceTimeout     = errors.New("image output edit source read timed out")
)

// ReadImageOutputEditSource returns a fully validated local copy. The caller
// owns the returned archive and must remove it after sending the response.
func ReadImageOutputEditSource(
	ctx context.Context,
	db *gorm.DB,
	store ImageAssetStore,
	userID int,
	assetID int64,
) (*FetchedImageArchive, string, error) {
	asset, err := GetAuthorizedImageAsset(ctx, db, userID, false, assetID)
	if err != nil {
		return nil, "", err
	}
	if asset.Scope != model.ImageAssetScopeUser || asset.OwnerUserID != userID ||
		asset.Kind != model.ImageAssetKindOutput || asset.GenerationID == nil {
		return nil, "", ErrImageAssetAccessDenied
	}
	if asset.State != model.ImageAssetStateReady || asset.ObjectKey == "" {
		return nil, "", ErrImageAssetNotFound
	}
	if _, err := GetOwnedImageGeneration(ctx, db, userID, *asset.GenerationID); err != nil {
		return nil, "", err
	}
	settings := image_studio_setting.Get()
	maxBytes := min(settings.MaxOutputBytes, settings.MaxReferenceBytes, settings.MaxReferenceTotalBytes)
	if asset.SizeBytes > maxBytes {
		return nil, "", ErrImageArchiveTooLarge
	}
	if asset.SizeBytes <= 0 || !isSupportedImageMIME(asset.MIMEType) {
		return nil, "", ErrImageArchiveMIMERejected
	}
	if asset.Width <= 0 || asset.Height <= 0 || int64(asset.Width) > settings.MaxPixels/int64(asset.Height) {
		return nil, "", ErrImageArchivePixelsExceeded
	}
	if store == nil {
		return nil, "", ErrImageOutputEditSourceUnavailable
	}
	readCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	object, err := store.Get(readCtx, asset.ObjectKey)
	if object.Body != nil {
		closeObject := sync.OnceFunc(func() { _ = object.Body.Close() })
		stopClose := context.AfterFunc(readCtx, closeObject)
		defer stopClose()
		defer closeObject()
	}
	if errors.Is(err, ErrVideoAssetObjectNotFound) || isVideoAssetObjectNotFound(err) {
		return nil, "", ErrImageAssetNotFound
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, "", errors.Join(ErrImageOutputEditSourceTimeout, context.DeadlineExceeded)
	}
	if readCtx.Err() != nil {
		if errors.Is(readCtx.Err(), context.DeadlineExceeded) {
			return nil, "", errors.Join(ErrImageOutputEditSourceTimeout, readCtx.Err())
		}
		return nil, "", errors.Join(ErrImageOutputEditSourceUnavailable, readCtx.Err())
	}
	if err != nil || object.Body == nil {
		return nil, "", ErrImageOutputEditSourceUnavailable
	}
	if object.ContentLength > maxBytes {
		return nil, "", ErrImageArchiveTooLarge
	}
	if normalizedVideoObjectContentType(object.ContentType) != asset.MIMEType {
		return nil, "", ErrImageArchiveMIMERejected
	}
	archive, err := NewHTTPImageArchiveFetcher(ImageStudioTempDirectory()).Ingest(
		object.Body, asset.MIMEType, maxBytes, settings.MaxPixels,
	)
	if readCtx.Err() != nil {
		archive.Remove()
		if errors.Is(readCtx.Err(), context.DeadlineExceeded) {
			return nil, "", errors.Join(ErrImageOutputEditSourceTimeout, readCtx.Err())
		}
		return nil, "", errors.Join(ErrImageOutputEditSourceUnavailable, readCtx.Err())
	}
	if err != nil {
		if errors.Is(err, ErrImageArchiveResponseRejected) {
			return nil, "", ErrImageOutputEditSourceUnavailable
		}
		return nil, "", err
	}
	if object.ContentLength >= 0 && archive.SizeBytes != object.ContentLength {
		archive.Remove()
		return nil, "", ErrImageOutputEditSourceUnavailable
	}
	if archive.SizeBytes != asset.SizeBytes ||
		archive.Width != asset.Width || archive.Height != asset.Height ||
		(asset.SHA256 != "" && !strings.EqualFold(archive.SHA256, asset.SHA256)) {
		archive.Remove()
		return nil, "", ErrImageArchiveResponseRejected
	}
	return archive, generatedImageAssetFilename(asset.ID, archive.MIMEType), nil
}
