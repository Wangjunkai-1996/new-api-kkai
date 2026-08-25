package sora

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToOpenAIVideoKeepsProviderIdentifiersAndURLsPrivate(t *testing.T) {
	tests := []struct {
		name            string
		publicResultURL string
		wantVideoURL    string
	}{
		{
			name:            "completed task uses the public content proxy",
			publicResultURL: "https://legacy-provider.example/private-result.mp4",
			wantVideoURL:    taskcommon.BuildProxyURL("task_public"),
		},
		{
			name: "unfinished task omits the provider result URL",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			task := &model.Task{
				TaskID: "task_public",
				PrivateData: model.TaskPrivateData{
					ResultURL: test.publicResultURL,
				},
			}
			task.SetData(map[string]any{
				"id":        "provider_id",
				"task_id":   "provider_task_id",
				"object":    "video",
				"status":    "completed",
				"video_url": "https://provider.example/v1/videos/provider_task_id/content",
			})

			data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
			require.NoError(t, err)

			var response map[string]any
			require.NoError(t, common.Unmarshal(data, &response))
			assert.Equal(t, "task_public", response["id"])
			assert.Equal(t, "task_public", response["task_id"])
			if test.wantVideoURL == "" {
				assert.NotContains(t, response, "video_url")
			} else {
				assert.Equal(t, test.wantVideoURL, response["video_url"])
			}
		})
	}
}
