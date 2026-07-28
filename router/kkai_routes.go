package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func registerKKAIRoutes(apiRouter *gin.RouterGroup, anonymousRequestBodyLimit gin.HandlerFunc) {
	apiRouter.GET("/status/groups", middleware.UserAuth(), controller.GetKKAIGroupStatus)
	registerVideoStudioAPIRoutes(apiRouter)

	internalRoute := apiRouter.Group("/internal")
	internalRoute.Use(middleware.KKAIBalanceAdjustmentAuth(), anonymousRequestBodyLimit)
	internalRoute.POST("/balance-adjustments", controller.CreateKKAIBalanceAdjustment)
}

func registerVideoStudioAPIRoutes(apiRouter *gin.RouterGroup) {
	videoStudio := apiRouter.Group("/video-studio")
	videoStudio.Use(middleware.UserAuth(), middleware.VideoStudioAccess())
	{
		videoStudio.GET("/models", controller.ListVideoStudioModels)
		videoStudio.GET("/token", controller.GetVideoStudioTokenStatus)
		videoStudio.POST("/token", controller.EnsureVideoStudioToken)
		videoStudio.GET("/samples", controller.ListVideoStudioSamples)
		videoStudio.GET("/samples/:id", controller.GetVideoStudioSample)
		videoStudio.POST("/uploads", controller.CreateVideoStudioUpload)
		videoStudio.GET("/uploads/:id", controller.GetVideoStudioUpload)
		videoStudio.POST("/uploads/:id/parts/:part_number", controller.SignVideoStudioUploadPart)
		videoStudio.GET("/uploads/:id/parts", controller.ListVideoStudioUploadParts)
		videoStudio.POST("/uploads/:id/complete", controller.CompleteVideoStudioUpload)
		videoStudio.DELETE("/uploads/:id", controller.AbortVideoStudioUpload)
		videoStudio.GET("/generations", controller.ListVideoStudioGenerations)
		videoStudio.GET("/generations/:id", controller.GetVideoStudioGeneration)
		videoStudio.DELETE("/generations/:id", controller.DeleteVideoStudioGeneration)
		videoStudio.GET("/assets/:id", controller.GetVideoStudioAsset)
		videoStudio.DELETE("/assets/:id", controller.DeleteVideoStudioReferenceAsset)
	}
	videoStudioMedia := apiRouter.Group("/video-studio")
	videoStudioMedia.Use(middleware.VideoStudioMediaAuth(), middleware.VideoStudioAccess())
	{
		videoStudioMedia.GET("/assets/:id/content", controller.GetVideoStudioAssetContent)
		videoStudioMedia.GET("/assets/:id/download", controller.DownloadVideoStudioAsset)
	}

	adminVideoStudio := apiRouter.Group("/admin/video-studio")
	adminVideoStudio.Use(middleware.AdminAuth())
	{
		adminVideoStudio.GET("/model-profiles", controller.AdminListVideoStudioModelProfiles)
		adminVideoStudio.GET("/model-profiles/:id", controller.AdminGetVideoStudioModelProfile)
		adminVideoStudio.POST("/model-profiles", controller.AdminCreateVideoStudioModelProfile)
		adminVideoStudio.PUT("/model-profiles/:id", controller.AdminUpdateVideoStudioModelProfile)
		adminVideoStudio.DELETE("/model-profiles/:id", controller.AdminDeleteVideoStudioModelProfile)
		adminVideoStudio.GET("/samples", controller.AdminListVideoStudioSamples)
		adminVideoStudio.GET("/samples/:id", controller.AdminGetVideoStudioSample)
		adminVideoStudio.POST("/samples", controller.AdminCreateVideoStudioSample)
		adminVideoStudio.PUT("/samples/:id", controller.AdminUpdateVideoStudioSample)
		adminVideoStudio.DELETE("/samples/:id", controller.AdminDeleteVideoStudioSample)
		adminVideoStudio.POST("/uploads", controller.AdminCreateVideoStudioUpload)
		adminVideoStudio.GET("/uploads/:id", controller.AdminGetVideoStudioUpload)
		adminVideoStudio.POST("/uploads/:id/parts/:part_number", controller.AdminSignVideoStudioUploadPart)
		adminVideoStudio.GET("/uploads/:id/parts", controller.AdminListVideoStudioUploadParts)
		adminVideoStudio.POST("/uploads/:id/complete", controller.AdminCompleteVideoStudioUpload)
		adminVideoStudio.DELETE("/uploads/:id", controller.AdminAbortVideoStudioUpload)
		adminVideoStudio.POST("/outbox/:id/redrive", middleware.CriticalRateLimit(), controller.AdminRedriveVideoStudioOutboxEvent)
	}
}
