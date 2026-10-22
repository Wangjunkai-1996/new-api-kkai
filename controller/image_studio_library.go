package controller

import (
	"mime"
	"net/http"
	"os"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func ListImageStudioGenerations(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "24"))
	if err != nil {
		respondImageStudioError(c, service.ErrInvalidImageGenerationFilter)
		return
	}
	page, err := service.ListImageGenerations(c.Request.Context(), model.DB, c.GetInt("id"), service.ImageGenerationFilter{
		Model: c.Query("model"), Query: c.Query("q"), Status: c.Query("status"), Cursor: c.Query("cursor"), Limit: limit,
	})
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	common.ApiSuccess(c, page)
}

func GetImageStudioGeneration(c *gin.Context) {
	id, err := imageStudioID(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	view, err := service.GetImageGenerationView(c.Request.Context(), model.DB, c.GetInt("id"), id)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func GetImageStudioGenerationInput(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := imageStudioID(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	input, err := service.GetImageGenerationInput(c.Request.Context(), model.DB, c.GetInt("id"), id)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	common.ApiSuccess(c, input)
}

func DeleteImageStudioGeneration(c *gin.Context) {
	id, err := imageStudioID(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	if err := service.DeleteImageGeneration(c.Request.Context(), model.DB, c.GetInt("id"), id); err != nil {
		respondImageStudioError(c, err)
		return
	}
	imageStudioSuccess(c, http.StatusAccepted, nil)
}

func GetImageStudioAsset(c *gin.Context) {
	id, err := imageStudioID(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	isAdmin := c.GetInt("role") >= common.RoleAdminUser
	asset, err := service.GetAuthorizedImageAsset(c.Request.Context(), model.DB, c.GetInt("id"), isAdmin, id)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"id": asset.ID, "scope": asset.Scope, "kind": asset.Kind, "state": asset.State,
		"mime_type": asset.MIMEType, "size_bytes": asset.SizeBytes,
		"width": asset.Width, "height": asset.Height, "thumbnail_state": asset.ThumbnailState,
	})
}

func GetImageStudioAssetContent(c *gin.Context) {
	redirectImageStudioAsset(c, false)
}

func DownloadImageStudioAsset(c *gin.Context) {
	redirectImageStudioAsset(c, true)
}

func GetImageStudioAssetEditSource(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	id, err := imageStudioID(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	store, err := imageStudioAssetStore(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	archive, filename, err := service.ReadImageOutputEditSource(c.Request.Context(), model.DB, store, c.GetInt("id"), id)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	defer archive.Remove()
	file, err := os.Open(archive.Path)
	if err != nil {
		respondImageStudioError(c, service.ErrImageOutputEditSourceUnavailable)
		return
	}
	defer file.Close()
	c.Header("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": filename}))
	c.DataFromReader(http.StatusOK, archive.SizeBytes, archive.MIMEType, file, nil)
}

func redirectImageStudioAsset(c *gin.Context, attachment bool) {
	id, err := imageStudioID(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	store, err := imageStudioAssetStore(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	thumbnail := !attachment && c.Query("variant") == "thumbnail"
	isAdmin := c.GetInt("role") >= common.RoleAdminUser
	location, err := service.SignAuthorizedImageAsset(
		c.Request.Context(), model.DB, store, c.GetInt("id"), isAdmin, id, thumbnail, attachment,
	)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	c.Redirect(http.StatusFound, location)
}
