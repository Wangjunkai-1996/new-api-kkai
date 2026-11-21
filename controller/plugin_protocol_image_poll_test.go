package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPluginProtocolImagePoll(t *testing.T) {
	plugin, exists := pluginruntime.DefaultRegistry.Get("alibaba")
	require.True(t, exists)
	pinned := pluginruntime.PinnedEndpoint{Protocol: "openai_image", Plugin: plugin}
	oldFactory := service.GetTaskAdaptorFunc
	service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor { return relay.GetTaskAdaptor(platform) }
	t.Cleanup(func() { service.GetTaskAdaptorFunc = oldFactory })

	const imageURL = "https://cdn.example/image.png"
	const success = `{"output":{"task_status":"SUCCEEDED","results":[{"url":"https://cdn.example/image.png"}]}}`
	const failure = `{"output":{"task_status":"FAILED","message":"render rejected"}}`
	for _, tc := range []struct {
		name, body          string
		wantStatus          model.TaskStatus
		wantCode, wantPolls int
	}{
		{"success", success, model.TaskStatusSuccess, 200, 1},
		{"failure", failure, model.TaskStatusFailure, 400, 1},
		{"background finished", success, model.TaskStatusSuccess, 200, 0},
		{"still queued", "", model.TaskStatusQueued, 504, 0},
		{"disconnected during query", "", model.TaskStatusQueued, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupVideoProxyTestDB(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			channel := model.Channel{Type: constant.ChannelTypeAli, Key: "bound-key", BaseURL: common.GetPointer("https://dashscope.example")}
			require.NoError(t, model.DB.Create(&channel).Error)
			task := model.Task{UserId: 7, ChannelId: channel.Id, TaskID: "public-image", Platform: "alibaba", Status: model.TaskStatusQueued, Progress: "0%", Properties: model.Properties{OriginModelName: "wanx2.1-t2i-turbo"}, PrivateData: model.TaskPrivateData{Key: "bound-key", UpstreamTaskID: "provider-image"}}

			submits, polls := 0, 0
			deps := pluginProtocolBridgeDeps{imagePollInterval: time.Millisecond, submissionTimeout: time.Second}
			deps.submit = func(_ *gin.Context, _ *relaycommon.RelayInfo) (*taskSubmissionOutcome, *dto.TaskError) {
				submits++
				require.NoError(t, model.DB.Create(&task).Error)
				if tc.name == "background finished" {
					require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]any{"status": model.TaskStatusSuccess, "data": []byte(success)}).Error)
				}
				return &taskSubmissionOutcome{Task: &task}, nil
			}
			if tc.name == "still queued" {
				deps.imagePollInterval = time.Hour
				deps.submissionTimeout = 20 * time.Millisecond
			}
			client := service.GetHttpClient()
			originalTransport := client.Transport
			t.Cleanup(func() { client.Transport = originalTransport })
			client.Transport = videoProxyTestRoundTripper(func(req *http.Request) (*http.Response, error) {
				polls++
				assert.Equal(t, "/api/v1/tasks/provider-image", req.URL.Path)
				assert.Equal(t, "Bearer bound-key", req.Header.Get("Authorization"))
				if tc.name == "disconnected during query" {
					cancel()
					<-req.Context().Done()
					return nil, req.Context().Err()
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.body)), Request: req}, nil
			})

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/images/generations", nil)
			c.Set(pluginruntime.ContextKeyProtocolRequest, pluginruntime.ProtocolRequestContext{Protocol: pinned.Protocol, Model: task.Properties.OriginModelName})
			serveTaskPluginImageProtocol(c, pinned, deps)
			assert.Equal(t, 1, submits)
			assert.Equal(t, tc.wantPolls, polls)
			assert.Equal(t, model.TaskStatus(model.TaskStatusQueued), task.Status)
			var saved model.Task
			require.NoError(t, model.DB.First(&saved, task.ID).Error)
			assert.Equal(t, tc.wantStatus, saved.Status)
			if tc.wantCode == 0 {
				assert.False(t, c.Writer.Written())
				return
			}
			assert.Equal(t, tc.wantCode, recorder.Code, recorder.Body.String())
			switch tc.wantCode {
			case http.StatusOK:
				assert.Equal(t, imageURL, gjson.Get(recorder.Body.String(), "data.0.url").String())
			case http.StatusBadRequest:
				assert.Contains(t, saved.FailReason, "render rejected")
				assert.Contains(t, gjson.Get(recorder.Body.String(), "error.message").String(), "render rejected")
			case http.StatusGatewayTimeout:
				assert.Equal(t, "task_timeout", gjson.Get(recorder.Body.String(), "error.code").String())
			}
		})
	}
}
