package controller

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVideoWorksOrdinaryPosterUsesSourceDataOmittedByList(t *testing.T) {
	task := setupGenericTaskTest(t)
	task.Action = constant.TaskActionTextToVideo
	task.PrivateData.ResultURL = "https://media.example/output.mp4"
	task.Data = []byte(`{"result":"first output"}`)
	require.NoError(t, model.DB.Save(task).Error)
	version := model.TaskVideoPosterVersion(task, "video")
	task.PrivateData.VideoPosters = map[string]model.TaskVideoPosterSnapshot{
		"video": {State: "ready", Version: version, ObjectKey: "users/7/task-posters/" + version + ".jpg"},
	}
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Update("private_data", task.PrivateData).Error)
	setTaskLogCreditEpoch(t, taskLogCreditEpochMarker(t))
	_, response := videoWorksRequest(t, "", false)
	require.Len(t, response.Data.Items, 1)
	poster := response.Data.Items[0].Poster
	require.NotNil(t, poster)
	assert.Equal(t, "ready", poster.State)
	assert.Equal(t, "video", poster.ArtifactKey)
	assert.Equal(t, "https://media.example/users/7/task-posters/"+version+".jpg", poster.URL)
	assert.Equal(t, "null", string(response.Data.Items[0].Data))
}

func TestTaskVideoPosterSourceKeepsProviderCredentialsOnOrigin(t *testing.T) {
	baseURL := "https://provider.example/v1"
	channel := &model.Channel{BaseURL: &baseURL}
	for _, test := range []struct {
		name       string
		descriptor relaychannel.TaskContentRequest
		allowed    bool
	}{
		{name: "provider origin", descriptor: relaychannel.TaskContentRequest{URL: "https://provider.example/video.mp4", Method: http.MethodGet, Headers: map[string]string{"Authorization": "Bearer secret"}}, allowed: true},
		{name: "foreign origin", descriptor: relaychannel.TaskContentRequest{URL: "https://media.example/video.mp4", Method: http.MethodGet, Headers: map[string]string{"Authorization": "Bearer secret"}}},
		{name: "credentialless with headers", descriptor: relaychannel.TaskContentRequest{URL: "https://provider.example/video.mp4", Method: http.MethodGet, Headers: map[string]string{"Authorization": "Bearer secret"}, Credentialless: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, err := taskVideoPosterSourceFromDescriptor("provider-task", &test.descriptor, channel)
			if test.allowed {
				require.NoError(t, err)
				assert.Equal(t, "Bearer secret", source.Headers["Authorization"])
			} else {
				require.Error(t, err)
				assert.Empty(t, source.URL)
			}
		})
	}
}

func TestTaskVideoPosterOwnersMatchStudioMediaBoundary(t *testing.T) {
	task := setupGenericTaskTest(t)
	task.TenantID = 42
	require.NoError(t, model.DB.Save(task).Error)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", task.UserId).Update("tenant_id", 42).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: 8, Username: "platform-admin", AffCode: "poster-admin-8", Status: common.UserStatusEnabled, Role: common.RoleAdminUser}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: 9, Username: "tenant-admin", AffCode: "poster-admin-9", Status: common.UserStatusEnabled, Role: common.RoleAdminUser, TenantID: 42}).Error)
	ctx := context.Background()
	for _, test := range []struct {
		viewer int
		admin  bool
		allow  bool
	}{
		{viewer: 7, allow: true},
		{viewer: 8, admin: true, allow: true},
		{viewer: 9, admin: true, allow: false},
		{viewer: 9, allow: false},
	} {
		allowed, err := authorizedTaskVideoPosterOwners(ctx, model.DB, []*model.Task{task}, test.viewer, test.admin)
		require.NoError(t, err)
		assert.Equal(t, test.allow, allowed[task.ID])
	}
}
