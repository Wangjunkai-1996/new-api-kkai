package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type videoWorksSigningStore struct{ videoStudioRelayTestStore }

func (videoWorksSigningStore) PresignDownload(_ context.Context, key, _ string, attachment bool, _ time.Duration) (string, error) {
	if attachment {
		return "https://media.example/" + key + "?download=1", nil
	}
	return "https://media.example/" + key, nil
}

func videoWorksHostedFixture(t *testing.T) *model.Task {
	t.Helper()
	task := setupGenericTaskTest(t)
	task.Action = constant.TaskActionTextToVideo
	task.PrivateData.ResultURL = "https://private-upstream.invalid/video"
	require.NoError(t, model.DB.Save(task).Error)
	require.NoError(t, model.DB.Create(&model.KKAIVideoGeneration{TaskID: task.ID, UserID: task.UserId}).Error)
	asset := &model.KKAIVideoAsset{OwnerUserID: task.UserId, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
		State: model.VideoAssetStateReady, ObjectKey: "video.mp4", PosterObjectKey: "poster.jpg", Width: 1280, Height: 720, DurationSeconds: 5}
	require.NoError(t, model.DB.Create(asset).Error)
	require.NoError(t, model.DB.Create(&model.KKAIVideoTaskAsset{TaskID: task.ID, AssetID: asset.ID, Role: model.VideoTaskAssetRoleOutput}).Error)
	raw := config.GlobalConfig.Get("video_studio")
	original, err := config.ConfigToMap(raw)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, config.UpdateConfigFromMap(raw, original)) })
	require.NoError(t, config.UpdateConfigFromMap(raw, map[string]string{"access_mode": "all", "worker_enabled": "false"}))
	return task
}

type videoWorksTestResponse struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Data    struct {
		Items  []*dto.TaskDto         `json:"items"`
		Total  int64                  `json:"total"`
		Counts model.VideoWorksCounts `json:"status_counts"`
	} `json:"data"`
}

func videoWorksRequest(t *testing.T, query string, admin bool) (*httptest.ResponseRecorder, videoWorksTestResponse) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/task/self?media_type=video&"+query, nil)
	c.Set("id", 7)
	c.Set("role", common.RoleAdminUser)
	c.Set(videoStudioAssetStoreContextKey, videoWorksSigningStore{})
	if admin {
		GetAllTask(c)
	} else {
		GetUserTask(c)
	}
	var response videoWorksTestResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return recorder, response
}

func TestVideoWorksHostedPosterAndArtifactContract(t *testing.T) {
	task := videoWorksHostedFixture(t)
	_, list := videoWorksRequest(t, "", false)
	require.Len(t, list.Data.Items, 1)
	item := list.Data.Items[0]
	require.NotNil(t, item.Media)
	assert.Equal(t, "ready", item.Media.State)
	assert.Equal(t, "https://media.example/poster.jpg", item.Media.PosterURL)
	assert.Greater(t, item.Media.PosterExpiresAt, time.Now().Unix())
	assert.False(t, item.LegacyVideoAvailable)
	assert.Empty(t, item.ResultURL)
	for _, deleted := range []bool{false, true} {
		if deleted {
			require.NoError(t, model.DB.Model(&model.KKAIVideoGeneration{}).Where("task_id = ?", task.ID).Update("deleted_at", 1).Error)
		}
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/task/"+task.TaskID+"/artifacts", nil)
		c.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
		c.Set("id", 7)
		c.Set(videoStudioAssetStoreContextKey, videoWorksSigningStore{})
		GetDashboardTaskArtifacts(c)
		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Data struct {
				HostedVideo dto.TaskHostedVideo    `json:"hosted_video"`
				Artifacts   []taskArtifactResponse `json:"artifacts"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		view := response.Data.HostedVideo
		assert.Equal(t, task.ID, view.TaskRecordID)
		assert.Empty(t, response.Data.Artifacts)
		assert.NotContains(t, recorder.Body.String(), "private-upstream")
		assert.NotContains(t, recorder.Body.String(), "legacy_content_url")
		if deleted {
			assert.Equal(t, "unavailable", view.State)
			assert.Empty(t, view.ContentURL)
			assert.Empty(t, view.DownloadURL)
		} else {
			assert.Equal(t, "ready", view.State)
			assert.Equal(t, "https://media.example/video.mp4", view.ContentURL)
			assert.Equal(t, "https://media.example/video.mp4?download=1", view.DownloadURL)
			assert.Greater(t, view.ExpiresAt, time.Now().Unix())
		}
	}
}

func TestVideoWorksMediaFailurePreservesTasksAndBilling(t *testing.T) {
	task := videoWorksHostedFixture(t)
	legacy := &model.Task{
		TaskID: "ordinary-video", UserId: task.UserId, Action: constant.TaskActionTextToVideo,
		Status: model.TaskStatusSuccess, Quota: 100,
		PrivateData: model.TaskPrivateData{ResultURL: "https://legacy-upstream.invalid/video.mp4"},
	}
	require.NoError(t, model.DB.Create(legacy).Error)
	setTaskLogCreditEpoch(t, taskLogCreditEpochMarker(t))
	require.NoError(t, model.DB.Migrator().DropTable(&model.KKAIVideoAsset{}))
	recorder, list := videoWorksRequest(t, "", false)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Len(t, list.Data.Items, 2)
	byID := make(map[int64]*dto.TaskDto)
	for _, item := range list.Data.Items {
		byID[item.ID] = item
		require.NotNil(t, item.Billing.AmountAvailable)
		assert.True(t, *item.Billing.AmountAvailable)
	}
	item := byID[task.ID]
	require.NotNil(t, item)
	assert.Equal(t, task.ID, item.ID)
	require.NotNil(t, item.Media)
	assert.Equal(t, "unavailable", item.Media.State)
	assert.False(t, item.LegacyVideoAvailable)
	legacyItem := byID[legacy.ID]
	require.NotNil(t, legacyItem)
	assert.Nil(t, legacyItem.Media, "an unrelated media outage must not misclassify a legacy video as Studio")
	assert.True(t, legacyItem.LegacyVideoAvailable, "ordinary previews must remain on their existing authorized path")
	assert.Empty(t, legacyItem.ResultURL)
	assert.Contains(t, recorder.Body.String(), `"media_warning":"media_temporarily_unavailable"`)
}

func TestVideoWorksDashboardRejectsDuplicatePublicID(t *testing.T) {
	task := videoWorksHostedFixture(t)
	require.NoError(t, model.DB.Create(&model.Task{TaskID: task.TaskID, UserId: task.UserId}).Error)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/task/"+task.TaskID+"/artifacts", nil)
	c.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	c.Set("id", 7)
	GetDashboardTaskArtifacts(c)
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestVideoWorksFiltersBeforePaginationAndCounts(t *testing.T) {
	setupGenericTaskTest(t)
	setTaskLogCreditEpoch(t, taskLogCreditEpochMarker(t))
	require.NoError(t, model.DB.Create(&model.User{Id: 8, Username: "other-video-owner", AffCode: "video8", Status: common.UserStatusEnabled}).Error)
	for i, row := range []struct {
		owner                int
		action, status, name string
	}{
		{7, constant.TaskActionTextToVideo, model.TaskStatusSuccess, "sora_2"},
		{7, constant.TaskActionTextGenerate, model.TaskStatusInProgress, "sora_2"},
		{7, constant.TaskActionPluginRemix, model.TaskStatusFailure, "sora_2"},
		{7, constant.TaskActionImageToVideo, model.TaskStatusSuccess, "soraX2"},
		{8, constant.TaskActionTextToVideo, model.TaskStatusSuccess, "sora_2"},
		{7, "MUSIC", model.TaskStatusSuccess, "sora_2"},
	} {
		task := model.Task{ID: int64(11 + i), TaskID: "video-" + row.status + row.name, UserId: row.owner,
			Action: row.action, Status: model.TaskStatus(row.status), SubmitTime: 100,
			Properties: model.Properties{OriginModelName: row.name}, Quota: 200}
		require.NoError(t, model.DB.Create(&task).Error)
	}
	recorder, response := videoWorksRequest(t, "q=sora_2&page_size=1&status_group=success&start_timestamp=50&end_timestamp=150", false)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
	require.True(t, response.Success)
	require.Len(t, response.Data.Items, 1)
	assert.Equal(t, int64(1), response.Data.Total)
	assert.Equal(t, model.VideoWorksCounts{All: 3, Success: 1, Processing: 1, Failed: 1}, response.Data.Counts)
	assert.Equal(t, 7, response.Data.Items[0].UserId)
	require.NotNil(t, response.Data.Items[0].Billing)
	require.NotNil(t, response.Data.Items[0].Billing.AmountAvailable)
	assert.True(t, *response.Data.Items[0].Billing.AmountAvailable, "missing price snapshot does not hide a trustworthy amount")
	assert.Equal(t, 200, response.Data.Items[0].Quota)
	assert.Nil(t, response.Data.Items[0].AdminInfo)

	_, all := videoWorksRequest(t, "q=sora_2&page_size=1&p=2", true)
	assert.Equal(t, int64(4), all.Data.Total)
	require.Len(t, all.Data.Items, 1)
	assert.Equal(t, "artifact-owner", all.Data.Items[0].Username)
}

func TestVideoWorksRejectsInvalidFiltersAndDoesNotHideDatabaseErrors(t *testing.T) {
	setupGenericTaskTest(t)
	for _, query := range []string{"p=-1", "page_size=0", "page_size=101", "status_group=unknown", "status=SUCCESS&status_group=success", "channel_id=1", "start_timestamp=20&end_timestamp=10", "start_timestamp=bad"} {
		recorder, response := videoWorksRequest(t, query, false)
		assert.Equal(t, http.StatusBadRequest, recorder.Code, query)
		assert.False(t, response.Success)
	}
	require.NoError(t, model.DB.Migrator().DropTable(&model.Task{}))
	recorder, response := videoWorksRequest(t, "", false)
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Equal(t, "video_works_query_failed", response.Code)
}

func TestVideoWorksUnavailableAmountIsNotFree(t *testing.T) {
	task := setupGenericTaskTest(t)
	task.Action = constant.TaskActionTextToVideo
	task.Quota = 100
	require.NoError(t, model.DB.Save(task).Error)
	setTaskLogCreditEpoch(t, "{}")
	_, response := videoWorksRequest(t, "", false)
	require.Len(t, response.Data.Items, 1)
	require.NotNil(t, response.Data.Items[0].Billing.AmountAvailable)
	assert.False(t, *response.Data.Items[0].Billing.AmountAvailable)
}

func TestTaskArtifactDownloadKeepsContentAndForcesSafeAttachment(t *testing.T) {
	task := setupGenericTaskTest(t)
	task.Action = constant.TaskActionTextToVideo
	task.PrivateData.ResultURL = "data:video/mp4;base64,dmlkZW8="
	require.NoError(t, model.DB.Save(task).Error)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Set("id", 7)
		c.Params = gin.Params{{Key: "key", Value: task.TaskID}, {Key: "artifact_key", Value: "video"}}
		c.Request = httptest.NewRequest(method, "/v1/tasks/"+task.TaskID+"/artifacts/video/content?download=1", nil)
		TaskArtifactContent(c)
		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, "attachment; filename=task_generic.mp4", recorder.Header().Get("Content-Disposition"))
		assert.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
		if method == http.MethodGet {
			assert.Equal(t, "video", recorder.Body.String())
		} else {
			assert.Empty(t, recorder.Body.String())
		}
	}
}

func TestTaskArtifactContentRejectsHostedGenerationBeforeBackfill(t *testing.T) {
	task := videoWorksHostedFixture(t)
	task.PrivateData.ResultURL = "data:video/mp4;base64,dmlkZW8="
	require.NoError(t, model.DB.Save(task).Error)
	for _, deleted := range []bool{false, true} {
		if deleted {
			require.NoError(t, model.DB.Model(&model.KKAIVideoGeneration{}).Where("task_id = ?", task.ID).Update("deleted_at", 1).Error)
		}
		for _, content := range []bool{false, true} {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Set("id", 7)
			c.Params = gin.Params{{Key: "key", Value: task.TaskID}, {Key: "artifact_key", Value: "video"}}
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/"+task.TaskID+"/artifacts/video/content", nil)
			if content {
				TaskArtifactContent(c)
				assert.Equal(t, http.StatusNotFound, recorder.Code)
			} else {
				GetTaskArtifacts(c)
				assert.Equal(t, http.StatusOK, recorder.Code)
				assert.Contains(t, recorder.Body.String(), `"artifacts":[]`)
				assert.NotContains(t, recorder.Body.String(), "legacy_content_url")
			}
		}
	}
}

func TestTaskArtifactDownloadPreservesRangeAndHead(t *testing.T) {
	task := setupGenericTaskTest(t)
	allowPrivateTaskMediaTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "bytes=0-3", r.Header.Get("Range"))
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Range", "bytes 0-3/10")
		w.Header().Set("Content-Length", "4")
		w.Header().Set("Content-Disposition", "attachment; filename=untrusted.exe")
		w.WriteHeader(http.StatusPartialContent)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte("data"))
		}
	}))
	defer upstream.Close()
	task.Action = constant.TaskActionTextToVideo
	task.PrivateData.ResultURL = upstream.URL
	require.NoError(t, model.DB.Save(task).Error)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Set("id", 7)
		c.Params = gin.Params{{Key: "key", Value: task.TaskID}, {Key: "artifact_key", Value: "video"}}
		c.Request = httptest.NewRequest(method, "/v1/tasks/"+task.TaskID+"/artifacts/video/content?download=1", nil)
		c.Request.Header.Set("Range", "bytes=0-3")
		TaskArtifactContent(c)
		assert.Equal(t, http.StatusPartialContent, recorder.Code)
		assert.Equal(t, "bytes 0-3/10", recorder.Header().Get("Content-Range"))
		assert.Equal(t, "attachment; filename=task_generic.mp4", recorder.Header().Get("Content-Disposition"))
		if method == http.MethodHead {
			assert.Empty(t, recorder.Body.String())
		} else {
			assert.Equal(t, "data", recorder.Body.String())
		}
	}
}
