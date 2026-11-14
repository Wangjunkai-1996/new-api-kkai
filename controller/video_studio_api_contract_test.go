package controller

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVideoStudioAPIContractPreservesOldTabsAndGatesNewCreation(t *testing.T) {
	previous := video_studio_setting.Get().MultimodalEnabled
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"video_studio.multimodal_enabled": strconv.FormatBool(previous)}))
	})
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, method, path, version string
		enabled, legacy             bool
		status                      int
		code                        string
	}{
		{name: "old tab submits while disabled", method: "POST", path: "/pg/videos", legacy: true, status: 204},
		{name: "explicit API3 uploads while disabled", method: "POST", path: "/api/video-studio/uploads", version: "3", legacy: true, status: 204},
		{name: "old admin remains editable", method: "PUT", path: "/api/admin/video-studio/model-profiles/1", legacy: true, status: 204},
		{name: "new task waits for rollout", method: "POST", path: "/pg/videos", version: "4", status: 503, code: "video_multimodal_unavailable"},
		{name: "new quote waits for rollout", method: "POST", path: "/pg/videos/quote", version: "4", status: 503, code: "video_multimodal_unavailable"},
		{name: "new upload waits for writer", method: "POST", path: "/api/video-studio/uploads", version: "4", status: 503, code: "video_multimodal_unavailable"},
		{name: "admin prepares new model while disabled", method: "POST", path: "/api/admin/video-studio/model-profiles", version: "4", status: 204},
		{name: "admin upgrades model while disabled", method: "PUT", path: "/api/admin/video-studio/model-profiles/1", version: "4", status: 204},
		{name: "admin sample mutation waits for rollout", method: "PUT", path: "/api/admin/video-studio/samples/1", version: "4", status: 503, code: "video_multimodal_unavailable"},
		{name: "admin sample upload waits for writer", method: "POST", path: "/api/admin/video-studio/uploads", version: "4", status: 503, code: "video_multimodal_unavailable"},
		{name: "catalog inspection remains available", method: "GET", path: "/api/video-studio/models", version: "4", status: 204},
		{name: "accepted task lookup remains available", method: "GET", path: "/api/video-studio/submissions/by-key", version: "4", status: 204},
		{name: "inflight completion remains available", method: "POST", path: "/api/video-studio/uploads/1/complete", version: "4", status: 204},
		{name: "persisted enable opens new creation", method: "POST", path: "/pg/videos", version: "4", enabled: true, status: 204},
		{name: "unknown version fails before handler", method: "POST", path: "/pg/videos", version: "5", status: 400, code: "video_request_contract_invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"video_studio.multimodal_enabled": strconv.FormatBool(test.enabled)}))
			router := gin.New()
			called := false
			router.Use(VideoStudioAPIContract)
			router.Handle(test.method, test.path, func(c *gin.Context) {
				called = true
				assert.Equal(t, test.legacy, service.IsVideoStudioAPI3(c.Request.Context()))
				c.Status(http.StatusNoContent)
			})
			request := httptest.NewRequest(test.method, test.path, nil)
			if test.version != "" {
				request.Header.Set("X-KKAI-Console-API", test.version)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			assert.Equal(t, test.status, recorder.Code)
			assert.Equal(t, test.status == 204, called)
			assert.Contains(t, recorder.Header().Values("Vary"), "X-KKAI-Console-API")
			if test.code != "" {
				var response struct {
					Code string `json:"code"`
				}
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
				assert.Equal(t, test.code, response.Code)
			}
		})
	}
}

func TestVideoStudioAPI3HTTPReplaysPreUpgradeTaskWithoutNewFieldsOrStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, token := newVideoStudioRelayTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	now := time.Now().Unix()
	task := model.Task{TaskID: "api3-existing", UserId: 7, Status: model.TaskStatusQueued, Properties: model.Properties{OriginModelName: "video-model-v1"}, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&task).Error)
	legacyJSON := fmt.Sprintf(`{"token_id":%d,"model":"video-model-v1","group":"Seedance 视频","mode":"text_to_video","prompt":"test","parameters":{},"references":[]}`, token.Id)
	require.NoError(t, db.Create(&model.KKAIIdempotencyKey{UserID: 7, Operation: model.VideoIdempotencyOperationSubmit, Key: "api3-retry", RequestHash: fmt.Sprintf("%x", sha256.Sum256([]byte(legacyJSON))), ResourceType: model.VideoIdempotencyResourceTask, ResourceID: task.TaskID, CreatedAt: now, ExpiresAt: now + 3600}).Error)
	body, err := common.Marshal(map[string]any{"token_id": token.Id, "model": "video-model-v1", "mode": "text_to_video", "prompt": "test", "max_quota": 1, "quote_hash": fmt.Sprintf("%064x", 1), "quote_expires_at": now - 60})
	require.NoError(t, err)
	router := gin.New()
	router.POST("/pg/videos", func(c *gin.Context) { c.Set("id", 7); c.Set("user_group", "default") }, VideoStudioAPIContract, PrepareVideoStudioTaskRequest, func(c *gin.Context) { t.Error("accepted replay must not reach distribution") })
	request := httptest.NewRequest(http.MethodPost, "/pg/videos", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "api3-retry")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), `"api3-existing"`)
	assert.Contains(t, response.Body.String(), `"queued"`)
}
