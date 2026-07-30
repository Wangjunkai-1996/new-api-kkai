package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/video_studio_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetStatusExposesVideoStudioUploadLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)

	GetStatus(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data struct {
			VideoStudio struct {
				ProcessingAvailable bool                              `json:"processing_available"`
				UploadLimits        video_studio_setting.UploadLimits `json:"upload_limits"`
			} `json:"video_studio"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, video_studio_setting.Get().WorkerEnabled, response.Data.VideoStudio.ProcessingAvailable)
	require.Equal(t, video_studio_setting.Get().UploadLimits(), response.Data.VideoStudio.UploadLimits)
}
