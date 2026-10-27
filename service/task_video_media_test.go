package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Only local signing is implemented; any remote store operation fails the test.
type taskVideoSigningStore struct {
	VideoAssetStore
	calls []struct {
		key, filename string
		attachment    bool
	}
}

func (store *taskVideoSigningStore) PresignDownload(_ context.Context, key, filename string, attachment bool, _ time.Duration) (string, error) {
	store.calls = append(store.calls, struct {
		key, filename string
		attachment    bool
	}{key, filename, attachment})
	return fmt.Sprintf("https://signed.example/%s?attachment=%t", key, attachment), nil
}

func taskVideoOutputFixture(t *testing.T) (*gorm.DB, *model.Task, *model.KKAIVideoAsset) {
	t.Helper()
	db := newVideoPipelineTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.User{}))
	for _, user := range []model.User{
		{Id: 7, Username: "video-owner", Status: common.UserStatusEnabled, Role: common.RoleCommonUser},
		{Id: 8, Username: "video-admin", Status: common.UserStatusEnabled, Role: common.RoleAdminUser},
		{Id: 9, Username: "video-stranger", Status: common.UserStatusEnabled, Role: common.RoleCommonUser},
	} {
		user.AffCode = fmt.Sprintf("video%d", user.Id)
		require.NoError(t, db.Create(&user).Error)
	}
	task := &model.Task{TaskID: "hosted-video", UserId: 7, Status: model.TaskStatusSuccess}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, db.Create(&model.KKAIVideoGeneration{TaskID: task.ID, UserID: 7}).Error)
	asset := &model.KKAIVideoAsset{OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
		State: model.VideoAssetStateReady, ObjectKey: "video.mp4", PosterObjectKey: "poster.jpg",
		OriginalFilename: "clip.mp4", Width: 1920, Height: 1080, DurationSeconds: 6}
	require.NoError(t, db.Create(asset).Error)
	require.NoError(t, db.Create(&model.KKAIVideoTaskAsset{TaskID: task.ID, AssetID: asset.ID, Role: model.VideoTaskAssetRoleOutput}).Error)
	raw := config.GlobalConfig.Get("video_studio")
	original, err := config.ConfigToMap(raw)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, config.UpdateConfigFromMap(raw, original)) })
	require.NoError(t, config.UpdateConfigFromMap(raw, map[string]string{
		"access_mode": "all", "worker_enabled": "false", "archive_enqueue_enabled": "false", "backfill_enabled": "false",
	}))
	return db, task, asset
}

func TestTaskVideoOutputsReadsReadyMediaWithWorkersOff(t *testing.T) {
	db, task, asset := taskVideoOutputFixture(t)
	second := &model.Task{TaskID: "second-video", UserId: 7, Status: model.TaskStatusSuccess}
	require.NoError(t, db.Create(second).Error)
	require.NoError(t, db.Create(&model.KKAIVideoGeneration{TaskID: second.ID, UserID: 7}).Error)
	require.NoError(t, db.Create(&model.KKAIVideoTaskAsset{TaskID: second.ID, AssetID: asset.ID, Role: model.VideoTaskAssetRoleOutput}).Error)
	queries, writes := 0, 0
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("video_works_query_budget", func(*gorm.DB) { queries++ }))
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("video_works_read_only", func(*gorm.DB) { writes++ }))
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("video_works_read_only", func(*gorm.DB) { writes++ }))
	require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("video_works_read_only", func(*gorm.DB) { writes++ }))
	outputs, err := LoadTaskVideoOutputs(context.Background(), db, []*model.Task{task, second}, 7, false)
	require.NoError(t, err)
	assert.LessOrEqual(t, queries, 4, "page association must remain batched")
	require.Len(t, outputs, 2)
	store := &taskVideoSigningStore{}
	output := outputs[task.ID]
	require.Equal(t, "ready", output.Media.State)
	require.NoError(t, output.SignPoster(context.Background(), store))
	view, err := output.SignContent(context.Background(), store)
	require.NoError(t, err)
	assert.Equal(t, "https://signed.example/poster.jpg?attachment=false", output.Media.PosterURL)
	assert.Greater(t, output.Media.PosterExpiresAt, time.Now().Unix())
	assert.Equal(t, task.ID, view.TaskRecordID)
	assert.Equal(t, "https://signed.example/video.mp4?attachment=false", view.ContentURL)
	assert.Equal(t, "https://signed.example/video.mp4?attachment=true", view.DownloadURL)
	assert.Equal(t, 1920, view.Width)
	assert.Equal(t, 6.0, view.DurationSeconds)
	require.Len(t, store.calls, 3)
	assert.True(t, store.calls[2].attachment)
	assert.Equal(t, "clip.mp4", store.calls[2].filename)
	assert.Zero(t, writes, "reading a work cannot enqueue processing or change billing")
	var saved model.Task
	require.NoError(t, db.First(&saved, task.ID).Error)
	assert.False(t, saved.IsAssetHostedResult(), "legacy association recognition is not a backfill")
}

func TestTaskVideoOutputsEnforcesFreshOwnerAndViewerPermissions(t *testing.T) {
	for _, test := range []struct {
		name       string
		viewer     int
		admin      bool
		userID     int
		updates    map[string]any
		mode, want string
	}{
		{name: "owner", viewer: 7, want: "ready"},
		{name: "stranger", viewer: 9, want: "restricted"},
		{name: "admin task projection", viewer: 8, admin: true, want: "ready"},
		{name: "admin self scope", viewer: 8, want: "restricted"},
		{name: "disabled owner", viewer: 8, admin: true, userID: 7, updates: map[string]any{"status": common.UserStatusDisabled}, want: "restricted"},
		{name: "disabled viewer", viewer: 8, admin: true, userID: 8, updates: map[string]any{"status": common.UserStatusDisabled}, want: "restricted"},
		{name: "downgraded admin", viewer: 8, admin: true, userID: 8, updates: map[string]any{"role": common.RoleCommonUser}, want: "restricted"},
		{name: "tenant admin", viewer: 8, admin: true, userID: 8, updates: map[string]any{"tenant_id": int64(22)}, want: "restricted"},
		{name: "owner tenant changed", viewer: 7, userID: 7, updates: map[string]any{"tenant_id": int64(22)}, want: "restricted"},
		{name: "access disabled", viewer: 7, mode: "off", want: "restricted"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, task, asset := taskVideoOutputFixture(t)
			if test.userID != 0 {
				require.NoError(t, db.Model(&model.User{}).Where("id = ?", test.userID).Updates(test.updates).Error)
			}
			if test.mode != "" {
				require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("video_studio"), map[string]string{"access_mode": test.mode}))
			}
			outputs, err := LoadTaskVideoOutputs(context.Background(), db, []*model.Task{task}, test.viewer, test.admin)
			require.NoError(t, err)
			require.Equal(t, test.want, outputs[task.ID].Media.State)
			store := &taskVideoSigningStore{}
			view, err := outputs[task.ID].SignContent(context.Background(), store)
			require.NoError(t, err)
			if test.want != "ready" {
				assert.Empty(t, view.ContentURL)
				assert.Empty(t, view.DownloadURL)
				assert.Empty(t, store.calls)
			}
			_, err = GetAuthorizedVideoAsset(context.Background(), db, 8, true, asset.ID)
			assert.ErrorIs(t, err, ErrVideoAssetAccessDenied, "task projection never relaxes original Studio owner access")
		})
	}
}

func TestTaskVideoOutputsInvalidAssociationCannotExposeRawResult(t *testing.T) {
	for _, test := range []struct {
		name    string
		model   any
		updates map[string]any
	}{
		{"generation owner mismatch", &model.KKAIVideoGeneration{}, map[string]any{"user_id": 9}},
		{"deleted generation", &model.KKAIVideoGeneration{}, map[string]any{"deleted_at": 1}},
		{"asset owner mismatch", &model.KKAIVideoAsset{}, map[string]any{"owner_user_id": 9}},
		{"deleted asset", &model.KKAIVideoAsset{}, map[string]any{"deleted_at": 1}},
		{"catalog asset", &model.KKAIVideoAsset{}, map[string]any{"scope": model.VideoAssetScopeCatalog}},
		{"reference asset", &model.KKAIVideoAsset{}, map[string]any{"kind": model.VideoAssetKindReference}},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, task, _ := taskVideoOutputFixture(t)
			task.PrivateData.ResultURL = "https://private-upstream.example/video"
			require.NoError(t, db.Model(test.model).Where("id > 0").Updates(test.updates).Error)
			outputs, err := LoadTaskVideoOutputs(context.Background(), db, []*model.Task{task}, 7, false)
			require.NoError(t, err)
			assert.Equal(t, "unavailable", outputs[task.ID].Media.State)
			assert.True(t, task.IsAssetHostedResult())
			assert.Empty(t, task.PublicResultURL())
			view, err := outputs[task.ID].SignContent(context.Background(), nil)
			require.NoError(t, err)
			assert.Empty(t, view.ContentURL)
		})
	}
}

func TestTaskVideoOutputsDoesNotInventArchiveProgress(t *testing.T) {
	db, task, asset := taskVideoOutputFixture(t)
	require.NoError(t, db.Delete(&model.KKAIVideoTaskAsset{}, "task_id = ?", task.ID).Error)
	for _, test := range []struct {
		status model.TaskStatus
		want   string
	}{
		{model.TaskStatusInProgress, "processing"}, {model.TaskStatusSuccess, "unavailable"}, {model.TaskStatusFailure, "unavailable"},
	} {
		task.Status = test.status
		outputs, err := LoadTaskVideoOutputs(context.Background(), db, []*model.Task{task}, 7, false)
		require.NoError(t, err)
		assert.Equal(t, test.want, outputs[task.ID].Media.State)
	}
	require.NoError(t, db.Model(asset).Update("state", model.VideoAssetStateProcessing).Error)
	require.NoError(t, db.Create(&model.KKAIVideoTaskAsset{TaskID: task.ID, AssetID: asset.ID, Role: model.VideoTaskAssetRoleOutput}).Error)
	task.Status = model.TaskStatusSuccess
	outputs, err := LoadTaskVideoOutputs(context.Background(), db, []*model.Task{task}, 7, false)
	require.NoError(t, err)
	assert.Equal(t, "processing", outputs[task.ID].Media.State, "known asset processing is independent of completed generation")
}
