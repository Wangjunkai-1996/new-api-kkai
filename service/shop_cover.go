package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"regexp"

	"golang.org/x/image/draw"
)

const MaxShopCoverBytes = 5 << 20

var (
	ErrInvalidShopCover  = errors.New("invalid shop cover")
	ErrShopCoverTooLarge = errors.New("shop cover too large")
	shopCoverName        = regexp.MustCompile(`^[a-f0-9]{64}\.jpg$`)
)

type ShopCover struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// SaveShopCover uses the shared object store, but never creates a temporary
// Studio upload. Covers remain available to saved products and order snapshots.
func SaveShopCover(ctx context.Context, store ImageAssetStore, reader io.Reader) (*ShopCover, error) {
	if ctx == nil || store == nil || reader == nil {
		return nil, ErrInvalidShopCover
	}
	content, err := io.ReadAll(io.LimitReader(reader, MaxShopCoverBytes+1))
	if err != nil {
		return nil, ErrInvalidShopCover
	}
	if len(content) > MaxShopCoverBytes {
		return nil, ErrShopCoverTooLarge
	}
	mimeType := detectSupportedImageMIME(content)
	width, height, err := decodeImageDimensions(bytes.NewReader(content), mimeType)
	if err != nil || width <= 0 || height <= 0 || width > 4096 || height > 4096 || int64(width)*int64(height) > 16_000_000 {
		return nil, ErrInvalidShopCover
	}
	source, _, err := image.Decode(bytes.NewReader(content))
	if err != nil || source.Bounds().Dx() != width || source.Bounds().Dy() != height {
		return nil, ErrInvalidShopCover
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Publish pixels only: remove EXIF/GPS, embedded payloads and animation,
	// and keep a bounded, immutable cover independent of the original file.
	if width > 1600 || height > 1600 {
		if width >= height {
			height, width = max(1, height*1600/width), 1600
		} else {
			width, height = max(1, width*1600/height), 1600
		}
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(canvas, canvas.Bounds(), source, source.Bounds(), draw.Over, nil)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, canvas, &jpeg.Options{Quality: 88}); err != nil {
		return nil, ErrInvalidShopCover
	}
	if encoded.Len() > MaxShopCoverBytes {
		return nil, ErrShopCoverTooLarge
	}
	digest := sha256.Sum256(encoded.Bytes())
	filename := fmt.Sprintf("%x.jpg", digest)
	if err := store.Put(ctx, "shop/covers/v1/"+filename, "image/jpeg", bytes.NewReader(encoded.Bytes()), int64(encoded.Len())); err != nil {
		return nil, err
	}
	return &ShopCover{URL: "/api/shop/covers/" + filename, Width: width, Height: height}, nil
}

// ReadShopCover never accepts a caller-supplied object key. Public catalogue
// access can only resolve normalized covers in this dedicated namespace.
func ReadShopCover(ctx context.Context, store ImageAssetStore, filename string) (ImageAssetObject, error) {
	if !shopCoverName.MatchString(filename) {
		return ImageAssetObject{}, ErrVideoAssetObjectNotFound
	}
	if ctx == nil || store == nil {
		return ImageAssetObject{}, ErrInvalidShopCover
	}
	object, err := store.Get(ctx, "shop/covers/v1/"+filename)
	if err != nil {
		if isVideoAssetObjectNotFound(err) {
			return ImageAssetObject{}, ErrVideoAssetObjectNotFound
		}
		return ImageAssetObject{}, err
	}
	if object.Body == nil || object.ContentType != "image/jpeg" || object.ContentLength <= 0 || object.ContentLength > MaxShopCoverBytes {
		if object.Body != nil {
			_ = object.Body.Close()
		}
		return ImageAssetObject{}, ErrVideoAssetObjectNotFound
	}
	return object, nil
}
