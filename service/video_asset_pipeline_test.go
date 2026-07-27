package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

type archiveSourceRefreshAdaptor struct {
	response *http.Response
}

func (adaptor *archiveSourceRefreshAdaptor) Init(*relaycommon.RelayInfo) {}

func (adaptor *archiveSourceRefreshAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return adaptor.response, nil
}

func (adaptor *archiveSourceRefreshAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return nil, nil
}

func (adaptor *archiveSourceRefreshAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	return 0
}

func TestRefreshVideoArchiveSourceRejectsNilResponseOrBody(t *testing.T) {
	tests := []struct {
		name     string
		response *http.Response
	}{
		{name: "nil response"},
		{name: "nil body", response: &http.Response{StatusCode: http.StatusOK}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousFactory := GetTaskAdaptorFunc
			GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor {
				return &archiveSourceRefreshAdaptor{response: tt.response}
			}
			t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

			pipeline := &VideoAssetPipeline{}
			_, err := pipeline.refreshVideoArchiveSource(context.Background(), model.Task{
				Platform: constant.TaskPlatform("gemini"),
				PrivateData: model.TaskPrivateData{
					UpstreamTaskID: "operations/private",
				},
			}, model.Channel{Type: constant.ChannelTypeGemini, Key: "private-key"})

			require.ErrorIs(t, err, ErrVideoArchiveResponseRejected)
		})
	}
}

func TestResolveVideoArchiveSourceScopesGeminiAPIKeyToProviderOrigin(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantAPIKey bool
	}{
		{name: "same origin", source: "https://generativelanguage.googleapis.com/v1beta/files/video", wantAPIKey: true},
		{name: "cross origin", source: "https://media.example/private.mp4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newVideoPipelineTestDB(t)
			channel := model.Channel{
				Id: 41, Type: constant.ChannelTypeGemini, Key: "channel-key",
				BaseURL: common.GetPointer("https://generativelanguage.googleapis.com"),
			}
			require.NoError(t, db.Create(&channel).Error)
			task := model.Task{
				TaskID: "gemini-credential-scope", UserId: 7, ChannelId: channel.Id,
				Status: model.TaskStatusSuccess,
				PrivateData: model.TaskPrivateData{
					ArchiveSource: tt.source, AssetHostedResult: true, Key: "private-key",
				},
			}
			require.NoError(t, db.Create(&task).Error)
			asset := model.KKAIVideoAsset{
				OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
				State: model.VideoAssetStateProcessing, ObjectKey: "output.mp4",
				ArchiveSourceURL: videoTaskResultArchiveSource(task.ID), MIMEType: "video/mp4",
			}
			require.NoError(t, db.Create(&asset).Error)
			require.NoError(t, db.Create(&model.KKAIVideoTaskAsset{
				TaskID: task.ID, AssetID: asset.ID, Role: model.VideoTaskAssetRoleOutput, Position: 0,
			}).Error)

			resolved, err := (&VideoAssetPipeline{db: db}).resolveVideoArchiveSource(context.Background(), asset)
			require.NoError(t, err)
			assertedKey := resolved.fetch.Headers["x-goog-api-key"]
			if tt.wantAPIKey {
				require.Equal(t, "private-key", assertedKey)
			} else {
				require.Empty(t, assertedKey)
			}
		})
	}
}
