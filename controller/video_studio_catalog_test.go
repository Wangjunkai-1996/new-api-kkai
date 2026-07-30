package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAdminListVideoStudioModelCandidatesReturnsAbilityNamesDirectly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupVideoStudioTokenControllerTest(t)

	request := func() []string {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/admin/video-studio/model-candidates", nil)
		AdminListVideoStudioModelCandidates(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Success bool     `json:"success"`
			Data    []string `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success)
		return response.Data
	}

	require.Equal(t, []string{"video-model-a"}, request())
	require.NoError(t, db.Model(&model.Ability{}).Where("model = ?", "video-model-a").Update("enabled", false).Error)
	empty := request()
	require.NotNil(t, empty)
	require.Empty(t, empty)
}
