package controller

import (
	"bytes"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const internalBalanceTestSecret = "test-invitations-internal-secret-000000000001"

type internalBalanceTestResponse struct {
	Success bool `json:"success"`
	Data    *struct {
		AdjustmentID  int64  `json:"adjustment_id"`
		OperationID   string `json:"operation_id"`
		UserID        int    `json:"user_id"`
		Delta         int64  `json:"delta"`
		Reason        string `json:"reason"`
		BalanceBefore int64  `json:"balance_before"`
		BalanceAfter  int64  `json:"balance_after"`
		CreatedAt     int64  `json:"created_at"`
		Replayed      bool   `json:"replayed"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func setupInternalBalanceAdjustmentTest(t *testing.T) (*gorm.DB, *gin.Engine) {
	t.Helper()

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalRedisEnabled := common.RedisEnabled
	originalRedisClient := common.RDB
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		model.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedisClient
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})

	gin.SetMode(gin.TestMode)
	model.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.RDB = nil
	common.MemoryCacheEnabled = false
	t.Setenv("INVITATIONS_INTERNAL_SECRET", internalBalanceTestSecret)

	dsn := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared&_pragma=busy_timeout(5000)",
		strings.ReplaceAll(t.Name(), "/", "_"),
	)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.InternalBalanceAdjustment{}))

	router := gin.New()
	router.POST(
		"/api/internal/balance-adjustments",
		middleware.InternalBalanceAdjustmentAuth(),
		CreateInternalBalanceAdjustment,
	)

	t.Cleanup(func() {
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db, router
}

func seedInternalBalanceUser(t *testing.T, db *gorm.DB, id int, quota int) {
	t.Helper()
	require.NoError(t, db.Create(&model.User{
		Id:       id,
		Username: fmt.Sprintf("internal-balance-%d", id),
		Password: "not-a-real-password",
		Quota:    quota,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  fmt.Sprintf("internal-aff-%d", id),
	}).Error)
}

func performInternalBalanceRequest(
	t *testing.T,
	router http.Handler,
	secret string,
	payload map[string]any,
	headers map[string]string,
) (*httptest.ResponseRecorder, internalBalanceTestResponse) {
	t.Helper()
	body, err := common.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/internal/balance-adjustments",
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	var response internalBalanceTestResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return recorder, response
}

func internalBalanceCreditPayload(operationID string, userID int, delta int64) map[string]any {
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

func TestInternalBalanceAdjustmentAuthenticatesOnlyDedicatedBearerSecret(t *testing.T) {
	db, router := setupInternalBalanceAdjustmentTest(t)
	seedInternalBalanceUser(t, db, 101, 100)

	recorder, response := performInternalBalanceRequest(
		t,
		router,
		"",
		internalBalanceCreditPayload("auth-missing", 101, 25),
		map[string]string{"New-API-User": "101"},
	)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.NotNil(t, response.Error)
	assert.Equal(t, "internal_auth_failed", response.Error.Code)

	recorder, response = performInternalBalanceRequest(
		t,
		router,
		"wrong-secret-that-is-long-enough-000000000000",
		internalBalanceCreditPayload("auth-wrong", 101, 25),
		nil,
	)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.NotNil(t, response.Error)
	assert.Equal(t, "internal_auth_failed", response.Error.Code)
	assert.NotContains(t, recorder.Body.String(), internalBalanceTestSecret)
	assert.NotContains(t, recorder.Body.String(), "wrong-secret")

	t.Setenv("INVITATIONS_INTERNAL_SECRET", "short")
	recorder, response = performInternalBalanceRequest(
		t,
		router,
		"short",
		internalBalanceCreditPayload("auth-weak-config", 101, 25),
		nil,
	)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.NotNil(t, response.Error)
	assert.Equal(t, "internal_auth_unavailable", response.Error.Code)

	var quota int
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 101).Select("quota").Scan(&quota).Error)
	assert.Equal(t, 100, quota)
}

func TestInternalBalanceAdjustmentAppliesOnceAndReplaysStoredResult(t *testing.T) {
	db, router := setupInternalBalanceAdjustmentTest(t)
	seedInternalBalanceUser(t, db, 201, 100)
	seedInternalBalanceUser(t, db, 202, 500)
	payload := internalBalanceCreditPayload("credit-once-201", 201, 25)

	firstRecorder, first := performInternalBalanceRequest(
		t,
		router,
		internalBalanceTestSecret,
		payload,
		map[string]string{"New-API-User": "202"},
	)
	require.Equal(t, http.StatusCreated, firstRecorder.Code)
	require.True(t, first.Success)
	require.NotNil(t, first.Data)
	assert.False(t, first.Data.Replayed)
	assert.Equal(t, int64(100), first.Data.BalanceBefore)
	assert.Equal(t, int64(125), first.Data.BalanceAfter)

	replayRecorder, replay := performInternalBalanceRequest(
		t,
		router,
		internalBalanceTestSecret,
		payload,
		map[string]string{"New-API-User": "999999"},
	)
	require.Equal(t, http.StatusOK, replayRecorder.Code)
	require.True(t, replay.Success)
	require.NotNil(t, replay.Data)
	assert.True(t, replay.Data.Replayed)
	assert.Equal(t, first.Data.AdjustmentID, replay.Data.AdjustmentID)
	assert.Equal(t, first.Data.BalanceAfter, replay.Data.BalanceAfter)

	var users []model.User
	require.NoError(t, db.Where("id IN ?", []int{201, 202}).Order("id").Find(&users).Error)
	require.Len(t, users, 2)
	assert.Equal(t, 125, users[0].Quota)
	assert.Equal(t, 500, users[1].Quota)

	var adjustmentCount int64
	require.NoError(t, db.Model(&model.InternalBalanceAdjustment{}).Count(&adjustmentCount).Error)
	assert.Equal(t, int64(1), adjustmentCount)
}

func TestInternalBalanceAdjustmentRejectsIdempotencyPayloadConflict(t *testing.T) {
	db, router := setupInternalBalanceAdjustmentTest(t)
	seedInternalBalanceUser(t, db, 301, 100)

	payload := internalBalanceCreditPayload("conflicting-credit-301", 301, 10)
	firstRecorder, _ := performInternalBalanceRequest(t, router, internalBalanceTestSecret, payload, nil)
	require.Equal(t, http.StatusCreated, firstRecorder.Code)

	payload["delta"] = int64(11)
	conflictRecorder, conflict := performInternalBalanceRequest(
		t,
		router,
		internalBalanceTestSecret,
		payload,
		nil,
	)
	require.Equal(t, http.StatusConflict, conflictRecorder.Code)
	require.NotNil(t, conflict.Error)
	assert.Equal(t, "idempotency_conflict", conflict.Error.Code)

	var user model.User
	require.NoError(t, db.First(&user, 301).Error)
	assert.Equal(t, 110, user.Quota)
}

func TestInternalBalanceAdjustmentReversalMustMatchOriginalCredit(t *testing.T) {
	db, router := setupInternalBalanceAdjustmentTest(t)
	seedInternalBalanceUser(t, db, 401, 100)

	creditRecorder, _ := performInternalBalanceRequest(
		t,
		router,
		internalBalanceTestSecret,
		internalBalanceCreditPayload("credit-to-reverse-401", 401, 40),
		nil,
	)
	require.Equal(t, http.StatusCreated, creditRecorder.Code)

	reversal := map[string]any{
		"operation_id": "reversal-401",
		"user_id":      401,
		"delta":        int64(-40),
		"reason":       "invitation_reward_reversal",
		"metadata": map[string]any{
			"rebate_record_id":      9001,
			"payout_id":             7002,
			"original_operation_id": "credit-to-reverse-401",
		},
	}
	reversalRecorder, reversalResponse := performInternalBalanceRequest(
		t,
		router,
		internalBalanceTestSecret,
		reversal,
		nil,
	)
	require.Equal(t, http.StatusCreated, reversalRecorder.Code)
	require.NotNil(t, reversalResponse.Data)
	assert.Equal(t, int64(140), reversalResponse.Data.BalanceBefore)
	assert.Equal(t, int64(100), reversalResponse.Data.BalanceAfter)

	reversal["operation_id"] = "second-reversal-401"
	secondRecorder, second := performInternalBalanceRequest(
		t,
		router,
		internalBalanceTestSecret,
		reversal,
		nil,
	)
	require.Equal(t, http.StatusConflict, secondRecorder.Code)
	require.NotNil(t, second.Error)
	assert.Equal(t, "reversal_conflict", second.Error.Code)

	var user model.User
	require.NoError(t, db.First(&user, 401).Error)
	assert.Equal(t, 100, user.Quota)
}

func TestInternalBalanceAdjustmentRollsBackFailedDebit(t *testing.T) {
	db, router := setupInternalBalanceAdjustmentTest(t)
	seedInternalBalanceUser(t, db, 501, 0)

	creditRecorder, _ := performInternalBalanceRequest(
		t,
		router,
		internalBalanceTestSecret,
		internalBalanceCreditPayload("spent-credit-501", 501, 40),
		nil,
	)
	require.Equal(t, http.StatusCreated, creditRecorder.Code)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 501).Update("quota", 0).Error)

	reversal := map[string]any{
		"operation_id": "insufficient-reversal-501",
		"user_id":      501,
		"delta":        int64(-40),
		"reason":       "invitation_reward_reversal",
		"metadata": map[string]any{
			"original_operation_id": "spent-credit-501",
		},
	}
	recorder, response := performInternalBalanceRequest(
		t,
		router,
		internalBalanceTestSecret,
		reversal,
		nil,
	)
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	require.NotNil(t, response.Error)
	assert.Equal(t, "insufficient_balance", response.Error.Code)

	var count int64
	require.NoError(t, db.Model(&model.InternalBalanceAdjustment{}).
		Where("operation_id = ?", "insufficient-reversal-501").Count(&count).Error)
	assert.Zero(t, count)
}

func TestInternalBalanceAdjustmentRejectsUnknownMetadataAndInvalidDirection(t *testing.T) {
	db, router := setupInternalBalanceAdjustmentTest(t)
	seedInternalBalanceUser(t, db, 601, 100)

	tests := []struct {
		name    string
		payload map[string]any
	}{
		{
			name: "unknown metadata",
			payload: map[string]any{
				"operation_id": "unknown-metadata-601",
				"user_id":      601,
				"delta":        int64(10),
				"reason":       "invitation_reward",
				"metadata": map[string]any{
					"raw_upstream_body": "must not be accepted",
				},
			},
		},
		{
			name: "credit with negative delta",
			payload: map[string]any{
				"operation_id": "wrong-direction-601",
				"user_id":      601,
				"delta":        int64(-10),
				"reason":       "invitation_reward",
			},
		},
		{
			name: "reversal without original operation",
			payload: map[string]any{
				"operation_id": "orphan-reversal-601",
				"user_id":      601,
				"delta":        int64(-10),
				"reason":       "invitation_reward_reversal",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder, response := performInternalBalanceRequest(
				t,
				router,
				internalBalanceTestSecret,
				test.payload,
				nil,
			)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.NotNil(t, response.Error)
			assert.Equal(t, "invalid_request", response.Error.Code)
		})
	}
}

func TestInternalBalanceAdjustmentReturnsNotFoundWithoutLedgerRow(t *testing.T) {
	db, router := setupInternalBalanceAdjustmentTest(t)
	recorder, response := performInternalBalanceRequest(
		t,
		router,
		internalBalanceTestSecret,
		internalBalanceCreditPayload("missing-user-credit", 999001, 25),
		nil,
	)
	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.NotNil(t, response.Error)
	assert.Equal(t, "user_not_found", response.Error.Code)

	var count int64
	require.NoError(t, db.Model(&model.InternalBalanceAdjustment{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestInternalBalanceAdjustmentEnforcesCrossDatabaseIntegerBounds(t *testing.T) {
	db, router := setupInternalBalanceAdjustmentTest(t)
	seedInternalBalanceUser(t, db, 651, math.MaxInt32-1)

	maxRecorder, maxResponse := performInternalBalanceRequest(
		t,
		router,
		internalBalanceTestSecret,
		internalBalanceCreditPayload("max-balance-651", 651, 1),
		nil,
	)
	require.Equal(t, http.StatusCreated, maxRecorder.Code)
	require.NotNil(t, maxResponse.Data)
	assert.Equal(t, int64(math.MaxInt32), maxResponse.Data.BalanceAfter)

	overflowRecorder, overflowResponse := performInternalBalanceRequest(
		t,
		router,
		internalBalanceTestSecret,
		internalBalanceCreditPayload("overflow-balance-651", 651, 1),
		nil,
	)
	require.Equal(t, http.StatusUnprocessableEntity, overflowRecorder.Code)
	require.NotNil(t, overflowResponse.Error)
	assert.Equal(t, "balance_overflow", overflowResponse.Error.Code)

	tooLarge := internalBalanceCreditPayload("too-large-delta-651", 651, int64(math.MaxInt32)+1)
	tooLargeRecorder, tooLargeResponse := performInternalBalanceRequest(
		t,
		router,
		internalBalanceTestSecret,
		tooLarge,
		nil,
	)
	require.Equal(t, http.StatusBadRequest, tooLargeRecorder.Code)
	require.NotNil(t, tooLargeResponse.Error)
	assert.Equal(t, "invalid_request", tooLargeResponse.Error.Code)

	var count int64
	require.NoError(t, db.Model(&model.InternalBalanceAdjustment{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestInternalBalanceAdjustmentConcurrentReplayCreditsOnlyOnce(t *testing.T) {
	db, router := setupInternalBalanceAdjustmentTest(t)
	seedInternalBalanceUser(t, db, 701, 100)
	payload := internalBalanceCreditPayload("concurrent-credit-701", 701, 50)

	const requests = 8
	statuses := make(chan int, requests)
	errorsByCode := make(chan string, requests)
	var waitGroup sync.WaitGroup
	for range requests {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			body, err := common.Marshal(payload)
			if err != nil {
				errorsByCode <- err.Error()
				return
			}
			req := httptest.NewRequest(
				http.MethodPost,
				"/api/internal/balance-adjustments",
				bytes.NewReader(body),
			)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+internalBalanceTestSecret)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			statuses <- recorder.Code
			if recorder.Code != http.StatusCreated && recorder.Code != http.StatusOK {
				errorsByCode <- recorder.Body.String()
			}
		}()
	}
	waitGroup.Wait()
	close(statuses)
	close(errorsByCode)

	var created int
	for status := range statuses {
		if status == http.StatusCreated {
			created++
		}
	}
	var requestErrors []string
	for requestError := range errorsByCode {
		requestErrors = append(requestErrors, requestError)
	}
	assert.Empty(t, requestErrors)
	assert.Equal(t, 1, created)

	var user model.User
	require.NoError(t, db.First(&user, 701).Error)
	assert.Equal(t, 150, user.Quota)
	var adjustmentCount int64
	require.NoError(t, db.Model(&model.InternalBalanceAdjustment{}).Count(&adjustmentCount).Error)
	assert.Equal(t, int64(1), adjustmentCount)
}
