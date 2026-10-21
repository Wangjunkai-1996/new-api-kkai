package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResellerGroupMarkupTracksPlatformPricesAndResets(t *testing.T) {
	db := resellerPricesFixture(t)
	saved := resellerPricesHTTP(t, 42, 7, http.MethodPatch, "/prices", `{"prices":[{"sales_group":"premium","markup_percent":"20","expected_version":0}]}`)
	require.Equal(t, http.StatusOK, saved.Code)
	var result struct {
		Success bool                       `json:"success"`
		Data    []service.TenantGroupPrice `json:"data"`
	}
	require.NoError(t, common.Unmarshal(saved.Body.Bytes(), &result))
	require.True(t, result.Success, saved.Body.String())
	require.Len(t, result.Data, 2)
	assert.Equal(t, "2.4", result.Data[1].SalesMultiplier)
	assert.Equal(t, "20", result.Data[1].MarkupPercent)
	assert.Equal(t, "markup", result.Data[1].PricingMode)
	assert.False(t, result.Data[1].Inherited)

	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"premium":2.5}`))
	read := resellerPricesHTTP(t, 42, 7, http.MethodGet, "/prices?scope=group", "")
	require.NoError(t, common.Unmarshal(read.Body.Bytes(), &result))
	require.True(t, result.Success, read.Body.String())
	assert.Equal(t, "3", result.Data[1].SalesMultiplier)
	assert.Equal(t, "20", result.Data[1].MarkupPercent)
	var projection struct {
		Success bool                       `json:"success"`
		Data    []model.TenantPriceVersion `json:"data"`
	}
	read = resellerPricesHTTP(t, 42, 7, http.MethodGet, "/prices", "")
	require.NoError(t, common.Unmarshal(read.Body.Bytes(), &projection))
	require.True(t, projection.Success, read.Body.String())
	require.Len(t, projection.Data, 2)
	assert.Equal(t, "shared-text", projection.Data[1].Model)
	assert.Equal(t, "3", projection.Data[1].SalesMultiplier)

	reset := resellerPricesHTTP(t, 42, 7, http.MethodPatch, "/prices", `{"prices":[{"sales_group":"premium","markup_percent":"0","expected_version":1}]}`)
	require.Equal(t, http.StatusOK, reset.Code)
	require.NoError(t, common.Unmarshal(reset.Body.Bytes(), &result))
	require.True(t, result.Success, reset.Body.String())
	assert.Equal(t, "2.5", result.Data[1].SalesMultiplier)
	assert.Equal(t, "0", result.Data[1].MarkupPercent)
	assert.True(t, result.Data[1].Inherited)
	assert.EqualValues(t, 2, result.Data[1].Version)
	var count int64
	require.NoError(t, db.Model(&model.TenantPriceVersion{}).Count(&count).Error)
	assert.EqualValues(t, 2, count, "reads never rewrite price history")
}

func TestResellerGroupMarkupConvertsLegacyWithoutRoundingPrice(t *testing.T) {
	db := resellerPricesFixture(t)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"premium":1.5}`))
	legacy := model.TenantPriceVersion{TenantID: 42, Group: "premium", SalesMultiplier: "2", Version: 1}
	require.NoError(t, db.Create(&legacy).Error)
	var result struct {
		Success bool                       `json:"success"`
		Data    []service.TenantGroupPrice `json:"data"`
	}
	read := resellerPricesHTTP(t, 42, 7, http.MethodGet, "/prices?scope=group", "")
	require.NoError(t, common.Unmarshal(read.Body.Bytes(), &result))
	require.True(t, result.Success, read.Body.String())
	assert.Equal(t, "legacy", result.Data[1].PricingMode)
	assert.Equal(t, "2", result.Data[1].SalesMultiplier)

	conflict := resellerPricesHTTP(t, 42, 7, http.MethodPatch, "/prices", `{"prices":[{"sales_group":"premium","convert_legacy":true,"expected_platform_multiplier":"1","expected_version":1}]}`)
	assert.Equal(t, http.StatusConflict, conflict.Code)
	saved := resellerPricesHTTP(t, 42, 7, http.MethodPatch, "/prices", `{"prices":[{"sales_group":"premium","convert_legacy":true,"expected_platform_multiplier":"1.5","expected_version":1}]}`)
	require.Equal(t, http.StatusOK, saved.Code)
	require.NoError(t, common.Unmarshal(saved.Body.Bytes(), &result))
	require.True(t, result.Success, saved.Body.String())
	assert.Equal(t, "markup", result.Data[1].PricingMode)
	assert.Equal(t, "2", result.Data[1].SalesMultiplier, "converting 33.333...% keeps the exact existing price")
	assert.EqualValues(t, 2, result.Data[1].Version)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"premium":3}`))
	read = resellerPricesHTTP(t, 42, 7, http.MethodGet, "/prices?scope=group", "")
	require.NoError(t, common.Unmarshal(read.Body.Bytes(), &result))
	require.True(t, result.Success, read.Body.String())
	assert.Equal(t, "4", result.Data[1].SalesMultiplier)
	var original model.TenantPriceVersion
	require.NoError(t, db.First(&original, legacy.ID).Error)
	assert.Equal(t, "2", original.SalesMultiplier)
}

func TestResellerGroupMarkupRejectsInvalidRequestsWithoutWrites(t *testing.T) {
	db := resellerPricesFixture(t)
	for _, body := range []string{
		`{"prices":[{"sales_group":"default","markup_percent":"-1"}]}`,
		`{"prices":[{"sales_group":"default","markup_percent":"1000001"}]}`,
		`{"prices":[{"sales_group":"default","markup_percent":"NaN"}]}`,
		`{"prices":[{"sales_group":"default","markup_percent":"Infinity"}]}`,
		`{"prices":[{"sales_group":"default","markup_percent":"1e1000000"}]}`,
		`{"prices":[{"sales_group":"default","markup_percent":""}]}`,
		`{"prices":[{"sales_group":"default","markup_percent":null}]}`,
		`{"prices":[{"sales_group":"default","markup_percent":"20","sale_multiplier":""}]}`,
		`{"prices":[{"sales_group":"default","markup_percent":"20","convert_legacy":true}]}`,
		`{"prices":[{"sales_group":"default","convert_legacy":true}]}`,
		`{"prices":[{"sales_group":"default","markup_percent":"20","expected_version":-1}]}`,
		`{"prices":[{"sales_group":"private","markup_percent":"20"}]}`,
		`{"prices":[{"sales_group":"default","markup_percent":"20"},{"sales_group":"default","markup_percent":"30"}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			response := resellerPricesHTTP(t, 42, 7, http.MethodPatch, "/prices", body)
			assert.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		})
	}
	var count int64
	require.NoError(t, db.Model(&model.TenantPriceVersion{}).Count(&count).Error)
	assert.Zero(t, count)
}
