package relay

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type taskVisibilityRoundTripper func(*http.Request) (*http.Response, error)

func (transport taskVisibilityRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestTaskModel2DtoRedactsAssetHostedResult(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_managed",
		FailReason: "https://upstream.example/legacy-private.mp4",
		PrivateData: model.TaskPrivateData{
			ResultURL:         "https://upstream.example/private.mp4",
			ArchiveSource:     "data:video/mp4;base64,cHJpdmF0ZQ==",
			AssetHostedResult: true,
		},
		Data: json.RawMessage(`{"secret":"upstream payload"}`),
	}

	public := TaskModel2Dto(task)
	assert.Empty(t, public.ResultURL)
	assert.Equal(t, model.AssetHostedTaskPublicFailureReason, public.FailReason)
	assert.Nil(t, public.Data)
	assert.Equal(t, "https://upstream.example/private.mp4", task.PrivateData.ResultURL)
	assert.NotEmpty(t, task.Data)
}

func TestVideoFetchByIDRedactsAssetHostedResultForAllFormats(t *testing.T) {
	setupTaskReliabilityDB(t)
	task := model.Task{
		TaskID:   "task_managed_fetch",
		UserId:   42,
		Platform: constant.TaskPlatform("unsupported-public-converter"),
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		PrivateData: model.TaskPrivateData{
			ResultURL:         "https://upstream.example/private.mp4",
			ArchiveSource:     "data:video/mp4;base64,cHJpdmF0ZQ==",
			AssetHostedResult: true,
		},
		Data: json.RawMessage(`{"secret":"upstream payload"}`),
	}
	require.NoError(t, model.DB.Create(&task).Error)

	tests := []struct {
		name string
		path string
	}{
		{name: "OpenAI videos", path: "/v1/videos/task_managed_fetch"},
		{name: "generic video fetch", path: "/v1/video/generations/task_managed_fetch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, test.path, nil)
			ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
			ctx.Set("id", task.UserId)

			body, taskErr := videoFetchByIDRespBodyBuilder(ctx)
			require.Nil(t, taskErr)
			require.NotEmpty(t, body)
			assert.NotContains(t, string(body), "upstream.example")
			assert.NotContains(t, string(body), "upstream payload")
			assert.NotContains(t, string(body), "cHJpdmF0ZQ")

			if test.path == "/v1/videos/task_managed_fetch" {
				var video dto.OpenAIVideo
				require.NoError(t, common.Unmarshal(body, &video))
				assert.Equal(t, task.TaskID, video.ID)
				assert.Nil(t, video.Metadata)
			}
		})
	}
}

func TestVideoFetchByIDPersistsManagedRealtimeResultWithoutExposingIt(t *testing.T) {
	setupTaskReliabilityDB(t)
	service.InitHttpClient()
	client := service.GetHttpClient()
	originalTransport := client.Transport
	upstreamCalls := 0
	responseBody := `{
		"name":"operations/generated-video",
		"done":true,
		"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"https://media.example/private.mp4"}}]}}
	}`
	client.Transport = taskVisibilityRoundTripper(func(request *http.Request) (*http.Response, error) {
		upstreamCalls++
		require.Equal(t, "provider-key", request.Header.Get("x-goog-api-key"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(responseBody)),
			Request:    request,
		}, nil
	})
	t.Cleanup(func() { client.Transport = originalTransport })

	channel := model.Channel{
		Id: constant.ChannelTypeGemini, Type: constant.ChannelTypeGemini, Key: "provider-key",
		BaseURL: common.GetPointer("https://provider.example"),
	}
	require.NoError(t, model.DB.Create(&channel).Error)
	task := model.Task{
		TaskID: "task_managed_realtime", UserId: 42, ChannelId: channel.Id,
		Platform: constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeGemini)),
		Status:   model.TaskStatusInProgress, Progress: "50%",
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID:    taskcommon.EncodeLocalTaskID("operations/generated-video"),
			Key:               "provider-key",
			AssetHostedResult: true,
		},
	}
	require.NoError(t, model.DB.Create(&task).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID, nil)
	ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	ctx.Set("id", task.UserId)
	body, taskErr := videoFetchByIDRespBodyBuilder(ctx)
	require.Nil(t, taskErr)
	assert.NotContains(t, string(body), "media.example")

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), persisted.Status)
	assert.Equal(t, "https://media.example/private.mp4", persisted.PrivateData.ArchiveSource)
	assert.Contains(t, persisted.PrivateData.ResultURL, "/v1/videos/"+task.TaskID+"/content")
	assert.Equal(t, 1, upstreamCalls)

	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]any{
		"status": model.TaskStatusInProgress, "progress": "50%",
	}).Error)
	responseBody = `{"name":"operations/generated-video","done":true,"response":{}}`
	secondRecorder := httptest.NewRecorder()
	secondContext, _ := gin.CreateTestContext(secondRecorder)
	secondContext.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID, nil)
	secondContext.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	secondContext.Set("id", task.UserId)
	secondBody, secondTaskErr := videoFetchByIDRespBodyBuilder(secondContext)
	require.Nil(t, secondTaskErr)
	assert.NotContains(t, string(secondBody), "media.example")

	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Equal(t, "https://media.example/private.mp4", persisted.PrivateData.ArchiveSource)
	assert.Equal(t, 2, upstreamCalls)

	persisted.PrivateData.ResultURL = ""
	persisted.PrivateData.ArchiveSource = ""
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]any{
		"status": model.TaskStatusSuccess, "progress": "100%", "private_data": persisted.PrivateData,
	}).Error)
	responseBody = `{
		"name":"operations/generated-video",
		"done":true,
		"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"https://media.example/resurrected.mp4"}}]}}
	}`
	thirdRecorder := httptest.NewRecorder()
	thirdContext, _ := gin.CreateTestContext(thirdRecorder)
	thirdContext.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID, nil)
	thirdContext.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	thirdContext.Set("id", task.UserId)
	thirdBody, thirdTaskErr := videoFetchByIDRespBodyBuilder(thirdContext)
	require.Nil(t, thirdTaskErr)
	assert.NotContains(t, string(thirdBody), "resurrected")
	assert.Equal(t, 2, upstreamCalls)

	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Empty(t, persisted.PrivateData.ResultURL)
	assert.Empty(t, persisted.PrivateData.ArchiveSource)
}
