package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func registerRequestTicketRoutes(api *gin.RouterGroup) {
	schemaGate := middleware.RequestTicketSchemaGate(model.DB)
	user := api.Group("/requests", middleware.UserAuth(), schemaGate, middleware.DisableCache())
	user.GET("", controller.ListRequestTickets)
	user.POST("", middleware.UserCriticalRateLimit("request-ticket"), controller.CreateRequestTicket)
	user.GET("/notifications", controller.ListRequestTicketNotifications)
	user.POST("/notifications/read-all", controller.ReadAllRequestTicketNotifications)
	user.POST("/notifications/:id/read", controller.ReadRequestTicketNotification)
	user.GET("/:id", controller.GetRequestTicket)

	admin := api.Group("/requests/admin", middleware.AdminAuth(), schemaGate, middleware.DisableCache())
	admin.GET("", controller.AdminListRequestTickets)
	admin.GET("/:id", controller.AdminGetRequestTicket)
	admin.PATCH("/:id/status", controller.AdminTransitionRequestTicket)
}
