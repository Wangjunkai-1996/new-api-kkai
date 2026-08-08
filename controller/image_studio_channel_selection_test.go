package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageStudioPrepareExcludesReplicateBeforeMultiReferenceRelay(t *testing.T) {
	db, token := newImageStudioRelayTestDB(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCacheEnabled })
	require.NoError(t, i18n.Init())
	var channel model.Channel
	require.NoError(t, db.First(&channel).Error)
	require.NoError(t, db.Model(&channel).Update("type", constant.ChannelTypeReplicate).Error)
	require.NoError(t, model.SyncChannelCacheOnce())

	references := []service.ImageStudioReferenceMetadata{
		{SHA256: strings.Repeat("a", 64), SizeBytes: 100},
		{SHA256: strings.Repeat("b", 64), SizeBytes: 200},
	}
	body, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: token.Id, Model: service.ImageStudioEditModel, Prompt: "edit",
		Mode: service.ImageStudioModeEdit, References: references,
	})
	require.NoError(t, err)

	selected := 0
	engine := gin.New()
	engine.POST("/pg/images/edits/quote", func(c *gin.Context) {
		c.Set("id", 7)
		c.Set("user_group", "default")
	}, PrepareImageStudioRequest, middleware.Distribute(), func(c *gin.Context) {
		selected = common.GetContextKeyInt(c, constant.ContextKeyChannelId)
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/pg/images/edits/quote", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	assert.Zero(t, selected)
	assert.Contains(t, response.Body.String(), service.ErrNoChannelSupportsImageReferences.Error())
}
