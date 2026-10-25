package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func registerWalletPaymentRoutes(api *gin.RouterGroup) {
	user := api.Group("/user/topup", middleware.UserAuth(), middleware.DisableCache())
	user.Use(func(c *gin.Context) {
		c.Header("Referrer-Policy", "no-referrer")
		c.Next()
	})
	user.GET("/current", controller.GetWalletTopUpCurrent)
	user.GET("/summary", controller.GetWalletFundingSummary)
	user.GET("/orders/:trade_no", controller.GetWalletTopUpOrder)
	write := user.Group("", middleware.WalletPaymentMutationGuard())
	write.POST("/current/restore", middleware.WalletPaymentRateLimit("state"), controller.RestoreWalletTopUpCurrent)
	write.POST("/quote", middleware.WalletPaymentRateLimit("quote"), controller.QuoteWalletTopUp)
	write.POST("/orders", middleware.WalletPaymentRateLimit("checkout"), controller.CreateWalletTopUpOrder)
	write.POST("/orders/:trade_no/resume", middleware.WalletPaymentRateLimit("checkout"), controller.ResumeWalletTopUpOrder)
	write.POST("/orders/:trade_no/cancel", middleware.WalletPaymentRateLimit("state"), controller.CancelWalletTopUpOrder)
	write.POST("/orders/:trade_no/end", middleware.WalletPaymentRateLimit("state"), controller.EndWalletTopUpOrder)
	write.POST("/orders/:trade_no/reconcile", middleware.WalletPaymentRateLimit("check"), controller.ReconcileWalletTopUpOrder)
	write.POST("/current/acknowledge", middleware.WalletPaymentRateLimit("state"), controller.AcknowledgeWalletTopUpCurrent)

	admin := api.Group("/user/topup", middleware.AdminAuth(), middleware.DisableCache())
	admin.GET("/orders/:trade_no/admin", controller.GetAdminWalletTopUpOrder)
	admin.POST("/orders/:trade_no/admin-reconcile", middleware.WalletPaymentMutationGuard(), middleware.WalletPaymentRateLimit("check"), controller.AdminReconcileWalletTopUpOrder)

	runs := api.Group("/user/topup/reconcile-runs", middleware.RootAuth(), middleware.DisableCache(), middleware.WalletPaymentBatchGuard())
	runs.GET("/:run_id", controller.GetWalletPaymentRun)
	runs.POST("/report", middleware.UserCriticalRateLimit("wallet-payment-report"), controller.ReportWalletPaymentRun)
	runs.POST("", middleware.UserCriticalRateLimit("wallet-payment-batch"), controller.StartWalletPaymentRun)
	runs.POST("/:run_id/pause", middleware.UserCriticalRateLimit("wallet-payment-batch"), controller.PauseWalletPaymentRun)
	runs.POST("/:run_id/resume", middleware.UserCriticalRateLimit("wallet-payment-batch"), controller.ResumeWalletPaymentRun)
}
