package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoStudioAssetStoreKeepsRequestContextInjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	injected := &service.S3VideoAssetStore{}
	ctx.Set(videoStudioAssetStoreContextKey, injected)

	store, err := videoStudioAssetStore(ctx)
	require.NoError(t, err)
	require.Same(t, injected, store)
}
