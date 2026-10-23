package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func registerInvoiceRoutes(api *gin.RouterGroup) {
	gate := middleware.InvoiceSchemaGate(model.DB)
	user := api.Group("/invoices", middleware.UserAuth(), middleware.DisableCache())
	// Policy remains readable before the explicit schema expansion so the UI can
	// explain the disabled capability instead of presenting a broken form.
	user.GET("/policy", controller.InvoicePolicy)
	user.Use(gate)
	user.GET("", controller.InvoiceList(false))
	user.GET("/summary", controller.InvoiceSummary(false))
	user.GET("/eligible-orders", controller.InvoiceCandidates)
	user.POST("/quote", controller.InvoiceQuote)
	user.POST("", middleware.UserCriticalRateLimit("invoice-create"), controller.InvoiceCreate)
	user.GET("/notifications", controller.InvoiceNotifications(false))
	user.POST("/notifications/read-all", controller.InvoiceReadNotifications(false))
	user.POST("/notifications/:event_id/read", controller.InvoiceReadNotifications(false))
	user.GET("/:public_id", controller.InvoiceDetail(false))
	user.GET("/:public_id/files/:file_id", controller.InvoiceFile(false))
	user.POST("/:public_id/attachments", middleware.UserCriticalRateLimit("invoice-upload"), controller.InvoiceUpload(false))
	user.POST("/:public_id/confirm", controller.InvoiceAction(false, "confirm"))
	user.POST("/:public_id/disputes", controller.InvoiceAction(false, "dispute"))
	user.POST("/:public_id/disputes/:dispute_id/messages", controller.InvoiceAction(false, "message"))

	profiles := api.Group("/invoice-profiles", middleware.UserAuth(), middleware.DisableCache(), gate)
	profiles.GET("", controller.InvoiceProfiles)
	profiles.GET("/:id", controller.InvoiceGetProfile)
	profiles.POST("", controller.InvoiceSaveProfile)
	profiles.PATCH("/:id", controller.InvoiceSaveProfile)
	profiles.DELETE("/:id", controller.InvoiceDeleteProfile)

	admin := api.Group("/admin/invoices", middleware.AdminAuth(), middleware.DisableCache(), gate)
	admin.GET("", controller.InvoiceList(true))
	admin.GET("/summary", controller.InvoiceSummary(true))
	admin.GET("/notifications", controller.InvoiceNotifications(true))
	admin.POST("/notifications/read-all", controller.InvoiceReadNotifications(true))
	admin.POST("/notifications/:event_id/read", controller.InvoiceReadNotifications(true))
	admin.GET("/:public_id", controller.InvoiceDetail(true))
	admin.GET("/:public_id/files/:file_id", controller.InvoiceFile(true))
	admin.POST("/:public_id/attachments", middleware.UserCriticalRateLimit("invoice-admin-upload"), controller.InvoiceUpload(true))
	admin.POST("/:public_id/approve", controller.InvoiceAction(true, "approve"))
	admin.POST("/:public_id/reject", controller.InvoiceAction(true, "reject"))
	admin.POST("/:public_id/hold", controller.InvoiceAction(true, "hold"))
	admin.POST("/:public_id/resume", controller.InvoiceAction(true, "resume"))
	admin.POST("/:public_id/deliveries", controller.InvoiceAction(true, "deliver"))
	admin.POST("/:public_id/disputes/:dispute_id/reply", controller.InvoiceAction(true, "reply"))
	admin.POST("/:public_id/disputes/:dispute_id/reissue", controller.InvoiceAction(true, "reissue"))
}
