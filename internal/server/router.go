package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/handlers"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/middleware"
)

func NewRouter(handler *handlers.Handler, authMiddleware *middleware.AuthMiddleware) *gin.Engine {
	router := gin.Default()

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	v1 := router.Group("/api/v1")
	{
		auth := v1.Group("/auth")
		auth.POST("/register", handler.Register)
		auth.POST("/login", handler.Login)

		protected := v1.Group("/")
		protected.Use(authMiddleware.RequireAuth())
		protected.GET("/me", handler.Me)
		protected.GET("/me/plan", handler.MyPlan)
		protected.GET("/me/features", handler.MyFeatures)
		protected.POST("/devices/register", handler.RegisterDevice)
		protected.GET("/devices/me", handler.MyDevice)

		protected.GET("/me/auth-files", handler.ListMyAuthFiles)
		protected.POST("/me/auth-files/upload", handler.UploadMyAuthFile)
		protected.GET("/me/auth-files/:id/download", handler.DownloadMyAuthFile)
		protected.DELETE("/me/auth-files/:id", handler.DeleteMyAuthFile)

		protected.GET("/shared/auth-files", handler.ListSharedAuthFiles)
		protected.GET("/shared/auth-files/:id/download", handler.DownloadSharedAuthFile)

		admin := protected.Group("/admin")
		admin.POST("/shared-auth-files/upload", handler.AdminUploadSharedAuthFile)
		admin.PATCH("/users/:id/plan", handler.AdminAssignPlan)
	}

	return router
}
