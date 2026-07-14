package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

var getKKAIUserGroup = model.GetUserGroup

func GetKKAIGroupStatus(c *gin.Context) {
	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	userGroup, err := getKKAIUserGroup(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := service.GetKKAIGroupStatuses(service.KKAIGroupStatusRequest{
		UsableGroups: service.GetUserUsableGroups(userGroup),
		AutoGroups:   setting.GetAutoGroups(),
		Hours:        hours,
		Window:       c.Query("window"),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
