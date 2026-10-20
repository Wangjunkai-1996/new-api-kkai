package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

func GetPerfMetricsSummary(c *gin.Context) {
	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	var result perfmetrics.SummaryAllResult
	var err error
	if common.GetContextKeyInt64(c, constant.ContextKeyTenantID) > model.PlatformTenantID {
		prices, priceErr := tenantCustomerPrices(c)
		if priceErr != nil {
			common.ApiError(c, priceErr)
			return
		}
		modelGroups := make(map[string][]string, len(prices))
		for _, price := range prices {
			modelGroups[price.ModelName] = price.EnableGroup
		}
		result, err = perfmetrics.QuerySummaryForModels(hours, modelGroups)
	} else {
		activeGroups := append(lo.Keys(ratio_setting.GetGroupRatioCopy()), setting.GetAutoGroupNames()...)
		result, err = perfmetrics.QuerySummaryAll(hours, activeGroups)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

func GetPerfMetrics(c *gin.Context) {
	modelName := c.Query("model")
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "model is required",
		})
		return
	}

	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	allowedGroups := append(lo.Keys(ratio_setting.GetGroupRatioCopy()), setting.GetAutoGroupNames()...)
	if common.GetContextKeyInt64(c, constant.ContextKeyTenantID) > model.PlatformTenantID {
		prices, err := tenantCustomerPrices(c)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		allowedGroups = []string{}
		for _, price := range prices {
			if price.ModelName == modelName {
				allowedGroups = price.EnableGroup
				break
			}
		}
	}
	result, err := perfmetrics.Query(perfmetrics.QueryParams{
		Model:         modelName,
		Group:         c.Query("group"),
		Hours:         hours,
		AllowedGroups: allowedGroups,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}
