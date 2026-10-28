package controller

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func UploadShopCover(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.MaxShopCoverBytes+(64<<10))
	parseErr := c.Request.ParseMultipartForm(service.MaxShopCoverBytes)
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	if parseErr != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(parseErr, &tooLarge) {
			shopError(c, http.StatusRequestEntityTooLarge, "SHOP_COVER_TOO_LARGE")
			return
		}
		shopError(c, http.StatusBadRequest, "SHOP_COVER_INVALID")
		return
	}
	form := c.Request.MultipartForm
	if form == nil || len(form.Value) != 0 || len(form.File) != 1 || len(form.File["file"]) != 1 {
		shopError(c, http.StatusBadRequest, "SHOP_COVER_INVALID")
		return
	}
	header := form.File["file"][0]
	if header.Size > service.MaxShopCoverBytes {
		shopError(c, http.StatusRequestEntityTooLarge, "SHOP_COVER_TOO_LARGE")
		return
	}
	file, err := header.Open()
	if err != nil {
		shopError(c, http.StatusBadRequest, "SHOP_COVER_INVALID")
		return
	}
	defer file.Close()
	store, err := imageStudioAssetStore(c)
	if err != nil {
		shopError(c, http.StatusServiceUnavailable, "SHOP_COVER_STORAGE_UNAVAILABLE")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	cover, err := service.SaveShopCover(ctx, store, file)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrShopCoverTooLarge):
			shopError(c, http.StatusRequestEntityTooLarge, "SHOP_COVER_TOO_LARGE")
		case errors.Is(err, service.ErrInvalidShopCover):
			shopError(c, http.StatusBadRequest, "SHOP_COVER_INVALID")
		default:
			shopError(c, http.StatusServiceUnavailable, "SHOP_COVER_STORAGE_UNAVAILABLE")
		}
		return
	}
	shopSuccess(c, http.StatusCreated, cover)
}

func GetShopCover(c *gin.Context) {
	store, err := imageStudioAssetStore(c)
	if err != nil {
		shopError(c, http.StatusServiceUnavailable, "SHOP_COVER_STORAGE_UNAVAILABLE")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	filename := c.Param("filename")
	object, err := service.ReadShopCover(ctx, store, filename)
	if err != nil {
		if errors.Is(err, service.ErrVideoAssetObjectNotFound) {
			shopError(c, http.StatusNotFound, "SHOP_NOT_FOUND")
			return
		}
		shopError(c, http.StatusServiceUnavailable, "SHOP_COVER_STORAGE_UNAVAILABLE")
		return
	}
	defer object.Body.Close()
	etag := `"` + strings.TrimSuffix(filename, ".jpg") + `"`
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("ETag", etag)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", `inline; filename="cover.jpg"`)
	if c.GetHeader("If-None-Match") == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.DataFromReader(http.StatusOK, object.ContentLength, "image/jpeg", io.LimitReader(object.Body, service.MaxShopCoverBytes), nil)
}
