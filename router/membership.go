package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func registerMembershipRoutes(api *gin.RouterGroup, bodyLimit gin.HandlerFunc) {
	membership := api.Group("/membership", middleware.UserAuth(), middleware.DisableCache())
	membership.GET("/catalog", controller.GetMembershipCatalog)
	membership.GET("/self", controller.GetMembershipSelf)
	membership.GET("/orders", controller.GetMembershipOrders)
	membership.GET("/orders/:trade_no", controller.GetMembershipOrder)
	membership.POST("/orders", middleware.CriticalRateLimit(), controller.CreateMembershipOrder)
	membership.POST("/orders/:trade_no/pay", middleware.CriticalRateLimit(), controller.ResumeMembershipPayment)
	admin := api.Group("/membership/admin", middleware.RootAuth(), middleware.DisableCache())
	admin.GET("/config", controller.AdminGetMembershipConfig)
	admin.PUT("/config", middleware.CriticalRateLimit(), controller.AdminPublishMembershipConfig)
	admin.POST("/publish", middleware.CriticalRateLimit(), controller.AdminPublishMembershipConfig)
	admin.GET("/billing-reviews", controller.AdminListMembershipBillingReviews)
	admin.GET("/billing-reviews/:request_id", controller.AdminGetMembershipBillingReview)
	admin.POST("/billing-reviews/:request_id/resolve", middleware.CriticalRateLimit(), controller.AdminResolveMembershipBillingReview)
	admin.GET("/orders", controller.AdminGetMembershipOrders)
	admin.POST("/orders/:trade_no/resolve", middleware.CriticalRateLimit(), controller.AdminResolveMembershipOrder)
	api.GET("/membership/epay/notify", controller.MembershipEpayNotify)
	api.POST("/membership/epay/notify", bodyLimit, controller.MembershipEpayNotify)
}
