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

func TestAdminRedriveVideoStudioOutboxEventRestoresAggregateAndAudits(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalRedisEnabled := common.RedisEnabled
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetMainDatabaseType(originalMainDatabaseType)
		common.SetLogDatabaseType(originalLogDatabaseType)
		common.RedisEnabled = originalRedisEnabled
	})

	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Log{}, &model.KKAIOutboxEvent{}, &model.KKAIVideoAsset{},
	))
	require.NoError(t, db.Create(&model.User{
		Id: 42, Username: "video-admin", Password: "not-a-real-password",
		Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "video-admin-aff",
	}).Error)
	now := time.Now().Unix()
	asset := model.KKAIVideoAsset{
		OwnerUserID: 9, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
		State: model.VideoAssetStateFailed, ObjectKey: "video/output.mp4", MIMEType: "video/mp4",
		FailureReason: "media processing failed", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)
	payload, err := common.Marshal(service.VideoAssetEventPayload{AssetID: asset.ID})
	require.NoError(t, err)
	event := model.KKAIOutboxEvent{
		EventKey: "video-admin-redrive", Topic: service.VideoOutboxTopicPoster,
		AggregateID: fmt.Sprintf("%d", asset.ID), Payload: string(payload),
		Status: model.KKAIOutboxStatusDead, Attempts: 12, AvailableAt: now,
		LastError: "poster dependency unavailable", CreatedAt: now,
	}
	require.NoError(t, db.Create(&event).Error)

	body, err := common.Marshal(map[string]string{"redrive_key": "admin-retry-1"})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/admin/video-studio/outbox/%d/redrive", event.ID),
		bytes.NewReader(body),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", event.ID)}}
	ctx.Set("id", 42)
	ctx.Set("username", "video-admin")
	ctx.Set("role", common.RoleAdminUser)

	AdminRedriveVideoStudioOutboxEvent(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), event.Payload)
	require.NotContains(t, recorder.Body.String(), event.LastError)
	var response struct {
		Success bool                       `json:"success"`
		Data    videoOutboxRedriveResponse `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.True(t, response.Data.Applied)
	require.Equal(t, event.ID, response.Data.ID)
	require.Equal(t, model.KKAIOutboxStatusPending, response.Data.Status)

	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Equal(t, model.VideoAssetStateProcessing, asset.State)
	require.Empty(t, asset.FailureReason)
	require.NoError(t, db.First(&event, event.ID).Error)
	require.Equal(t, model.KKAIOutboxStatusPending, event.Status)
	require.Zero(t, event.Attempts)

	var audit model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeManage).First(&audit).Error)
	var other map[string]interface{}
	require.NoError(t, common.UnmarshalJsonStr(audit.Other, &other))
	op, ok := other["op"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "video.outbox.redrive", op["action"])
	params, ok := op["params"].(map[string]interface{})
	require.True(t, ok)
	require.EqualValues(t, event.ID, params["event_id"])
	require.Equal(t, service.VideoOutboxTopicPoster, params["topic"])
	require.Equal(t, "admin-retry-1", params["redrive_key"])
	require.Equal(t, true, params["applied"])
}
