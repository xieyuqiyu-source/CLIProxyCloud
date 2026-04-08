package main

import (
	"log"

	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/config"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/database"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/handlers"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/middleware"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/server"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/services"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/storage"
)

func main() {
	cfg := config.Load()

	db, err := database.Open(cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}

	if err := database.Migrate(db); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	storageSvc, err := storage.New(cfg.StorageRoot, cfg.StorageKey)
	if err != nil {
		log.Fatalf("init storage: %v", err)
	}

	planSvc := services.NewPlanService(db)
	if err := planSvc.SeedDefaults(); err != nil {
		log.Fatalf("seed plans: %v", err)
	}

	userSvc := services.NewUserService(db)
	if err := userSvc.EnsureAdmin(cfg.AdminEmail, cfg.AdminPassword); err != nil {
		log.Fatalf("ensure admin: %v", err)
	}

	authSvc := services.NewAuthService(db, cfg.JWTSecret, planSvc)
	deviceSvc := services.NewDeviceService(db)
	authFileSvc := services.NewAuthFileService(db, storageSvc)
	appReleaseSvc := services.NewAppReleaseService(cfg.StorageRoot, cfg.PublicBaseURL)
	paymentSvc, err := services.NewPaymentService(db, planSvc, cfg.Payment)
	if err != nil {
		log.Fatalf("init payment service: %v", err)
	}
	if err := paymentSvc.SeedDefaults(); err != nil {
		log.Fatalf("seed payment products: %v", err)
	}

	handler := handlers.New(authSvc, userSvc, planSvc, deviceSvc, authFileSvc, appReleaseSvc, paymentSvc)
	authMiddleware := middleware.NewAuthMiddleware(authSvc, userSvc)
	router := server.NewRouter(handler, authMiddleware, cfg.StorageRoot)

	log.Printf("CLIProxyCloud listening on %s", cfg.Addr)
	if err := router.Run(cfg.Addr); err != nil {
		log.Fatalf("run server: %v", err)
	}
}
