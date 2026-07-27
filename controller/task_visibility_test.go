package controller

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTasksToDtoRedactsAssetHostedResultsForTaskSelf(t *testing.T) {
	tasks := []*model.Task{{
		TaskID:     "task_self_managed",
		FailReason: "provider rejected https://provider.example/temp.mp4?token=query-secret key=echoed-provider-key",
		PrivateData: model.TaskPrivateData{
			ResultURL:         "https://upstream.example/private.mp4",
			ArchiveSource:     "data:video/mp4;base64,cHJpdmF0ZQ==",
			AssetHostedResult: true,
		},
		Data: json.RawMessage(`{"secret":"upstream payload"}`),
	}}

	result := tasksToDto(tasks, false)
	require.Len(t, result, 1)
	assert.Empty(t, result[0].ResultURL)
	assert.Nil(t, result[0].Data)
	assert.Equal(t, "video generation failed", result[0].FailReason)
	assert.NotContains(t, result[0].FailReason, "query-secret")
	assert.NotContains(t, result[0].FailReason, "echoed-provider-key")
}
