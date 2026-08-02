package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func ListImageStudioModels(c *gin.Context) {
	tokenID, err := imageStudioCatalogTokenID(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	profiles, err := service.ListEffectiveImageModelProfiles(
		c.Request.Context(), model.DB, c.GetInt("id"), tokenID, c.ClientIP(),
	)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	common.ApiSuccess(c, profiles)
}

func imageStudioCatalogTokenID(c *gin.Context) (int, error) {
	tokenID, err := strconv.Atoi(c.Query("token_id"))
	if err != nil || tokenID <= 0 {
		return 0, service.ErrImageStudioTokenRequired
	}
	return tokenID, nil
}

func AdminListImageStudioModelProfiles(c *gin.Context) {
	profiles, err := service.ListImageModelProfiles(c.Request.Context(), model.DB, true)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	common.ApiSuccess(c, profiles)
}

func AdminListImageStudioModelCandidates(c *gin.Context) {
	candidates, err := service.ListImageModelCandidates(c.Request.Context(), model.DB)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	common.ApiSuccess(c, candidates)
}

func AdminGetImageStudioModelProfile(c *gin.Context) {
	id, err := imageStudioID(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	profile, err := service.GetImageModelProfileByID(c.Request.Context(), model.DB, id)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	common.ApiSuccess(c, profile)
}

func AdminCreateImageStudioModelProfile(c *gin.Context) {
	var input service.ImageModelProfileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondImageStudioError(c, service.ErrInvalidImageModelSpec)
		return
	}
	profile, err := service.CreateImageModelProfile(c.Request.Context(), model.DB, input)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	imageStudioSuccess(c, 201, profile)
}

func AdminUpdateImageStudioModelProfile(c *gin.Context) {
	id, err := imageStudioID(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	var input service.ImageModelProfileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondImageStudioError(c, service.ErrInvalidImageModelSpec)
		return
	}
	profile, err := service.UpdateImageModelProfile(c.Request.Context(), model.DB, id, input)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	common.ApiSuccess(c, profile)
}

func AdminDeleteImageStudioModelProfile(c *gin.Context) {
	id, err := imageStudioID(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	if err := service.DeleteImageModelProfile(c.Request.Context(), model.DB, id); err != nil {
		respondImageStudioError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
