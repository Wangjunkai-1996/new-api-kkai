package controller

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVertexInlineVideoPollAndArtifactDownloadOverOneMiB(t *testing.T) {
	task := setupGenericTaskTest(t)
	service.InitHttpClient()
	previousFactory, previousCache := service.GetTaskAdaptorFunc, common.MemoryCacheEnabled
	service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor { return relay.GetTaskAdaptor(platform) }
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { service.GetTaskAdaptorFunc, common.MemoryCacheEnabled = previousFactory, previousCache })

	// This one-frame MP4 remains playable with an ISO BMFF free box appended.
	video, err := os.ReadFile("testdata/one-frame.mp4")
	require.NoError(t, err)
	freeBox := make([]byte, 1<<20)
	binary.BigEndian.PutUint32(freeBox, uint32(len(freeBox)))
	copy(freeBox[4:], "free")
	video = append(video, freeBox...)
	const operation = "projects/fixture/locations/us-central1/publishers/google/models/veo-3.0-generate-001/operations/test-one"
	resultJSON, err := common.Marshal(map[string]any{
		"name": operation, "done": true,
		"response": map[string]any{"videos": []any{map[string]any{"mimeType": "video/mp4", "bytesBase64Encoded": base64.StdEncoding.EncodeToString(video)}}},
	})
	require.NoError(t, err)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	credentials, err := common.Marshal(map[string]string{"client_email": "fixture@example.test", "project_id": "fixture", "private_key": base64.StdEncoding.EncodeToString(der)})
	require.NoError(t, err)

	const origin = "https://vertex.example.test"
	client := service.GetHttpClient()
	originalTransport := client.Transport
	t.Cleanup(func() { client.Transport = originalTransport })
	var reads atomic.Int32
	client.Transport = videoProxyTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		body := resultJSON
		switch req.URL.String() {
		case "https://www.googleapis.com/oauth2/v4/token":
			body = []byte(`{"access_token":"local-only-token","expires_in":3600}`)
		case origin + "/v1/projects/fixture/locations/us-central1/publishers/google/models/veo-3.0-generate-001:fetchPredictOperation":
			assert.Equal(t, http.MethodPost, req.Method)
			assert.Equal(t, "Bearer local-only-token", req.Header.Get("Authorization"))
			var query map[string]string
			require.NoError(t, common.DecodeJson(req.Body, &query))
			assert.Equal(t, operation, query["operationName"])
			reads.Add(1)
		default:
			return nil, fmt.Errorf("unexpected test request to %s", req.URL.Host)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(body)), Request: req}, nil
	})
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", task.ChannelId).Updates(map[string]any{
		"type": constant.ChannelTypeVertexAi, "key": string(credentials), "base_url": origin,
	}).Error)
	task.Status, task.Progress, task.FinishTime = model.TaskStatusInProgress, "50%", 0
	task.Platform = "vertex-ai"
	task.Properties.OriginModelName = "veo-3.0-generate-001"
	task.PrivateData.UpstreamTaskID = base64.RawURLEncoding.EncodeToString([]byte(operation))
	task.PrivateData.Execution = &model.TaskExecutionSnapshot{TaskPlugin: &model.TaskPluginSnapshot{Key: "vertex-ai", APIVersion: 1}}
	require.NoError(t, model.DB.Save(task).Error)

	require.NoError(t, service.PollTask(t.Context(), task))
	stored, found, err := model.GetByTaskId(task.UserId, task.TaskID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), stored.Status)
	assert.NotContains(t, string(stored.Data), "bytesBase64Encoded")
	assert.Contains(t, string(stored.Data), "video/mp4")
	require.EqualValues(t, 1, reads.Load())

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set(middleware.TaskArtifactAccessContextKey, true)
	c.Params = gin.Params{{Key: "key", Value: task.TaskID}, {Key: "artifact_key", Value: "video"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/"+task.TaskID+"/artifacts/video/content", nil)
	TaskArtifactContent(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	require.Equal(t, len(video), recorder.Body.Len())
	assert.Equal(t, video, recorder.Body.Bytes())
	assert.EqualValues(t, 2, reads.Load())
}
