package service

import "github.com/gin-gonic/gin"

const imageStudioBatchDispatchAttemptedContextKey = "image_studio_batch_dispatch_attempted"

func MarkImageStudioBatchDispatchAttempted(c *gin.Context, requestedCount int) {
	if c == nil || requestedCount <= 1 || ImageStudioGenerationID(c) <= 0 {
		return
	}
	c.Set(imageStudioBatchDispatchAttemptedContextKey, true)
}

func ShouldSkipRetryAfterImageStudioBatchDispatch(c *gin.Context) bool {
	return c != nil && c.GetBool(imageStudioBatchDispatchAttemptedContextKey)
}
