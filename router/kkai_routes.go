package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func registerKKAIRoutes(apiRouter *gin.RouterGroup, anonymousRequestBodyLimit gin.HandlerFunc) {
	apiRouter.GET("/status/groups", middleware.UserAuth(), controller.GetKKAIGroupStatus)

	internalRoute := apiRouter.Group("/internal")
	internalRoute.Use(middleware.KKAIBalanceAdjustmentAuth(), anonymousRequestBodyLimit)
	internalRoute.POST("/balance-adjustments", controller.CreateKKAIBalanceAdjustment)
}
