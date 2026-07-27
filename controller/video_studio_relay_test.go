package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newVideoStudioRelayTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:video-relay-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.KKAIIdempotencyKey{}))
	return db
}

func TestPrepareVideoStudioTaskRequestReplaysBeforeStorageAndDistribution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newVideoStudioRelayTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	now := time.Now().Unix()
	task := model.Task{
		TaskID: "task_existing", UserId: 7, Status: model.TaskStatusQueued, Progress: "20%",
		Properties: model.Properties{OriginModelName: "video-model-v1"}, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&task).Error)
	quoteHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	quoteExpiresAt := now - 60
	fingerprint, err := service.VideoStudioIdempotencyFingerprint(service.VideoStudioSubmissionRequest{
		Model: "disabled-or-missing-model", Mode: service.VideoModeTextToVideo, Prompt: "test",
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.KKAIIdempotencyKey{
		UserID: 7, Operation: model.VideoIdempotencyOperationSubmit, Key: "retry-key",
		RequestHash: fingerprint, ResourceType: model.VideoIdempotencyResourceTask,
		ResourceID: task.TaskID, CreatedAt: now, ExpiresAt: now + 3600,
	}).Error)

	body, err := common.Marshal(map[string]any{
		"model": "disabled-or-missing-model", "mode": "text_to_video", "prompt": "test",
		"max_quota": 1, "quote_hash": quoteHash, "quote_expires_at": quoteExpiresAt,
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/pg/videos", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "retry-key")
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	ctx.Set("id", 7)

	PrepareVideoStudioTaskRequest(ctx)

	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"task_existing"`)
	require.Contains(t, response.Body.String(), `"queued"`)
}

func TestPrepareVideoStudioTaskRequestRejectsCopiedQuoteForDifferentCreativeRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newVideoStudioRelayTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	quoteHash := strings.Repeat("a", 64)
	original := service.VideoStudioSubmissionRequest{
		Model: "video-model-v1", Mode: service.VideoModeTextToVideo, Prompt: "original prompt",
	}
	fingerprint, err := service.VideoStudioIdempotencyFingerprint(original)
	require.NoError(t, err)
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.KKAIIdempotencyKey{
		UserID: 7, Operation: model.VideoIdempotencyOperationSubmit, Key: "copied-quote-key",
		RequestHash: fingerprint, ResourceType: model.VideoIdempotencyResourceTask,
		ResourceID: "task_original", CreatedAt: now, ExpiresAt: now + 3600,
	}).Error)

	body, err := common.Marshal(map[string]any{
		"model": "video-model-v1", "mode": "text_to_video", "prompt": "different prompt",
		"max_quota": 1, "quote_hash": quoteHash, "quote_expires_at": now + 300,
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/pg/videos", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "copied-quote-key")
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	ctx.Set("id", 7)

	PrepareVideoStudioTaskRequest(ctx)

	require.Equal(t, http.StatusConflict, response.Code)
	require.Contains(t, response.Body.String(), "video_studio_conflict")
}

func TestPrepareVideoStudioTaskRequestRejectsExpiredQuoteBeforeStorageAndReleasesReservation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newVideoStudioRelayTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	now := time.Now().Unix()
	body, err := common.Marshal(map[string]any{
		"model": "missing-model", "mode": "text_to_video", "prompt": "test",
		"max_quota": 1, "quote_hash": strings.Repeat("a", 64), "quote_expires_at": now - 1,
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/pg/videos", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "expired-quote-key")
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	ctx.Set("id", 7)

	PrepareVideoStudioTaskRequest(ctx)

	require.Equal(t, http.StatusConflict, response.Code)
	require.Contains(t, response.Body.String(), "quote_stale")
	var reservations int64
	require.NoError(t, db.Model(&model.KKAIIdempotencyKey{}).Count(&reservations).Error)
	require.Zero(t, reservations)
}

func TestPrepareVideoStudioTaskRequestRequiresSubmissionGuardsBeforeStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newVideoStudioRelayTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	body, err := common.Marshal(map[string]any{
		"model": "video-model-v1", "mode": "text_to_video", "prompt": "test", "max_quota": 1,
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/pg/videos", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	ctx.Set("id", 7)

	PrepareVideoStudioTaskRequest(ctx)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), "idempotency_key_required")
}
