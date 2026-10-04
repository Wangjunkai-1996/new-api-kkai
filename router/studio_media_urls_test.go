package router

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStudioMediaURLsRequireJWTAndPreserveOwnership(t *testing.T) {
	fixture := newVideoStudioMediaAuthFixture(t)
	path := fmt.Sprintf("/api/video-studio/assets/%d/content", fixture.ownedAssetID)
	foreign := fmt.Sprintf("/api/video-studio/assets/%d/content", fixture.foreignAssetID)
	tests := []struct {
		name   string
		token  string
		paths  []string
		status int
	}{
		{"browser session", fixture.sessionToken, []string{path, path}, http.StatusOK},
		{"anonymous", "", []string{path}, http.StatusUnauthorized},
		{"foreign asset", fixture.sessionToken, []string{foreign}, http.StatusForbidden},
		{"mixed ownership", fixture.sessionToken, []string{path, foreign}, http.StatusForbidden},
		{"legacy PAT", fixture.accessToken, []string{path}, http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := common.Marshal(map[string]any{"paths": test.paths})
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodPost, "/api/studio/media-urls", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			recorder := httptest.NewRecorder()
			fixture.engine.ServeHTTP(recorder, request)
			require.Equal(t, test.status, recorder.Code, recorder.Body.String())
			assert.NotContains(t, recorder.Body.String(), fixture.sessionToken)
			if test.status != http.StatusOK {
				assert.NotContains(t, recorder.Body.String(), "X-Amz-Signature")
				return
			}
			var response struct {
				Success bool `json:"success"`
				Data    struct {
					URLs      map[string]string `json:"urls"`
					ExpiresAt int64             `json:"expires_at"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.True(t, response.Success)
			assert.Len(t, response.Data.URLs, 1)
			assert.Contains(t, response.Data.URLs[path], "r2.example.test/video-test/users/1/owned/source.mp4")
			assert.Contains(t, response.Data.URLs[path], "X-Amz-Expires=600")
			assert.Greater(t, response.Data.ExpiresAt, time.Now().Unix())
			assert.LessOrEqual(t, response.Data.ExpiresAt, time.Now().Unix()+600)
			assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
		})
	}
}

func TestStudioMediaURLsRejectNonAssetPathsAndUnboundedBatches(t *testing.T) {
	fixture := newVideoStudioMediaAuthFixture(t)
	paths := []string{
		"https://example.test/api/video-studio/assets/1/content",
		"//example.test/api/video-studio/assets/1/content",
		"/api/user/self",
		"/api/video-studio/assets/0/content",
		"/api/video-studio/assets/01/content",
		"/api/video-studio/assets/1/content?variant=thumbnail",
		"/api/image-studio/assets/1/content?variant=poster",
		"/api/video-studio/assets/1/download?variant=preview",
		"/api/video-studio/assets/1/content?token=secret",
		"/api/video-studio/assets/1/content?variant=poster&variant=preview",
		"/api/video-studio/assets/1/content#fragment",
		"/api/video-studio/assets/%31/content",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			body, err := common.Marshal(map[string]any{"paths": []string{path}})
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodPost, "/api/studio/media-urls", bytes.NewReader(body))
			request.Header.Set("Authorization", "Bearer "+fixture.sessionToken)
			recorder := httptest.NewRecorder()
			fixture.engine.ServeHTTP(recorder, request)
			assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
		})
	}
	for _, body := range []string{`{"paths":[]}`, `{"paths":[` + strings.Repeat(`"/api/video-studio/assets/1/content",`, 100) + `"/api/video-studio/assets/1/content"]}`} {
		request := httptest.NewRequest(http.MethodPost, "/api/studio/media-urls", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+fixture.sessionToken)
		recorder := httptest.NewRecorder()
		fixture.engine.ServeHTTP(recorder, request)
		assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestStudioMediaURLsRespectFeatureAccess(t *testing.T) {
	fixture := newVideoStudioMediaAuthFixture(t)
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("video_studio"), map[string]string{"access_mode": video_studio_setting.AccessModeOff}))
	request := httptest.NewRequest(http.MethodPost, "/api/studio/media-urls", strings.NewReader(fmt.Sprintf(`{"paths":["/api/video-studio/assets/%d/content"]}`, fixture.ownedAssetID)))
	request.Header.Set("Authorization", "Bearer "+fixture.sessionToken)
	recorder := httptest.NewRecorder()
	fixture.engine.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "video_studio_access_denied")
}
