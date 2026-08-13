package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestProcessKKAIPolicyAPIErrorAuditsWithoutDisablingAfterClientDisconnect(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:controller-policy-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Token{},
		&model.Channel{},
		&model.KKAIPolicyIncident{},
		&model.KKAIOutboxEvent{},
	))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })

	user := model.User{Username: "controller-policy-user", Password: "unused", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "controller-policy-token", Name: "policy token", Status: common.TokenStatusEnabled}
	require.NoError(t, db.Create(&token).Error)
	channel := model.Channel{Name: "controller-policy-channel", Key: "controller-policy-upstream", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&channel).Error)

	requestCtx, cancel := context.WithCancel(context.Background())
	cancel()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestCtx)
	c.Set(common.RequestIdKey, "req-controller-policy-disconnect")
	c.Set("id", user.Id)
	c.Set("token_id", token.Id)
	c.Set("token_key", token.Key)
	c.Set("original_model", "gpt-test")
	apiErr := types.NewErrorWithStatusCode(
		errors.New("request rejected"),
		types.ErrorCode("cyber_policy"),
		http.StatusForbidden,
		types.ErrOptionWithOriginalStatusCode(http.StatusForbidden),
		types.ErrOptionWithOriginalErrorCode(types.ErrorCode("cyber_policy")),
		types.ErrOptionWithPolicyEvidence("cyber_policy"),
	)

	detected := processKKAIPolicyAPIError(
		c,
		*types.NewChannelError(channel.Id, 1, channel.Name, false, channel.Key, true),
		apiErr,
	)

	require.True(t, detected)
	assert.True(t, service.ShouldSkipRetryAfterKKAIPolicy(c))
	assert.True(t, service.KKAIPolicyKeyCooldownApplied(c))
	status, publicErr := kkaiPublicOpenAIError(c, apiErr)
	assert.Equal(t, http.StatusForbidden, status)
	assert.Equal(t, service.KKAIPolicyMessageForCyber(), publicErr.Message)
	require.NoError(t, db.First(&token, token.Id).Error)
	assert.Equal(t, common.TokenStatusEnabled, token.Status)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, user.Status)
	require.NoError(t, db.First(&channel, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
	var incidents int64
	require.NoError(t, db.Model(&model.KKAIPolicyIncident{}).Count(&incidents).Error)
	assert.EqualValues(t, 1, incidents)
	var incident model.KKAIPolicyIncident
	require.NoError(t, db.First(&incident).Error)
	assert.Equal(t, service.RiskDecisionReject, incident.Decision)
	assert.Equal(t, "record_incident", incident.ActionTaken)
	assert.False(t, incident.TokenDisabled)
	assert.False(t, incident.UserDisabled)
}
