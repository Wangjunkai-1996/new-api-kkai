package controller

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const kkaiBalanceTestSecret = "test-invitations-internal-secret-000000000001"

type kkaiBalanceTestResponse struct {
	Success bool                                   `json:"success"`
	Data    *service.KKAIBalanceAdjustmentResponse `json:"data"`
	Error   *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func setupKKAIBalanceAdjustmentTest(t *testing.T) (*gorm.DB, http.Handler) {
	t.Helper()
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalRedisEnabled := common.RedisEnabled
	originalRole := common.CurrentNodeRole()
	originalDisableBackgroundTasks := os.Getenv("DISABLE_BACKGROUND_TASKS")
	originalNodeRole := os.Getenv(common.NodeRoleEnvironmentVariable)

	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetMainDatabaseType(originalMainDatabaseType)
		common.SetLogDatabaseType(originalLogDatabaseType)
		common.RedisEnabled = originalRedisEnabled
		_ = os.Setenv(common.NodeRoleEnvironmentVariable, string(originalRole))
		_ = os.Setenv("DISABLE_BACKGROUND_TASKS", originalDisableBackgroundTasks)
		_ = common.InitNodeRoleFromEnvironment()
		if originalNodeRole == "" {
			_ = os.Unsetenv(common.NodeRoleEnvironmentVariable)
		} else {
			_ = os.Setenv(common.NodeRoleEnvironmentVariable, originalNodeRole)
		}
	})

	gin.SetMode(gin.TestMode)
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	t.Setenv(middleware.KKAIInvitationsInternalSecretEnv, kkaiBalanceTestSecret)
	t.Setenv(common.NodeRoleEnvironmentVariable, string(common.NodeRoleLeader))
	t.Setenv("DISABLE_BACKGROUND_TASKS", "false")
	require.NoError(t, common.InitNodeRoleFromEnvironment())

	dsn := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared&_pragma=busy_timeout(5000)",
		strings.ReplaceAll(t.Name(), "/", "_"),
	)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}))
	_, err = kkaimigrate.Apply(context.Background(), db, kkaimigrate.Options{})
	require.NoError(t, err)

	router := gin.New()
	router.POST(
		"/api/internal/balance-adjustments",
		middleware.KKAIBalanceAdjustmentAuth(),
		CreateKKAIBalanceAdjustment,
	)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db, router
}

func seedKKAIBalanceUser(t *testing.T, db *gorm.DB, id int, quota int64) {
	t.Helper()
	require.NoError(t, db.Create(&model.User{
		Id:       id,
		Username: fmt.Sprintf("kkai-balance-%d", id),
		Password: "not-a-real-password",
		Quota:    quota,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  fmt.Sprintf("kkai-aff-%d", id),
	}).Error)
}

func performKKAIBalanceRequest(
	t *testing.T,
	router http.Handler,
	secret string,
	payload map[string]any,
) (*httptest.ResponseRecorder, kkaiBalanceTestResponse) {
	t.Helper()
	body, err := common.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/internal/balance-adjustments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	var response kkaiBalanceTestResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return recorder, response
}

func kkaiBalanceCreditPayload(operationID string, userID int, delta int64) map[string]any {
	return map[string]any{
		"operation_id": operationID,
		"user_id":      userID,
		"delta":        delta,
		"reason":       "invitation_reward",
		"metadata": map[string]any{
			"rebate_record_id": 9001,
			"payout_id":        7001,
		},
	}
}

func TestKKAIBalanceAdjustmentRequiresDedicatedSecretAndWritableNode(t *testing.T) {
	db, router := setupKKAIBalanceAdjustmentTest(t)
	seedKKAIBalanceUser(t, db, 101, 100)
	payload := kkaiBalanceCreditPayload("auth-credit-101", 101, 25)

	recorder, response := performKKAIBalanceRequest(t, router, "", payload)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.NotNil(t, response.Error)
	assert.Equal(t, "internal_auth_failed", response.Error.Code)

	recorder, response = performKKAIBalanceRequest(t, router, "wrong-secret-that-is-long-enough-000000000000", payload)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.NotNil(t, response.Error)
	assert.Equal(t, "internal_auth_failed", response.Error.Code)
	assert.NotContains(t, recorder.Body.String(), kkaiBalanceTestSecret)

	t.Setenv(middleware.KKAIInvitationsInternalSecretEnv, "short")
	recorder, response = performKKAIBalanceRequest(t, router, "short", payload)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.NotNil(t, response.Error)
	assert.Equal(t, "internal_auth_unavailable", response.Error.Code)

	t.Setenv(middleware.KKAIInvitationsInternalSecretEnv, kkaiBalanceTestSecret)
	t.Setenv(common.NodeRoleEnvironmentVariable, string(common.NodeRoleStandbyReadonly))
	require.NoError(t, common.InitNodeRoleFromEnvironment())
	recorder, response = performKKAIBalanceRequest(t, router, kkaiBalanceTestSecret, payload)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.NotNil(t, response.Error)
	assert.Equal(t, "internal_write_unavailable", response.Error.Code)

	var quota int
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 101).Select("quota").Scan(&quota).Error)
	assert.Equal(t, 100, quota)
}

func TestKKAIBalanceAdjustmentAppliesOnceAndRejectsPayloadConflict(t *testing.T) {
	db, router := setupKKAIBalanceAdjustmentTest(t)
	seedKKAIBalanceUser(t, db, 201, 100)
	payload := kkaiBalanceCreditPayload("credit-once-201", 201, 25)

	firstRecorder, first := performKKAIBalanceRequest(t, router, kkaiBalanceTestSecret, payload)
	require.Equal(t, http.StatusCreated, firstRecorder.Code)
	require.NotNil(t, first.Data)
	assert.False(t, first.Data.Replayed)
	assert.Equal(t, int64(100), first.Data.BalanceBefore)
	assert.Equal(t, int64(125), first.Data.BalanceAfter)

	replayRecorder, replay := performKKAIBalanceRequest(t, router, kkaiBalanceTestSecret, payload)
	require.Equal(t, http.StatusOK, replayRecorder.Code)
	require.NotNil(t, replay.Data)
	assert.True(t, replay.Data.Replayed)
	assert.Equal(t, first.Data.AdjustmentID, replay.Data.AdjustmentID)

	payload["delta"] = int64(26)
	conflictRecorder, conflict := performKKAIBalanceRequest(t, router, kkaiBalanceTestSecret, payload)
	require.Equal(t, http.StatusConflict, conflictRecorder.Code)
	require.NotNil(t, conflict.Error)
	assert.Equal(t, "idempotency_conflict", conflict.Error.Code)

	var user model.User
	require.NoError(t, db.First(&user, 201).Error)
	assert.Equal(t, int64(125), user.Quota)
	var count int64
	require.NoError(t, db.Model(&model.KKAIInternalBalanceAdjustment{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestKKAIBalanceAdjustmentConcurrentReplayChangesBalanceOnce(t *testing.T) {
	db, router := setupKKAIBalanceAdjustmentTest(t)
	seedKKAIBalanceUser(t, db, 301, 100)
	payload := kkaiBalanceCreditPayload("concurrent-credit-301", 301, 25)

	const requests = 8
	statuses := make(chan int, requests)
	var waitGroup sync.WaitGroup
	for range requests {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			body, err := common.Marshal(payload)
			if err != nil {
				statuses <- 0
				return
			}
			req := httptest.NewRequest(http.MethodPost, "/api/internal/balance-adjustments", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+kkaiBalanceTestSecret)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			statuses <- recorder.Code
		}()
	}
	waitGroup.Wait()
	close(statuses)

	created := 0
	replayed := 0
	for status := range statuses {
		switch status {
		case http.StatusCreated:
			created++
		case http.StatusOK:
			replayed++
		default:
			t.Fatalf("unexpected response status %d", status)
		}
	}
	assert.Equal(t, 1, created)
	assert.Equal(t, requests-1, replayed)

	var user model.User
	require.NoError(t, db.First(&user, 301).Error)
	assert.Equal(t, int64(125), user.Quota)
}

func TestKKAIBalanceAdjustmentCreditsBalanceAboveMaxInt32(t *testing.T) {
	db, router := setupKKAIBalanceAdjustmentTest(t)
	startingQuota := int64(math.MaxInt32) + 100
	seedKKAIBalanceUser(t, db, 302, startingQuota)

	recorder, response := performKKAIBalanceRequest(
		t,
		router,
		kkaiBalanceTestSecret,
		kkaiBalanceCreditPayload("bigint-credit-302", 302, 25),
	)

	require.Equal(t, http.StatusCreated, recorder.Code)
	require.NotNil(t, response.Data)
	assert.Equal(t, startingQuota, response.Data.BalanceBefore)
	assert.Equal(t, startingQuota+25, response.Data.BalanceAfter)
}

func TestKKAIBalanceAdjustmentRequiresExactReversalAndRollsBackFailedDebit(t *testing.T) {
	db, router := setupKKAIBalanceAdjustmentTest(t)
	seedKKAIBalanceUser(t, db, 401, 100)

	creditRecorder, _ := performKKAIBalanceRequest(
		t,
		router,
		kkaiBalanceTestSecret,
		kkaiBalanceCreditPayload("credit-to-reverse-401", 401, 40),
	)
	require.Equal(t, http.StatusCreated, creditRecorder.Code)

	reversal := map[string]any{
		"operation_id": "reversal-401",
		"user_id":      401,
		"delta":        int64(-40),
		"reason":       "invitation_reward_reversal",
		"metadata": map[string]any{
			"original_operation_id": "credit-to-reverse-401",
		},
	}
	reversalRecorder, reversalResponse := performKKAIBalanceRequest(t, router, kkaiBalanceTestSecret, reversal)
	require.Equal(t, http.StatusCreated, reversalRecorder.Code)
	require.NotNil(t, reversalResponse.Data)
	assert.Equal(t, int64(140), reversalResponse.Data.BalanceBefore)
	assert.Equal(t, int64(100), reversalResponse.Data.BalanceAfter)

	reversal["operation_id"] = "second-reversal-401"
	secondRecorder, second := performKKAIBalanceRequest(t, router, kkaiBalanceTestSecret, reversal)
	require.Equal(t, http.StatusConflict, secondRecorder.Code)
	require.NotNil(t, second.Error)
	assert.Equal(t, "reversal_conflict", second.Error.Code)

	creditRecorder, _ = performKKAIBalanceRequest(
		t,
		router,
		kkaiBalanceTestSecret,
		kkaiBalanceCreditPayload("spent-credit-401", 401, 40),
	)
	require.Equal(t, http.StatusCreated, creditRecorder.Code)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 401).Update("quota", 0).Error)
	failedReversal := map[string]any{
		"operation_id": "insufficient-reversal-401",
		"user_id":      401,
		"delta":        int64(-40),
		"reason":       "invitation_reward_reversal",
		"metadata": map[string]any{
			"original_operation_id": "spent-credit-401",
		},
	}
	failedRecorder, failed := performKKAIBalanceRequest(t, router, kkaiBalanceTestSecret, failedReversal)
	require.Equal(t, http.StatusUnprocessableEntity, failedRecorder.Code)
	require.NotNil(t, failed.Error)
	assert.Equal(t, "insufficient_balance", failed.Error.Code)

	var count int64
	require.NoError(t, db.Model(&model.KKAIInternalBalanceAdjustment{}).
		Where("operation_id = ?", "insufficient-reversal-401").Count(&count).Error)
	assert.Zero(t, count)
}

func TestKKAIBalanceAdjustmentRejectsInvalidNotFoundAndOverflowRequests(t *testing.T) {
	db, router := setupKKAIBalanceAdjustmentTest(t)
	seedKKAIBalanceUser(t, db, 501, math.MaxInt64)

	tests := []struct {
		name       string
		payload    map[string]any
		wantStatus int
		wantCode   string
	}{
		{
			name: "unknown metadata",
			payload: map[string]any{
				"operation_id": "unknown-metadata-501",
				"user_id":      501,
				"delta":        int64(10),
				"reason":       "invitation_reward",
				"metadata":     map[string]any{"raw_upstream_body": "rejected"},
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name: "credit with negative delta",
			payload: map[string]any{
				"operation_id": "negative-credit-501",
				"user_id":      501,
				"delta":        int64(-10),
				"reason":       "invitation_reward",
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name: "reversal without original operation",
			payload: map[string]any{
				"operation_id": "orphan-reversal-501",
				"user_id":      501,
				"delta":        int64(-10),
				"reason":       "invitation_reward_reversal",
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name:       "missing user",
			payload:    kkaiBalanceCreditPayload("missing-user-999", 999, 10),
			wantStatus: http.StatusNotFound,
			wantCode:   "user_not_found",
		},
		{
			name:       "balance overflow",
			payload:    kkaiBalanceCreditPayload("overflow-user-501", 501, 1),
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "balance_overflow",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder, response := performKKAIBalanceRequest(t, router, kkaiBalanceTestSecret, test.payload)
			require.Equal(t, test.wantStatus, recorder.Code)
			require.NotNil(t, response.Error)
			assert.Equal(t, test.wantCode, response.Error.Code)
		})
	}
}
