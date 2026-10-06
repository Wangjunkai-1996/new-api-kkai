package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRequestPolicyAPIAtomicUpdate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	previousDB, previousOptions := model.DB, model.CurrentRequestPolicy().Options
	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string, len(previousOptionMap))
	for key, value := range previousOptionMap {
		common.OptionMap[key] = value
	}
	common.OptionMapRWMutex.Unlock()
	model.DB = db
	t.Cleanup(func() {
		require.NoError(t, model.UpdateRequestPolicyOptions(previousOptions))
		model.DB = previousDB
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, model.UpdateRequestPolicyOptions(map[string]string{"RetryTimes": "2"}))
	engine := gin.New()
	engine.GET("/api/option/request_policy", GetRequestPolicy)
	engine.PATCH("/api/option/request_policy", UpdateRequestPolicy)
	for _, test := range []struct {
		body        string
		status      int
		wantRetries string
	}{
		{`{"options":{"RetryTimes":"4","channel_affinity_setting.session_mode":"unknown"}}`, http.StatusBadRequest, "2"},
		{`{"options":{"RetryTimes":"4","channel_affinity_setting.session_mode":"strict"}}`, http.StatusOK, "4"},
		{`{"options":{"RetryTimes":"-1"}}`, http.StatusBadRequest, "4"},
		{`{"options":{"ServerAddress":"https://example.invalid"}}`, http.StatusBadRequest, "4"},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPatch, "/api/option/request_policy", strings.NewReader(test.body))
		request.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(recorder, request)
		assert.Equal(t, test.status, recorder.Code, recorder.Body.String())
		var stored model.Option
		require.NoError(t, db.Where(map[string]any{"key": "RetryTimes"}).First(&stored).Error)
		assert.Equal(t, test.wantRetries, stored.Value)
		get := httptest.NewRecorder()
		engine.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/option/request_policy", nil))
		var response struct {
			Data struct {
				Options map[string]string `json:"options"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(get.Body.Bytes(), &response))
		assert.Equal(t, test.wantRetries, response.Data.Options["RetryTimes"])
	}
}

func TestRequestPolicyRetryModes(t *testing.T) {
	settings := operation_setting.GetChannelAffinitySetting()
	previous := *settings
	t.Cleanup(func() { *settings = previous })
	for _, mode := range []string{"off", "prefer", "strict"} {
		t.Run(mode, func(t *testing.T) {
			settings.Enabled = true
			settings.SessionMode = mode
			settings.Rules = []operation_setting.ChannelAffinityRule{{Name: "policy", ModelRegex: []string{".*"}, KeySources: []operation_setting.ChannelAffinityKeySource{{Type: "request_header", Key: "X-Session"}}, SessionMode: "inherit"}}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			ctx.Request.Header.Set("X-Session", t.Name())
			service.GetPreferredChannelByAffinity(ctx, "policy-model", "default")
			apiErr := types.NewOpenAIError(errors.New("upstream unavailable"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
			decision := decideRelayRetry(ctx, nil, apiErr, 2)
			taskDecision := decideTaskRetry(ctx, &dto.TaskError{StatusCode: http.StatusBadGateway}, 2)
			if mode == "strict" {
				assert.Equal(t, "strict_session", decision.Reason)
				assert.Equal(t, "stop", decision.Action)
				assert.Equal(t, "global", decision.Source)
				assert.Equal(t, "strict_session", taskDecision.Reason)
			} else {
				assert.Equal(t, "retry", decision.Action)
				assert.Equal(t, "retry", taskDecision.Action)
			}
			service.RequestPolicy(ctx).BeginAttempt(&model.Channel{Id: 9}, "default")
			service.RecordPolicyFailure(ctx, 9, apiErr, decision)
			adminInfo := map[string]any{}
			service.AppendChannelAffinityAdminInfo(ctx, adminInfo)
			events, ok := adminInfo["request_policy"].([]service.PolicyEvent)
			require.True(t, ok)
			require.NotEmpty(t, events)
			assert.Equal(t, decision, events[len(events)-1].Decision)
		})
	}
}
