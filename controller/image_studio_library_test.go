package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetImageStudioGenerationInputContract(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:image-input-controller-%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.KKAIImageGeneration{}, &model.KKAIImageExecution{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	generation := model.KKAIImageGeneration{UserID: 7, RequestID: "input-contract"}
	require.NoError(t, db.Create(&generation).Error)
	payload, err := common.Marshal(service.ImageExecutionPayload{
		Version: 1, Submission: service.NormalizedImageStudioSubmission{UserID: 7, Mode: service.ImageStudioModeGeneration, QuoteToken: "private-quote", TokenID: 12345},
		ClientIP: "private-client-ip",
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.KKAIImageExecution{GenerationID: generation.ID, Payload: string(payload)}).Error)
	for _, test := range []struct {
		name   string
		userID int
		id     string
		status int
	}{
		{"owner", 7, strconv.FormatInt(generation.ID, 10), http.StatusOK},
		{"another user", 8, strconv.FormatInt(generation.ID, 10), http.StatusNotFound},
		{"invalid ID", 7, "invalid", http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/image-studio/generations/"+test.id+"/input", nil)
			c.Params = gin.Params{{Key: "id", Value: test.id}}
			c.Set("id", test.userID)
			GetImageStudioGenerationInput(c)
			require.Equal(t, test.status, recorder.Code, recorder.Body.String())
			assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			assert.NotContains(t, recorder.Body.String(), "private-")
			assert.NotContains(t, recorder.Body.String(), "12345")
			if test.status != http.StatusOK {
				return
			}
			var response struct {
				Success bool           `json:"success"`
				Data    map[string]any `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.True(t, response.Success)
			assert.Equal(t, map[string]any{
				"generation_id": float64(generation.ID), "mode": "generation", "references_available": true, "references": []any{},
			}, response.Data)
		})
	}
}
