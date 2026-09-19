package controller

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTokenGroupTest(t *testing.T) *model.Token {
	t.Helper()
	db := setupTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "token-group-user",
		Password: "unused-password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)

	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	originalAutoGroups := setting.AutoGroups2JsonString()
	originalProfiles := setting.AutoGroupProfiles2JsonString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP","auto":"Auto","auto2":"Auto 2"," premium ":"Spaced"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1," premium ":1}`))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default"]`))
	require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(`{"auto2":["vip","default"]}`))
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(originalProfiles))
	})

	return seedToken(t, db, 1, "group-token", "group-token-key")
}

func updateTokenGroup(t *testing.T, tokenID int, userID int, group string) tokenAPIResponse {
	t.Helper()
	return updateTokenGroupPayload(t, tokenID, userID, map[string]string{"group": group})
}

func updateTokenGroupPayload(t *testing.T, tokenID int, userID int, body any) tokenAPIResponse {
	t.Helper()
	ctx, recorder := newAuthenticatedContext(t, http.MethodPatch, "/api/token/"+strconv.Itoa(tokenID)+"/group", body, userID)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(tokenID)}}
	UpdateTokenGroup(ctx)
	return decodeAPIResponse(t, recorder)
}

func TestUpdateTokenGroupRequiresGroupField(t *testing.T) {
	token := setupTokenGroupTest(t)
	for _, body := range []any{map[string]any{}, map[string]any{"group": nil}} {
		response := updateTokenGroupPayload(t, token.Id, 1, body)
		assert.False(t, response.Success)
	}
	var stored model.Token
	require.NoError(t, model.DB.First(&stored, token.Id).Error)
	assert.Equal(t, "default", stored.Group)
}

func TestUpdateTokenGroupRejectsOtherUsersToken(t *testing.T) {
	token := setupTokenGroupTest(t)
	response := updateTokenGroup(t, token.Id, 2, "vip")

	assert.False(t, response.Success)
	var stored model.Token
	require.NoError(t, model.DB.First(&stored, token.Id).Error)
	assert.Equal(t, "default", stored.Group)
}

func TestUpdateTokenGroupRejectsUnavailableGroup(t *testing.T) {
	token := setupTokenGroupTest(t)
	response := updateTokenGroup(t, token.Id, 1, "private")

	assert.False(t, response.Success)
	var stored model.Token
	require.NoError(t, model.DB.First(&stored, token.Id).Error)
	assert.Equal(t, "default", stored.Group)
}

func TestUpdateTokenGroupRejectsUnauthorizedAutoAndUnpricedGroup(t *testing.T) {
	token := setupTokenGroupTest(t)
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP","unpriced":"Unpriced"}`))

	autoResponse := updateTokenGroup(t, token.Id, 1, "auto2")
	assert.False(t, autoResponse.Success)
	unpricedResponse := updateTokenGroup(t, token.Id, 1, "unpriced")
	assert.False(t, unpricedResponse.Success)
}

func TestUpdateTokenGroupPreservesFieldsAndClearsRetryForOrdinaryGroup(t *testing.T) {
	token := setupTokenGroupTest(t)
	token.Name = "keep-name"
	token.RemainQuota = 321
	token.ExpiredTime = 123456
	token.ModelLimitsEnabled = true
	token.ModelLimits = "gpt-4"
	token.CrossGroupRetry = true
	require.NoError(t, token.Update())

	response := updateTokenGroup(t, token.Id, 1, "vip")
	assert.True(t, response.Success, response.Message)
	var responseToken tokenResponseItem
	require.NoError(t, common.Unmarshal(response.Data, &responseToken))
	assert.Equal(t, token.GetMaskedKey(), responseToken.Key)

	var stored model.Token
	require.NoError(t, model.DB.First(&stored, token.Id).Error)
	assert.Equal(t, "vip", stored.Group)
	assert.False(t, stored.CrossGroupRetry)
	assert.Equal(t, "keep-name", stored.Name)
	assert.Equal(t, 321, stored.RemainQuota)
	assert.Equal(t, int64(123456), stored.ExpiredTime)
	assert.True(t, stored.ModelLimitsEnabled)
	assert.Equal(t, "gpt-4", stored.ModelLimits)
}

func TestUpdateTokenGroupPreservesRetryForAutoGroup(t *testing.T) {
	token := setupTokenGroupTest(t)
	token.CrossGroupRetry = true
	require.NoError(t, token.Update())

	response := updateTokenGroup(t, token.Id, 1, "auto2")
	assert.True(t, response.Success, response.Message)

	var stored model.Token
	require.NoError(t, model.DB.First(&stored, token.Id).Error)
	assert.Equal(t, "auto2", stored.Group)
	assert.True(t, stored.CrossGroupRetry)
}

func TestUpdateTokenGroupPreservesCanonicalGroupKey(t *testing.T) {
	token := setupTokenGroupTest(t)
	response := updateTokenGroup(t, token.Id, 1, " premium ")
	assert.True(t, response.Success, response.Message)

	var stored model.Token
	require.NoError(t, model.DB.First(&stored, token.Id).Error)
	assert.Equal(t, " premium ", stored.Group)
}

func TestUpdateTokenGroupAllowsEmptyGroup(t *testing.T) {
	token := setupTokenGroupTest(t)
	response := updateTokenGroup(t, token.Id, 1, "")
	assert.True(t, response.Success, response.Message)

	var stored model.Token
	require.NoError(t, model.DB.First(&stored, token.Id).Error)
	assert.Empty(t, stored.Group)
}
