package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// registerResellerRoutes installs owner and platform management endpoints. Owner
// routes are deliberately separate from platform AdminAuth routes and always
// require the Host-derived tenant membership middleware.
func registerResellerRoutes(api *gin.RouterGroup, bodyLimit gin.HandlerFunc) {
	api.GET("/site-config", middleware.DisableCache(), controller.GetTenantSiteConfig)
	api.GET("/site-logo", controller.GetTenantSiteLogo)
	schemaGate := middleware.TenantBusinessSchemaGate(model.DB)
	applications := api.Group("/reseller/applications", middleware.UserAuth(), schemaGate, middleware.DisableCache())
	applications.POST("", bodyLimit, controller.CreateResellerApplication)
	applications.GET("", controller.ListResellerApplications)

	owner := api.Group("/reseller", middleware.UserAuth(), schemaGate, middleware.TenantAdminAuth(), middleware.DisableCache())
	owner.GET("/me", controller.GetResellerMe)
	owner.GET("/profile", controller.GetResellerProfile)
	owner.PATCH("/profile", bodyLimit, controller.UpdateResellerProfile)
	owner.POST("/brand/logo", bodyLimit, controller.UploadResellerLogo)
	owner.GET("/catalog", controller.GetResellerCatalog)
	owner.GET("/supply", controller.GetResellerSupply)
	owner.PATCH("/catalog", bodyLimit, controller.UpdateResellerCatalog)
	owner.GET("/prices", controller.GetResellerPrices)
	owner.PATCH("/prices", bodyLimit, controller.UpdateResellerPrices)
	owner.GET("/customers", controller.GetResellerCustomers)
	owner.GET("/earnings", controller.GetResellerEarnings)
	owner.GET("/withdrawals", controller.GetResellerWithdrawals)
	owner.POST("/withdrawals", bodyLimit, middleware.UserCriticalRateLimit("reseller-withdrawal"), controller.RequestResellerWithdrawal)

	platform := api.Group("/admin/resellers", middleware.AdminAuth(), schemaGate, middleware.DisableCache())
	platform.GET("", controller.AdminListResellers)
	platform.PATCH("/applications/:id", bodyLimit, controller.AdminReviewResellerApplication)
	platform.PATCH("/:id/status", bodyLimit, controller.AdminUpdateResellerStatus)
	platform.POST("/:id/domain", bodyLimit, controller.AdminProvisionResellerDomain)
	platform.GET("/withdrawals", controller.AdminListResellerWithdrawals)
	platform.PATCH("/withdrawals/:id", bodyLimit, controller.AdminReviewResellerWithdrawal)
	platform.POST("/:id/settlements/reverse", bodyLimit, controller.AdminReverseResellerSettlement)
	platform.POST("/:id/payments/:topup_id/reverse", bodyLimit, controller.AdminReverseResellerPayment)
	platform.POST("/:id/consumptions/:request_id/reconcile", bodyLimit, controller.AdminReconcileResellerConsumption)
}
