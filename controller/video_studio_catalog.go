package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func ListVideoStudioModels(c *gin.Context) {
	profiles, err := service.ListEffectiveVideoModelProfiles(
		c.Request.Context(), model.DB, c.GetInt("id"), c.ClientIP(),
	)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, profiles)
}

func ListVideoStudioSamples(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	profiles, err := service.ListEffectiveVideoModelProfiles(
		c.Request.Context(), model.DB, c.GetInt("id"), c.ClientIP(),
	)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	allowedModels := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		allowedModels = append(allowedModels, profile.Model)
	}
	page, err := service.ListVideoSamples(
		c.Request.Context(), model.DB, c.Query("model"), c.Query("cursor"), limit, false, allowedModels,
	)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, page)
}

func GetVideoStudioSample(c *gin.Context) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	profiles, err := service.ListEffectiveVideoModelProfiles(
		c.Request.Context(), model.DB, c.GetInt("id"), c.ClientIP(),
	)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	allowedModels := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		allowedModels = append(allowedModels, profile.Model)
	}
	sample, err := service.GetVideoSample(c.Request.Context(), model.DB, id, false, allowedModels)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, sample)
}

func AdminListVideoStudioModelProfiles(c *gin.Context) {
	profiles, err := service.ListVideoModelProfiles(c.Request.Context(), model.DB, true)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, profiles)
}

func AdminGetVideoStudioModelProfile(c *gin.Context) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	profile, err := service.GetVideoModelProfileByID(c.Request.Context(), model.DB, id)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, profile)
}

func AdminCreateVideoStudioModelProfile(c *gin.Context) {
	var input service.VideoModelProfileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondVideoStudioError(c, service.ErrInvalidVideoModelSpec)
		return
	}
	profile, err := service.CreateVideoModelProfile(c.Request.Context(), model.DB, input)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	videoStudioSuccess(c, http.StatusCreated, profile)
}

func AdminUpdateVideoStudioModelProfile(c *gin.Context) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	var input service.VideoModelProfileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondVideoStudioError(c, service.ErrInvalidVideoModelSpec)
		return
	}
	profile, err := service.UpdateVideoModelProfile(c.Request.Context(), model.DB, id, input)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, profile)
}

func AdminDeleteVideoStudioModelProfile(c *gin.Context) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	if err := service.DeleteVideoModelProfile(c.Request.Context(), model.DB, id); err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": true})
}

func AdminListVideoStudioSamples(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	page, err := service.ListVideoSamples(c.Request.Context(), model.DB, c.Query("model"), c.Query("cursor"), limit, true, nil)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, page)
}

func AdminGetVideoStudioSample(c *gin.Context) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	sample, err := service.GetVideoSample(c.Request.Context(), model.DB, id, true, nil)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, sample)
}

func AdminCreateVideoStudioSample(c *gin.Context) {
	var input service.VideoSampleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondVideoStudioError(c, service.ErrInvalidVideoSample)
		return
	}
	sample, err := service.CreateVideoSample(c.Request.Context(), model.DB, c.GetInt("id"), input)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	videoStudioSuccess(c, http.StatusCreated, sample)
}

func AdminUpdateVideoStudioSample(c *gin.Context) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	var input service.VideoSampleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondVideoStudioError(c, service.ErrInvalidVideoSample)
		return
	}
	sample, err := service.UpdateVideoSample(c.Request.Context(), model.DB, id, c.GetInt("id"), input)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, sample)
}

func AdminDeleteVideoStudioSample(c *gin.Context) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	if err := service.DeleteVideoSample(c.Request.Context(), model.DB, id); err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": true})
}
