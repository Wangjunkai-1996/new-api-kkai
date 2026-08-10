package service

import "github.com/gin-gonic/gin"

// MarkKKAIPolicyNoRetry records a definitive local policy rejection without
// creating a new upstream policy incident.
func MarkKKAIPolicyNoRetry(c *gin.Context) {
	markKKAIPolicyContext(c, "")
}
