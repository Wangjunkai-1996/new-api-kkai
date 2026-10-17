package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func CreateImageStudioUpload(c *gin.Context) {
	if !service.ImageAsyncAdmissionEnabled(c.Request.Context(), model.DB) {
		respondImageStudioUploadError(c, service.ErrImageUploadUnavailable)
		return
	}
	var request service.ImageUploadRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	if c.ContentType() != "application/json" || common.DecodeJson(c.Request.Body, &request) != nil {
		respondImageStudioUploadError(c, service.ErrInvalidImageUpload)
		return
	}
	store, err := imageStudioAssetStore(c)
	if err != nil {
		respondImageStudioUploadError(c, err)
		return
	}
	upload, err := service.CreateImageReferenceUpload(c.Request.Context(), model.DB, store, c.GetInt("id"), c.GetHeader("Idempotency-Key"), request)
	if err != nil {
		respondImageStudioUploadError(c, err)
		return
	}
	imageStudioSuccess(c, http.StatusCreated, upload)
}

func GetImageStudioUpload(c *gin.Context) {
	if !requireImageUploadSchema(c) {
		return
	}
	id, err := imageStudioID(c)
	if err != nil {
		respondImageStudioUploadError(c, err)
		return
	}
	upload, err := service.GetImageReferenceUpload(c.Request.Context(), model.DB, c.GetInt("id"), id)
	if err != nil {
		respondImageStudioUploadError(c, err)
		return
	}
	common.ApiSuccess(c, upload)
}

func SignImageStudioUploadPart(c *gin.Context) {
	if !requireImageUploadSchema(c) {
		return
	}
	id, err := imageStudioID(c)
	if err != nil {
		respondImageStudioUploadError(c, err)
		return
	}
	part, err := strconv.ParseInt(c.Param("part_number"), 10, 32)
	if err != nil || part <= 0 {
		respondImageStudioUploadError(c, service.ErrInvalidImageUpload)
		return
	}
	store, err := imageStudioAssetStore(c)
	if err != nil {
		respondImageStudioUploadError(c, err)
		return
	}
	signed, err := service.SignImageReferenceUploadPart(c.Request.Context(), model.DB, store, c.GetInt("id"), id, int32(part))
	if err != nil {
		respondImageStudioUploadError(c, err)
		return
	}
	common.ApiSuccess(c, signed)
}

func ListImageStudioUploadParts(c *gin.Context) {
	if !requireImageUploadSchema(c) {
		return
	}
	id, err := imageStudioID(c)
	if err != nil {
		respondImageStudioUploadError(c, err)
		return
	}
	store, err := imageStudioAssetStore(c)
	if err != nil {
		respondImageStudioUploadError(c, err)
		return
	}
	parts, err := service.ListImageReferenceUploadParts(c.Request.Context(), model.DB, store, c.GetInt("id"), id)
	if err != nil {
		respondImageStudioUploadError(c, err)
		return
	}
	if parts == nil {
		parts = []service.VideoAssetUploadedPart{}
	}
	common.ApiSuccess(c, gin.H{"parts": parts})
}

func CompleteImageStudioUpload(c *gin.Context) {
	if !requireImageUploadSchema(c) {
		return
	}
	id, err := imageStudioID(c)
	if err != nil {
		respondImageStudioUploadError(c, err)
		return
	}
	var request service.VideoAssetCompleteRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	if c.ContentType() != "application/json" || common.DecodeJson(c.Request.Body, &request) != nil {
		respondImageStudioUploadError(c, service.ErrInvalidImageUpload)
		return
	}
	asset, err := service.CompleteImageReferenceUpload(c.Request.Context(), model.DB, c.GetInt("id"), id, request.Parts)
	if err != nil {
		respondImageStudioUploadError(c, err)
		return
	}
	c.Header("Retry-After", "2")
	imageStudioSuccess(c, http.StatusAccepted, asset)
}

func AbortImageStudioUpload(c *gin.Context) {
	if !requireImageUploadSchema(c) {
		return
	}
	id, err := imageStudioID(c)
	if err != nil {
		respondImageStudioUploadError(c, err)
		return
	}
	asset, err := service.AbortImageReferenceUpload(c.Request.Context(), model.DB, c.GetInt("id"), id)
	if err != nil {
		respondImageStudioUploadError(c, err)
		return
	}
	imageStudioSuccess(c, http.StatusAccepted, asset)
}

func requireImageUploadSchema(c *gin.Context) bool {
	if service.ImageAsyncSchemaReady(c.Request.Context(), model.DB) {
		return true
	}
	respondImageStudioUploadError(c, service.ErrImageUploadUnavailable)
	return false
}

func respondImageStudioUploadError(c *gin.Context, err error) {
	status, code := 0, ""
	switch {
	case errors.Is(err, service.ErrInvalidImageUpload):
		status, code = http.StatusBadRequest, "invalid_image_upload"
	case errors.Is(err, service.ErrImageUploadConflict):
		status, code = http.StatusConflict, "image_upload_conflict"
	case errors.Is(err, service.ErrImageUploadExpired):
		status, code = http.StatusGone, "image_upload_expired"
	case errors.Is(err, service.ErrImageUploadCapacity):
		status, code = http.StatusTooManyRequests, "image_upload_capacity"
	case errors.Is(err, service.ErrImageUploadUnavailable), errors.Is(err, service.ErrImageUploadLengthNotSigned):
		status, code = http.StatusServiceUnavailable, "image_upload_unavailable"
	default:
		respondImageStudioError(c, err)
		return
	}
	c.JSON(status, gin.H{"success": false, "message": err.Error(), "code": code})
}
