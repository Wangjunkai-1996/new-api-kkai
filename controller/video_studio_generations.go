package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func ListVideoStudioGenerations(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	page, err := service.ListVideoGenerations(c.Request.Context(), model.DB, c.GetInt("id"), service.VideoGenerationListRequest{
		Cursor: c.Query("cursor"), Status: c.Query("status"), Limit: limit,
	})
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, page)
}

func GetVideoStudioGeneration(c *gin.Context) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	generation, err := service.GetVideoGeneration(c.Request.Context(), model.DB, c.GetInt("id"), id)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, generation)
}

func DeleteVideoStudioGeneration(c *gin.Context) {
	id, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	if err := service.DeleteVideoGeneration(c.Request.Context(), model.DB, c.GetInt("id"), id); err != nil {
		respondVideoStudioError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": true})
}
