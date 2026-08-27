package service

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkImageStudioBatchDispatchAttempted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("single output keeps retries enabled", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		require.NoError(t, SetImageStudioGenerationID(ctx, 1))
		MarkImageStudioBatchDispatchAttempted(ctx, 1)
		assert.False(t, ShouldSkipRetryAfterImageStudioBatchDispatch(ctx))
	})

	t.Run("batch output disables retries after dispatch", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		require.NoError(t, SetImageStudioGenerationID(ctx, 1))
		MarkImageStudioBatchDispatchAttempted(ctx, 2)
		assert.True(t, ShouldSkipRetryAfterImageStudioBatchDispatch(ctx))
	})

	t.Run("non studio batch keeps retries enabled", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		MarkImageStudioBatchDispatchAttempted(ctx, 2)
		assert.False(t, ShouldSkipRetryAfterImageStudioBatchDispatch(ctx))
	})
}
