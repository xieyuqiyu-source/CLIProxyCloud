package server

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/handlers"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/middleware"
)

func NewRouter(handler *handlers.Handler, authMiddleware *middleware.AuthMiddleware, storageRoot string) *gin.Engine {
	router := gin.Default()
	appRoot := filepath.Dir(storageRoot)
	webRoot := filepath.Join(appRoot, "web")
	router.Use(func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if strings.HasPrefix(origin, "http://localhost:") ||
				strings.HasPrefix(origin, "http://127.0.0.1:") ||
				strings.HasPrefix(origin, "http://192.168.") ||
				origin == "tauri://localhost" {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
			}
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Expose-Headers", "Content-Disposition")
			c.Header("Access-Control-Allow-Credentials", "true")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.Static("/assets", filepath.Join(webRoot, "assets"))
	router.StaticFile("/robots.txt", filepath.Join(webRoot, "robots.txt"))
	router.StaticFile("/sitemap.xml", filepath.Join(webRoot, "sitemap.xml"))
	router.GET("/", func(c *gin.Context) {
		c.File(filepath.Join(webRoot, "index.html"))
	})
	router.StaticFS("/downloads", gin.Dir(filepath.Join(storageRoot, "downloads"), false))

	v1 := router.Group("/api/v1")
	{
		auth := v1.Group("/auth")
		auth.POST("/register", handler.Register)
		auth.POST("/register/verify", handler.VerifyRegister)
		auth.POST("/login", handler.Login)
		auth.POST("/login/verify", handler.VerifyLogin)
		auth.POST("/device-login", handler.TrustedDeviceLogin)
		v1.POST("/pay/xunhu/notify", handler.XunhuPaymentNotify)
		v1.POST("/quota-cards/:id/check", handler.CheckQuotaCard)
		v1.POST("/quota-cards/:id/usage", handler.ReportQuotaCardUsage)

		protected := v1.Group("/")
		protected.Use(authMiddleware.RequireAuth())
		protected.GET("/me", handler.Me)
		protected.POST("/me/logout", handler.Logout)
		protected.POST("/me/change-password", handler.ChangePassword)
		protected.GET("/me/plan", handler.MyPlan)
		protected.GET("/me/features", handler.MyFeatures)
		protected.GET("/pay/products", handler.ListPaymentProducts)
		protected.POST("/pay/quote", handler.QuotePaymentOrder)
		protected.POST("/pay/orders", handler.CreatePaymentOrder)
		protected.GET("/pay/orders/:orderNo", handler.GetPaymentOrder)
		protected.POST("/pay/orders/:orderNo/cancel", handler.CancelPaymentOrder)
		protected.POST("/devices/register", handler.RegisterDevice)
		protected.GET("/devices/me", handler.MyDevice)

		protected.GET("/me/auth-files", handler.ListMyAuthFiles)
		protected.POST("/me/auth-files/upload", handler.UploadMyAuthFile)
		protected.DELETE("/me/auth-files", handler.DeleteAllMyAuthFiles)
		protected.GET("/me/auth-files/:id/download", handler.DownloadMyAuthFile)
		protected.DELETE("/me/auth-files/:id", handler.DeleteMyAuthFile)

		protected.GET("/shared/auth-files", handler.ListSharedAuthFiles)
		protected.GET("/shared/auth-files/sync-package", handler.SharedAuthSyncPackage)
		protected.GET("/shared/auth-files/:id/download", handler.DownloadSharedAuthFile)
		protected.POST("/shared/quota-cards/:id/consume", handler.ConsumeSharedQuotaCard)
		protected.POST("/shared/quota-cards/:id/api-call", handler.SharedQuotaCardAPICall)
		protected.GET("/agent/tasks/poll", handler.AgentPollTask)
		protected.POST("/agent/tasks/:id/result", handler.AgentSubmitTaskResult)

		admin := protected.Group("/admin")
		admin.GET("/users", handler.AdminListUsers)
		admin.GET("/plans", handler.AdminListPlans)
		admin.GET("/agent-status", handler.AdminAgentStatus)
		admin.GET("/agent-tasks", handler.AdminListAgentTasks)
		admin.POST("/agent-tasks", handler.AdminCreateAgentTask)
		admin.GET("/pay/products", handler.AdminListPaymentProducts)
		admin.POST("/pay/products", handler.AdminCreatePaymentProduct)
		admin.PATCH("/pay/products/:id", handler.AdminUpdatePaymentProduct)
		admin.GET("/pay/orders", handler.AdminListPaymentOrders)
		admin.POST("/pay/orders/:orderNo/regrant", handler.AdminRegrantPaymentOrder)
		admin.POST("/app-releases/upload", handler.AdminUploadAppRelease)
		admin.POST("/shared-auth-files/upload", handler.AdminUploadSharedAuthFile)
		admin.DELETE("/shared-auth-files", handler.AdminDeleteAllSharedAuthFiles)
		admin.DELETE("/shared-auth-files/:id", handler.AdminDeleteSharedAuthFile)
		admin.PATCH("/users/:id/plan", handler.AdminAssignPlan)
		admin.PATCH("/users/:id/role", handler.AdminUpdateUserRole)
	}

	return router
}
