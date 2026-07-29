package service

import (
	"context"
	"net/http"
	"testing"
	"time"

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

func TestResolveVideoArchiveSourceScopesSoraProviderContentToConfiguredBase(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	providerBaseURL := "http://seedance-special-adapter:8080"
	channel := model.Channel{
		Id: 55, Type: constant.ChannelTypeSora, Key: "provider-key",
		BaseURL: common.GetPointer(providerBaseURL),
	}
	require.NoError(t, db.Create(&channel).Error)
	task := model.Task{
		TaskID: "task_public", UserId: 7, ChannelId: channel.Id, Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			ArchiveSource:     "https://api.kkrich.ltd/v1/videos/task_public/content",
			UpstreamTaskID:    "upstream-private",
			AssetHostedResult: true,
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
	require.Equal(t, providerBaseURL+"/v1/videos/upstream-private/content", resolved.fetch.Source)
	require.Equal(t, providerBaseURL, resolved.fetch.ProviderContentBaseURL)
	require.Equal(t, "Bearer provider-key", resolved.fetch.Headers["Authorization"])
}

func TestVideoAssetPipelineInspectsVideoReferenceAndCreatesPoster(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	store.objects["reference.mp4"] = []byte("video")
	store.contentType["reference.mp4"] = "video/mp4"
	now := time.Now().Unix()
	asset := model.KKAIVideoAsset{
		OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference,
		State: model.VideoAssetStateUploaded, ObjectKey: "reference.mp4", MIMEType: "video/mp4",
		SizeBytes: 5, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)
	pipeline, err := NewVideoAssetPipeline(
		db, store, staticVideoMediaProcessor{}, &staticVideoArchiveFetcher{}, t.TempDir(),
	)
	require.NoError(t, err)
	payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
	require.NoError(t, err)
	event := model.KKAIOutboxEvent{Payload: string(payload)}

	require.NoError(t, pipeline.HandleInspect(context.Background(), event))
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Equal(t, model.VideoAssetStateProcessing, asset.State)
	require.NoError(t, pipeline.HandlePoster(context.Background(), event))
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Equal(t, model.VideoAssetStateReady, asset.State)
	require.NotEmpty(t, asset.PosterObjectKey)
}

func TestVideoAssetPipelineRejectsImageContentReservedAsVideoReference(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	store.objects["disguised-reference.mp4"] = []byte("image")
	store.contentType["disguised-reference.mp4"] = "video/mp4"
	now := time.Now().Unix()
	asset := model.KKAIVideoAsset{
		OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference,
		State: model.VideoAssetStateUploaded, ObjectKey: "disguised-reference.mp4", MIMEType: "video/mp4",
		SizeBytes: 5, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)
	media := callbackVideoMediaProcessor{inspect: func(context.Context, string) (VideoMediaMetadata, error) {
		return VideoMediaMetadata{MIMEType: "image/png", Width: 1920, Height: 1080, Codec: "png"}, nil
	}}
	pipeline, err := NewVideoAssetPipeline(
		db, store, media, &staticVideoArchiveFetcher{}, t.TempDir(),
	)
	require.NoError(t, err)
	payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
	require.NoError(t, err)

	err = pipeline.HandleInspect(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)})
	require.ErrorIs(t, err, ErrVideoMediaInvalid)
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Equal(t, model.VideoAssetStateFailed, asset.State)
	require.Equal(t, "video/mp4", asset.MIMEType)
}
