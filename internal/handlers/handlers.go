package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/middleware"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/models"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/services"
)

type Handler struct {
	authSvc       *services.AuthService
	userSvc       *services.UserService
	planSvc       *services.PlanService
	deviceSvc     *services.DeviceService
	authFileSvc   *services.AuthFileService
	appReleaseSvc *services.AppReleaseService
}

func New(
	authSvc *services.AuthService,
	userSvc *services.UserService,
	planSvc *services.PlanService,
	deviceSvc *services.DeviceService,
	authFileSvc *services.AuthFileService,
	appReleaseSvc *services.AppReleaseService,
) *Handler {
	return &Handler{
		authSvc:       authSvc,
		userSvc:       userSvc,
		planSvc:       planSvc,
		deviceSvc:     deviceSvc,
		authFileSvc:   authFileSvc,
		appReleaseSvc: appReleaseSvc,
	}
}

func (h *Handler) Register(c *gin.Context) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	user, err := h.authSvc.Register(strings.TrimSpace(strings.ToLower(req.Email)), req.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"user": user})
}

func (h *Handler) Login(c *gin.Context) {
	var req struct {
		Email      string `json:"email"`
		Password   string `json:"password"`
		DeviceID   string `json:"device_id"`
		DeviceName string `json:"device_name"`
		Platform   string `json:"platform"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	user, token, err := h.authSvc.Login(strings.TrimSpace(strings.ToLower(req.Email)), req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	plan, features, err := h.planSvc.ResolveUserPlan(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	device, err := h.deviceSvc.RegisterOrTouch(user, features, req.DeviceID, req.DeviceName, req.Platform)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"token":    token,
		"user":     user,
		"plan":     plan,
		"features": features,
		"device":   device,
	})
}

func (h *Handler) Me(c *gin.Context) {
	user := middleware.CurrentUser(c)
	plan, features, err := h.planSvc.ResolveUserPlan(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user":     user,
		"plan":     plan,
		"features": features,
	})
}

func (h *Handler) ChangePassword(c *gin.Context) {
	user := middleware.CurrentUser(c)
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if err := h.authSvc.ChangePassword(user.ID, req.CurrentPassword, req.NewPassword); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) MyPlan(c *gin.Context) {
	user := middleware.CurrentUser(c)
	plan, _, err := h.planSvc.ResolveUserPlan(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"plan": plan})
}

func (h *Handler) MyFeatures(c *gin.Context) {
	user := middleware.CurrentUser(c)
	_, features, err := h.planSvc.ResolveUserPlan(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"features": features})
}

func (h *Handler) RegisterDevice(c *gin.Context) {
	user := middleware.CurrentUser(c)
	var req struct {
		DeviceID   string `json:"device_id"`
		DeviceName string `json:"device_name"`
		Platform   string `json:"platform"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	_, features, err := h.planSvc.ResolveUserPlan(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	device, err := h.deviceSvc.RegisterOrTouch(user, features, req.DeviceID, req.DeviceName, req.Platform)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"device": device})
}

func (h *Handler) MyDevice(c *gin.Context) {
	user := middleware.CurrentUser(c)
	deviceID := c.Query("device_id")
	device, err := h.deviceSvc.FindByDeviceID(deviceID)
	if err != nil || device.UserID != user.ID {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"device": device})
}

func (h *Handler) ListMyAuthFiles(c *gin.Context) {
	user := middleware.CurrentUser(c)
	files, err := h.authFileSvc.ListPersonal(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"files": files})
}

func (h *Handler) UploadMyAuthFile(c *gin.Context) {
	user := middleware.CurrentUser(c)
	plan, features, err := h.planSvc.ResolveUserPlan(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !features.AllowPersonalCloudSync && user.Role != models.UserRoleAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "your plan does not allow personal cloud sync"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	authFile, err := h.authFileSvc.Upload(models.AuthOwnerTypeUser, &user.ID, models.AuthSourcePersonal, &plan.PlanCode, file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"file": authFile})
}

func (h *Handler) DownloadMyAuthFile(c *gin.Context) {
	user := middleware.CurrentUser(c)
	authFileID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	file, err := h.authFileSvc.FindPersonal(user.ID, uint(authFileID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "auth file not found"})
		return
	}
	content, err := h.authFileSvc.ReadContent(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", "attachment; filename="+file.FileName)
	c.Data(http.StatusOK, "application/json", content)
}

func (h *Handler) DeleteMyAuthFile(c *gin.Context) {
	user := middleware.CurrentUser(c)
	authFileID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.authFileSvc.DeletePersonal(user.ID, uint(authFileID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) DeleteAllMyAuthFiles(c *gin.Context) {
	user := middleware.CurrentUser(c)
	deleted, err := h.authFileSvc.DeleteAllPersonal(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "deleted": deleted})
}

func (h *Handler) ListSharedAuthFiles(c *gin.Context) {
	user := middleware.CurrentUser(c)
	_, features, err := h.planSvc.ResolveUserPlan(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !features.AllowSharedPool && user.Role != models.UserRoleAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "your plan does not allow shared auth files"})
		return
	}
	files, err := h.authFileSvc.ListSharedByStrategy(features)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"files": files})
}

func (h *Handler) SharedAuthSyncPackage(c *gin.Context) {
	user := middleware.CurrentUser(c)
	_, features, err := h.planSvc.ResolveUserPlan(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !features.AllowSharedPool && user.Role != models.UserRoleAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "your plan does not allow shared auth files"})
		return
	}

	files, err := h.authFileSvc.ListSharedByStrategy(features)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"mode":                  features.SharedPoolMode,
		"max_files":             features.SharedPoolMaxFiles,
		"refresh_after_minutes": features.SharedPoolRefreshMins,
		"files":                 files,
	})
}

func (h *Handler) DownloadSharedAuthFile(c *gin.Context) {
	user := middleware.CurrentUser(c)
	_, features, err := h.planSvc.ResolveUserPlan(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !features.AllowSharedPool && user.Role != models.UserRoleAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "your plan does not allow shared auth files"})
		return
	}
	authFileID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	file, err := h.authFileSvc.FindShared(uint(authFileID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "shared auth file not found"})
		return
	}
	content, err := h.authFileSvc.ReadContent(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", "attachment; filename="+file.FileName)
	c.Data(http.StatusOK, "application/json", content)
}

func (h *Handler) AdminUploadSharedAuthFile(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if err := h.userSvc.RequireAdmin(user); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	planCode := "vip2"
	authFile, err := h.authFileSvc.Upload(models.AuthOwnerTypeShared, nil, models.AuthSourceShared, &planCode, file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"file": authFile})
}

func (h *Handler) AdminDeleteSharedAuthFile(c *gin.Context) {
	if err := h.userSvc.RequireAdmin(middleware.CurrentUser(c)); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	authFileID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.authFileSvc.DeleteShared(uint(authFileID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) AdminDeleteAllSharedAuthFiles(c *gin.Context) {
	if err := h.userSvc.RequireAdmin(middleware.CurrentUser(c)); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	deleted, err := h.authFileSvc.DeleteAllShared()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "deleted": deleted})
}

func (h *Handler) AdminListUsers(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if err := h.userSvc.RequireAdmin(user); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	users, err := h.userSvc.ListUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	type userSummary struct {
		User     *models.User          `json:"user"`
		Plan     *models.Plan          `json:"plan"`
		Features services.FeatureFlags `json:"features"`
	}

	items := make([]userSummary, 0, len(users))
	for idx := range users {
		current := users[idx]
		plan, features, err := h.planSvc.ResolveUserPlan(&current)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		items = append(items, userSummary{
			User:     &current,
			Plan:     plan,
			Features: features,
		})
	}

	c.JSON(http.StatusOK, gin.H{"users": items})
}

func (h *Handler) AdminListPlans(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if err := h.userSvc.RequireAdmin(user); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	plans, err := h.planSvc.ListPlans()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"plans": plans})
}

func (h *Handler) AdminAssignPlan(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if err := h.userSvc.RequireAdmin(user); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	userID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	var req struct {
		PlanCode  string  `json:"plan_code"`
		ExpiresAt *string `json:"expires_at"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil && strings.TrimSpace(*req.ExpiresAt) != "" {
		value, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "expires_at must be RFC3339"})
			return
		}
		expiresAt = &value
	}

	if err := h.planSvc.AssignPlan(uint(userID), strings.TrimSpace(req.PlanCode), expiresAt); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) AdminUploadAppRelease(c *gin.Context) {
	version := strings.TrimSpace(c.PostForm("version"))
	notes := strings.TrimSpace(c.PostForm("notes"))
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	manifest, err := h.appReleaseSvc.Upload(version, notes, file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"manifest": manifest})
}
