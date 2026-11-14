package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func ListVideoStudioModelsAPI3(c *gin.Context) {
	tokenID, err := videoStudioCatalogTokenID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	profiles, err := service.ListEffectiveVideoModelProfilesAPI3(
		c.Request.Context(), model.DB, c.GetInt("id"), tokenID, c.ClientIP(),
	)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, profiles)
}

func ListVideoStudioSamplesAPI3(c *gin.Context) {
	tokenID, err := videoStudioCatalogTokenID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	profiles, err := service.ListEffectiveVideoModelProfilesAPI3(
		c.Request.Context(), model.DB, c.GetInt("id"), tokenID, c.ClientIP(),
	)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	allowedModels := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		allowedModels = append(allowedModels, profile.Model)
	}
	page, err := service.ListVideoSamplesAPI3(
		c.Request.Context(), model.DB, c.Query("model"), c.Query("category"), c.Query("cursor"), limit, false, allowedModels,
	)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, page)
}

func GetVideoStudioSampleAPI3(c *gin.Context) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	tokenID, err := videoStudioCatalogTokenID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	profiles, err := service.ListEffectiveVideoModelProfilesAPI3(
		c.Request.Context(), model.DB, c.GetInt("id"), tokenID, c.ClientIP(),
	)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	allowedModels := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		allowedModels = append(allowedModels, profile.Model)
	}
	sample, err := service.GetVideoSampleAPI3(c.Request.Context(), model.DB, id, false, allowedModels)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, sample)
}

func AdminListVideoStudioModelProfilesAPI3(c *gin.Context) {
	profiles, err := service.ListVideoModelProfilesAPI3(c.Request.Context(), model.DB, true)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, profiles)
}

func AdminListVideoStudioModelCandidatesAPI3(c *gin.Context) {
	candidates, err := service.ListVideoModelCandidates(c.Request.Context(), model.DB)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, candidates)
}

func AdminGetVideoStudioModelProfileAPI3(c *gin.Context) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	profile, err := service.GetVideoModelProfileByIDAPI3(c.Request.Context(), model.DB, id)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, profile)
}

func AdminCreateVideoStudioModelProfileAPI3(c *gin.Context) {
	var input service.VideoModelProfileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondVideoStudioError(c, service.ErrInvalidVideoModelSpec)
		return
	}
	profile, err := service.CreateVideoModelProfileAPI3(c.Request.Context(), model.DB, input)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	videoStudioSuccess(c, http.StatusCreated, profile)
}

func AdminUpdateVideoStudioModelProfileAPI3(c *gin.Context) {
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
	profile, err := service.UpdateVideoModelProfileAPI3(c.Request.Context(), model.DB, id, input)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, profile)
}

func AdminDeleteVideoStudioModelProfileAPI3(c *gin.Context) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	if err := service.DeleteVideoModelProfileAPI3(c.Request.Context(), model.DB, id); err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": true})
}

func AdminListVideoStudioSamplesAPI3(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	page, err := service.ListVideoSamplesAPI3(
		c.Request.Context(), model.DB, c.Query("model"), c.Query("category"), c.Query("cursor"), limit, true, nil,
	)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, page)
}

func AdminGetVideoStudioSampleAPI3(c *gin.Context) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	sample, err := service.GetVideoSampleAPI3(c.Request.Context(), model.DB, id, true, nil)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, sample)
}

func AdminCreateVideoStudioSampleAPI3(c *gin.Context) {
	var input service.VideoSampleInputAPI3
	if err := c.ShouldBindJSON(&input); err != nil {
		respondVideoStudioError(c, service.ErrInvalidVideoSample)
		return
	}
	sample, err := service.CreateVideoSampleAPI3(c.Request.Context(), model.DB, c.GetInt("id"), input)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	videoStudioSuccess(c, http.StatusCreated, sample)
}

func AdminUpdateVideoStudioSampleAPI3(c *gin.Context) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	var input service.VideoSampleInputAPI3
	if err := c.ShouldBindJSON(&input); err != nil {
		respondVideoStudioError(c, service.ErrInvalidVideoSample)
		return
	}
	sample, err := service.UpdateVideoSampleAPI3(c.Request.Context(), model.DB, id, c.GetInt("id"), input)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, sample)
}

func AdminDeleteVideoStudioSampleAPI3(c *gin.Context) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	if err := service.DeleteVideoSampleAPI3(c.Request.Context(), model.DB, id); err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": true})
}
