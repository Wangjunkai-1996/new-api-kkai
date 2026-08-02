package controller

import (
	"net/http"
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
		Model: c.Query("model"), Status: c.Query("status"), Cursor: c.Query("cursor"), Limit: limit,
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
