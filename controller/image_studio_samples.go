package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"

	"github.com/gin-gonic/gin"
)

func ListImageStudioSamples(c *gin.Context) {
	tokenID, err := imageStudioCatalogTokenID(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	allowedModels, err := effectiveImageStudioModels(c, tokenID)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "24"))
	if err != nil {
		respondImageStudioError(c, service.ErrInvalidImageSample)
		return
	}
	page, err := service.ListImageSamples(
		c.Request.Context(), model.DB, c.Query("model"), c.Query("category"), c.Query("cursor"),
		limit, false, allowedModels,
	)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	common.ApiSuccess(c, page)
}

func GetImageStudioSample(c *gin.Context) {
	id, err := imageStudioID(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	tokenID, err := imageStudioCatalogTokenID(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	allowedModels, err := effectiveImageStudioModels(c, tokenID)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	sample, err := service.GetImageSample(c.Request.Context(), model.DB, id, false, allowedModels)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	common.ApiSuccess(c, sample)
}

func AdminListImageStudioSamples(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil {
		respondImageStudioError(c, service.ErrInvalidImageSample)
		return
	}
	page, err := service.ListImageSamples(
		c.Request.Context(), model.DB, c.Query("model"), c.Query("category"), c.Query("cursor"),
		limit, true, nil,
	)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	common.ApiSuccess(c, page)
}

func AdminGetImageStudioSample(c *gin.Context) {
	id, err := imageStudioID(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	sample, err := service.GetImageSample(c.Request.Context(), model.DB, id, true, nil)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	common.ApiSuccess(c, sample)
}

func AdminCreateImageStudioSample(c *gin.Context) {
	var input service.ImageSampleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondImageStudioError(c, service.ErrInvalidImageSample)
		return
	}
	sample, err := service.CreateImageSample(c.Request.Context(), model.DB, input)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	imageStudioSuccess(c, http.StatusCreated, sample)
}

func AdminUpdateImageStudioSample(c *gin.Context) {
	id, err := imageStudioID(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	var input service.ImageSampleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondImageStudioError(c, service.ErrInvalidImageSample)
		return
	}
	sample, err := service.UpdateImageSample(c.Request.Context(), model.DB, id, input)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	common.ApiSuccess(c, sample)
}

func AdminDeleteImageStudioSample(c *gin.Context) {
	id, err := imageStudioID(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	if err := service.DeleteImageSample(c.Request.Context(), model.DB, id); err != nil {
		respondImageStudioError(c, err)
		return
	}
	imageStudioSuccess(c, http.StatusAccepted, nil)
}

func AdminUploadImageStudioSampleAsset(c *gin.Context) {
	settings := image_studio_setting.Get()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, settings.MaxOutputBytes+(1<<20))
	header, err := c.FormFile("file")
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			respondImageStudioError(c, service.ErrImageArchiveTooLarge)
			return
		}
		respondImageStudioError(c, service.ErrInvalidImageSample)
		return
	}
	if header.Size <= 0 {
		respondImageStudioError(c, service.ErrInvalidImageSample)
		return
	}
	if header.Size > settings.MaxOutputBytes {
		respondImageStudioError(c, service.ErrImageArchiveTooLarge)
		return
	}
	file, err := header.Open()
	if err != nil {
		respondImageStudioError(c, service.ErrInvalidImageSample)
		return
	}
	defer file.Close()
	store, err := imageStudioAssetStore(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	asset, err := service.CreateImageCatalogAsset(
		c.Request.Context(), model.DB, store,
		service.NewHTTPImageArchiveFetcher(service.ImageStudioTempDirectory()),
		c.GetInt("id"), header.Filename, header.Header.Get("Content-Type"), file,
	)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	imageStudioSuccess(c, http.StatusCreated, asset)
}

func effectiveImageStudioModels(c *gin.Context, tokenID int) ([]string, error) {
	profiles, err := service.ListEffectiveImageModelProfiles(
		c.Request.Context(), model.DB, c.GetInt("id"), tokenID, c.ClientIP(),
	)
	if err != nil {
		return nil, err
	}
	models := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		models = append(models, profile.Model)
	}
	return models, nil
}
