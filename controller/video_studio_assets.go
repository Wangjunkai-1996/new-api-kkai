package controller

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func CreateVideoStudioUpload(c *gin.Context) {
	createVideoStudioUpload(c, false)
}

func AdminCreateVideoStudioUpload(c *gin.Context) {
	createVideoStudioUpload(c, true)
}

func createVideoStudioUpload(c *gin.Context, isAdmin bool) {
	var request service.VideoAssetUploadRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondVideoStudioError(c, service.ErrInvalidVideoAssetUpload)
		return
	}
	store, err := videoStudioAssetStore(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	upload, err := service.CreateVideoAssetUpload(c.Request.Context(), model.DB, store, c.GetInt("id"), isAdmin, request)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	videoStudioSuccess(c, http.StatusCreated, upload)
}

func CompleteVideoStudioUpload(c *gin.Context) {
	completeVideoStudioUpload(c, false)
}

func AdminCompleteVideoStudioUpload(c *gin.Context) {
	completeVideoStudioUpload(c, true)
}

func completeVideoStudioUpload(c *gin.Context, isAdmin bool) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	store, err := videoStudioAssetStore(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	var request service.VideoAssetCompleteRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil && !errors.Is(err, io.EOF) {
			respondVideoStudioError(c, service.ErrInvalidVideoAssetUpload)
			return
		}
	}
	asset, err := service.CompleteVideoAssetUpload(c.Request.Context(), model.DB, store, c.GetInt("id"), isAdmin, id, request)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	videoStudioSuccess(c, http.StatusAccepted, asset)
}

func SignVideoStudioUploadPart(c *gin.Context) {
	signVideoStudioUploadPart(c, false)
}

func AdminSignVideoStudioUploadPart(c *gin.Context) {
	signVideoStudioUploadPart(c, true)
}

func signVideoStudioUploadPart(c *gin.Context, isAdmin bool) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	partNumber, err := strconv.ParseInt(c.Param("part_number"), 10, 32)
	if err != nil || partNumber <= 0 {
		respondVideoStudioError(c, service.ErrInvalidVideoAssetUpload)
		return
	}
	store, err := videoStudioAssetStore(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	signed, err := service.SignVideoAssetUploadPart(
		c.Request.Context(), model.DB, store, c.GetInt("id"), isAdmin, id, int32(partNumber),
	)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, signed)
}

func ListVideoStudioUploadParts(c *gin.Context) {
	listVideoStudioUploadParts(c, false)
}

func AdminListVideoStudioUploadParts(c *gin.Context) {
	listVideoStudioUploadParts(c, true)
}

func listVideoStudioUploadParts(c *gin.Context, isAdmin bool) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	store, err := videoStudioAssetStore(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	parts, err := service.ListVideoAssetUploadParts(c.Request.Context(), model.DB, store, c.GetInt("id"), isAdmin, id)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"parts": parts})
}

func AbortVideoStudioUpload(c *gin.Context) {
	abortVideoStudioUpload(c, false)
}

func AdminAbortVideoStudioUpload(c *gin.Context) {
	abortVideoStudioUpload(c, true)
}

func abortVideoStudioUpload(c *gin.Context, isAdmin bool) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	store, err := videoStudioAssetStore(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	asset, err := service.AbortVideoAssetUpload(c.Request.Context(), model.DB, store, c.GetInt("id"), isAdmin, id)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, asset)
}

func GetVideoStudioUpload(c *gin.Context) {
	getVideoStudioUpload(c, false)
}

func GetVideoStudioAsset(c *gin.Context) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	isAdmin := c.GetInt("role") >= common.RoleAdminUser
	asset, err := service.GetVideoAsset(c.Request.Context(), model.DB, c.GetInt("id"), isAdmin, id)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, asset)
}

func AdminGetVideoStudioUpload(c *gin.Context) {
	getVideoStudioUpload(c, true)
}

func getVideoStudioUpload(c *gin.Context, isAdmin bool) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	store, err := videoStudioAssetStore(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	asset, err := service.GetVideoAssetUpload(c.Request.Context(), model.DB, store, c.GetInt("id"), isAdmin, id)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, asset)
}

func GetVideoStudioAssetContent(c *gin.Context) {
	redirectVideoStudioAsset(c, false)
}

func DownloadVideoStudioAsset(c *gin.Context) {
	redirectVideoStudioAsset(c, true)
}

func DeleteVideoStudioReferenceAsset(c *gin.Context) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	asset, err := service.DeleteVideoReferenceAsset(c.Request.Context(), model.DB, c.GetInt("id"), id)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	videoStudioSuccess(c, http.StatusAccepted, asset)
}

func redirectVideoStudioAsset(c *gin.Context, attachment bool) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	store, err := videoStudioAssetStore(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	variant := c.Query("variant")
	if attachment {
		variant = ""
	}
	isAdmin := c.GetInt("role") >= common.RoleAdminUser
	location, err := service.SignAuthorizedVideoAsset(c.Request.Context(), model.DB, store, c.GetInt("id"), isAdmin, id, variant, attachment)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	c.Redirect(http.StatusFound, location)
}
