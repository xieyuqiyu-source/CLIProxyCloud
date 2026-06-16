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
	paymentSvc    *services.PaymentService
	agentTaskSvc  *services.AgentTaskService
}

func New(
	authSvc *services.AuthService,
	userSvc *services.UserService,
	planSvc *services.PlanService,
	deviceSvc *services.DeviceService,
	authFileSvc *services.AuthFileService,
	appReleaseSvc *services.AppReleaseService,
	paymentSvc *services.PaymentService,
	agentTaskSvc *services.AgentTaskService,
) *Handler {
	return &Handler{
		authSvc:       authSvc,
		userSvc:       userSvc,
		planSvc:       planSvc,
		deviceSvc:     deviceSvc,
		authFileSvc:   authFileSvc,
		appReleaseSvc: appReleaseSvc,
		paymentSvc:    paymentSvc,
		agentTaskSvc:  agentTaskSvc,
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
	challenge, err := h.authSvc.BeginRegister(strings.TrimSpace(strings.ToLower(req.Email)), req.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	payload := gin.H{
		"status":       "verification_required",
		"challenge_id": challenge.ChallengeID,
		"masked_email": challenge.MaskedEmail,
		"expires_at":   challenge.ExpiresAt,
	}
	if challenge.DebugCode != "" {
		payload["debug_code"] = challenge.DebugCode
	}
	c.JSON(http.StatusAccepted, payload)
}

func (h *Handler) VerifyRegister(c *gin.Context) {
	var req struct {
		Email       string `json:"email"`
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	user, err := h.authSvc.VerifyRegister(strings.TrimSpace(strings.ToLower(req.Email)), req.ChallengeID, req.Code)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "ok", "user": user})
}

func (h *Handler) Login(c *gin.Context) {
	var req struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DeviceID    string `json:"device_id"`
		DeviceName  string `json:"device_name"`
		Platform    string `json:"platform"`
		TrustDevice bool   `json:"trust_device"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	login, challenge, err := h.authSvc.BeginPasswordLogin(normalizeLoginIdentifier(req.Email), req.Password, req.DeviceID, req.DeviceName, req.Platform, req.TrustDevice)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	if challenge != nil {
		payload := gin.H{
			"status":       "verification_required",
			"challenge_id": challenge.ChallengeID,
			"masked_email": challenge.MaskedEmail,
			"expires_at":   challenge.ExpiresAt,
		}
		if challenge.DebugCode != "" {
			payload["debug_code"] = challenge.DebugCode
		}
		c.JSON(http.StatusAccepted, payload)
		return
	}
	h.writeLoginSuccess(c, login)
}

func (h *Handler) VerifyLogin(c *gin.Context) {
	var req struct {
		Email               string `json:"email"`
		ChallengeID         string `json:"challenge_id"`
		Code                string `json:"code"`
		TrustDevice         bool   `json:"trust_device"`
		ForceLogoutExisting bool   `json:"force_logout_existing"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	login, conflict, err := h.authSvc.VerifyLoginChallenge(
		normalizeLoginIdentifier(req.Email),
		req.ChallengeID,
		req.Code,
		req.TrustDevice,
		req.ForceLogoutExisting,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if conflict != nil {
		c.JSON(http.StatusConflict, gin.H{
			"status":        "conflict",
			"active_device": conflict,
			"error":         "device already active elsewhere",
		})
		return
	}
	h.writeLoginSuccess(c, login)
}

func (h *Handler) TrustedDeviceLogin(c *gin.Context) {
	var req struct {
		Email        string `json:"email"`
		DeviceID     string `json:"device_id"`
		DeviceName   string `json:"device_name"`
		Platform     string `json:"platform"`
		TrustedToken string `json:"trusted_token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	login, err := h.authSvc.LoginTrustedDevice(normalizeLoginIdentifier(req.Email), req.DeviceID, req.TrustedToken, req.DeviceName, req.Platform)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	h.writeLoginSuccess(c, login)
}

func normalizeLoginIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "@") {
		return strings.ToLower(value)
	}
	return value
}

func (h *Handler) Me(c *gin.Context) {
	user := middleware.CurrentUser(c)
	plan, features, err := h.planSvc.ResolveUserPlan(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var expiresAt *time.Time
	if user.Role != models.UserRoleAdmin {
		if sub, _, err := h.planSvc.GetActiveSubscription(user.ID); err == nil && sub != nil {
			expiresAt = sub.ExpiresAt
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"user":      user,
		"plan":      plan,
		"features":  features,
		"expiresAt": expiresAt,
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

func (h *Handler) Logout(c *gin.Context) {
	user := middleware.CurrentUser(c)
	claims := middleware.CurrentClaims(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}
	deviceID := ""
	if claims != nil {
		deviceID = claims.DeviceID
	}
	if err := h.authSvc.Logout(user.ID, deviceID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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

func (h *Handler) writeLoginSuccess(c *gin.Context, login *services.LoginSuccess) {
	if login == nil || login.User == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid login state"})
		return
	}
	plan, features, err := h.planSvc.ResolveUserPlan(login.User)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var expiresAt *time.Time
	if login.User.Role != models.UserRoleAdmin {
		if sub, _, err := h.planSvc.GetActiveSubscription(login.User.ID); err == nil && sub != nil {
			expiresAt = sub.ExpiresAt
		}
	}
	response := gin.H{
		"status":    "ok",
		"token":     login.Token,
		"user":      login.User,
		"plan":      plan,
		"features":  features,
		"expiresAt": expiresAt,
		"device":    login.Device,
	}
	if login.TrustedToken != "" {
		response["trusted_token"] = login.TrustedToken
		response["trusted_until"] = login.TrustedUntil
	}
	c.JSON(http.StatusOK, response)
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

func (h *Handler) ListPaymentProducts(c *gin.Context) {
	products, err := h.paymentSvc.ListEnabledProducts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"products": products})
}

func (h *Handler) QuotePaymentOrder(c *gin.Context) {
	user := middleware.CurrentUser(c)
	var req struct {
		ProductCode   string `json:"product_code"`
		BillingMonths int    `json:"billing_months"`
		PurchaseMode  string `json:"purchase_mode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	product, quote, err := h.paymentSvc.QuoteOrder(user.ID, req.ProductCode, req.BillingMonths, models.PaymentPurchaseMode(strings.TrimSpace(strings.ToLower(req.PurchaseMode))))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"product": product, "quote": quote})
}

func (h *Handler) CreatePaymentOrder(c *gin.Context) {
	user := middleware.CurrentUser(c)
	var req struct {
		ProductCode   string `json:"product_code"`
		Provider      string `json:"provider"`
		BillingMonths int    `json:"billing_months"`
		PurchaseMode  string `json:"purchase_mode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	order, product, checkout, err := h.paymentSvc.CreateOrder(
		c.Request.Context(),
		user.ID,
		req.ProductCode,
		models.PaymentProvider(strings.TrimSpace(strings.ToLower(req.Provider))),
		req.BillingMonths,
		models.PaymentPurchaseMode(strings.TrimSpace(strings.ToLower(req.PurchaseMode))),
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"order":    order,
		"product":  product,
		"checkout": checkout,
	})
}

func (h *Handler) GetPaymentOrder(c *gin.Context) {
	user := middleware.CurrentUser(c)
	orderNo := strings.TrimSpace(c.Param("orderNo"))
	order, err := h.paymentSvc.FindOrderByNo(orderNo)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "payment order not found"})
		return
	}
	if user.Role != models.UserRoleAdmin && order.UserID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "payment order access denied"})
		return
	}
	if refreshed, err := h.paymentSvc.RefreshOrderStatus(c.Request.Context(), order); err == nil {
		order = refreshed
	}
	c.JSON(http.StatusOK, gin.H{"order": order})
}

func (h *Handler) CancelPaymentOrder(c *gin.Context) {
	user := middleware.CurrentUser(c)
	orderNo := strings.TrimSpace(c.Param("orderNo"))
	order, err := h.paymentSvc.FindOrderByNo(orderNo)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "payment order not found"})
		return
	}
	if user.Role != models.UserRoleAdmin && order.UserID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "payment order access denied"})
		return
	}
	order, err = h.paymentSvc.CancelPendingOrder(order)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"order": order})
}

func (h *Handler) XunhuPaymentNotify(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	values := map[string]string{}
	for key, items := range c.Request.PostForm {
		if len(items) > 0 {
			values[key] = items[0]
		}
	}
	if _, err := h.paymentSvc.HandleXunhuNotify(values); err != nil {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	c.String(http.StatusOK, "success")
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
	if file.DistributionMode == models.AuthDistributionQuotaCard {
		c.JSON(http.StatusForbidden, gin.H{"error": "encrypted quota card credentials can only be used through the cloud proxy"})
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
	options := parseSharedAuthUploadOptions(c)
	result, err := h.authFileSvc.UploadMany(models.AuthOwnerTypeShared, nil, models.AuthSourceShared, &planCode, file, options)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var firstFile any
	if len(result.Files) > 0 {
		firstFile = result.Files[0]
	}
	c.JSON(http.StatusCreated, gin.H{
		"file":     firstFile,
		"files":    result.Files,
		"uploaded": len(result.Files),
		"skipped":  result.Skipped,
	})
}

func parseSharedAuthUploadOptions(c *gin.Context) services.AuthFileUploadOptions {
	mode := models.AuthDistributionMode(strings.TrimSpace(c.PostForm("distribution_mode")))
	if mode == "" {
		mode = models.AuthDistributionMode(strings.TrimSpace(c.PostForm("distributionMode")))
	}
	options := services.AuthFileUploadOptions{DistributionMode: mode}
	if quotaRaw := strings.TrimSpace(c.PostForm("quota_limit")); quotaRaw == "" {
		quotaRaw = strings.TrimSpace(c.PostForm("quotaLimit"))
		if parsed, err := strconv.ParseInt(quotaRaw, 10, 64); err == nil {
			options.QuotaLimit = parsed
		}
	} else if parsed, err := strconv.ParseInt(quotaRaw, 10, 64); err == nil {
		options.QuotaLimit = parsed
	}
	if resetRaw := strings.TrimSpace(c.PostForm("quota_reset_at")); resetRaw == "" {
		resetRaw = strings.TrimSpace(c.PostForm("quotaResetAt"))
		if parsed, err := time.Parse(time.RFC3339, resetRaw); err == nil {
			options.QuotaResetAt = &parsed
		}
	} else if parsed, err := time.Parse(time.RFC3339, resetRaw); err == nil {
		options.QuotaResetAt = &parsed
	}
	return options
}

func (h *Handler) ConsumeSharedQuotaCard(c *gin.Context) {
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
	var req struct {
		Units int64 `json:"units"`
	}
	if c.Request.Body != nil {
		_ = c.ShouldBindJSON(&req)
	}
	file, err := h.authFileSvc.ConsumeQuotaCard(uint(authFileID), req.Units)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "exceeded") {
			status = http.StatusPaymentRequired
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"file": file})
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
		User      *models.User             `json:"user"`
		Plan      *models.Plan             `json:"plan"`
		Features  services.FeatureFlags    `json:"features"`
		ExpiresAt *time.Time               `json:"expiresAt"`
		Sub       *models.UserSubscription `json:"subscription,omitempty"`
	}

	items := make([]userSummary, 0, len(users))
	for idx := range users {
		current := users[idx]
		plan, features, err := h.planSvc.ResolveUserPlan(&current)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		sub, _, subErr := h.planSvc.GetActiveSubscription(current.ID)
		if subErr != nil {
			sub = nil
		}
		var expiresAt *time.Time
		if sub != nil {
			expiresAt = sub.ExpiresAt
		}
		items = append(items, userSummary{
			User:      &current,
			Plan:      plan,
			Features:  features,
			ExpiresAt: expiresAt,
			Sub:       sub,
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

func (h *Handler) AdminListPaymentProducts(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if err := h.userSvc.RequireAdmin(user); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	products, err := h.paymentSvc.ListAdminProducts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"products": products})
}

func (h *Handler) AdminCreatePaymentProduct(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if err := h.userSvc.RequireAdmin(user); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	var req struct {
		ProductCode  string `json:"product_code"`
		Name         string `json:"name"`
		DisplayName  string `json:"display_name"`
		PlanCode     string `json:"plan_code"`
		PriceAmount  int64  `json:"price_amount"`
		Currency     string `json:"currency"`
		DurationDays int    `json:"duration_days"`
		Status       string `json:"status"`
		SortOrder    int    `json:"sort_order"`
		Description  string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	product, err := h.paymentSvc.UpsertProduct(req.ProductCode, services.PaymentProductInput{
		ProductCode:  req.ProductCode,
		Name:         req.Name,
		DisplayName:  req.DisplayName,
		PlanCode:     req.PlanCode,
		PriceAmount:  req.PriceAmount,
		Currency:     req.Currency,
		DurationDays: req.DurationDays,
		Status:       models.PaymentProductStatus(strings.TrimSpace(req.Status)),
		SortOrder:    req.SortOrder,
		Description:  req.Description,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"product": product})
}

func (h *Handler) AdminUpdatePaymentProduct(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if err := h.userSvc.RequireAdmin(user); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	productID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product id"})
		return
	}

	current, err := h.paymentSvc.FindProductByID(uint(productID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "payment product not found"})
		return
	}

	var req struct {
		ProductCode  string  `json:"product_code"`
		Name         *string `json:"name"`
		DisplayName  *string `json:"display_name"`
		PlanCode     *string `json:"plan_code"`
		PriceAmount  *int64  `json:"price_amount"`
		Currency     *string `json:"currency"`
		DurationDays *int    `json:"duration_days"`
		Status       *string `json:"status"`
		SortOrder    *int    `json:"sort_order"`
		Description  *string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	input := services.PaymentProductInput{
		ProductCode:  current.ProductCode,
		Name:         current.Name,
		DisplayName:  current.DisplayName,
		PlanCode:     current.PlanCode,
		PriceAmount:  current.PriceAmount,
		Currency:     current.Currency,
		DurationDays: current.DurationDays,
		Status:       current.Status,
		SortOrder:    current.SortOrder,
		Description:  current.Description,
	}
	if trimmed := strings.TrimSpace(req.ProductCode); trimmed != "" {
		input.ProductCode = trimmed
	}
	if req.Name != nil {
		input.Name = *req.Name
	}
	if req.DisplayName != nil {
		input.DisplayName = *req.DisplayName
	}
	if req.PlanCode != nil {
		input.PlanCode = *req.PlanCode
	}
	if req.PriceAmount != nil {
		input.PriceAmount = *req.PriceAmount
	}
	if req.Currency != nil {
		input.Currency = *req.Currency
	}
	if req.DurationDays != nil {
		input.DurationDays = *req.DurationDays
	}
	if req.Status != nil {
		input.Status = models.PaymentProductStatus(strings.TrimSpace(*req.Status))
	}
	if req.SortOrder != nil {
		input.SortOrder = *req.SortOrder
	}
	if req.Description != nil {
		input.Description = *req.Description
	}

	product, err := h.paymentSvc.UpsertProduct(input.ProductCode, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"product": product})
}

func (h *Handler) AdminListPaymentOrders(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if err := h.userSvc.RequireAdmin(user); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	limit := 50
	if value := strings.TrimSpace(c.Query("limit")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			limit = parsed
		}
	}
	status := strings.TrimSpace(c.Query("status"))
	query := strings.TrimSpace(c.Query("query"))

	orders, err := h.paymentSvc.ListAdminOrders(limit, status, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders})
}

func (h *Handler) AdminRegrantPaymentOrder(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if err := h.userSvc.RequireAdmin(user); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	orderNo := strings.TrimSpace(c.Param("orderNo"))
	if orderNo == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order no"})
		return
	}

	order, err := h.paymentSvc.RegrantOrder(orderNo)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "order": order})
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

func (h *Handler) AdminUpdateUserRole(c *gin.Context) {
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
		Role string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	role := models.UserRole(strings.ToLower(strings.TrimSpace(req.Role)))
	if role != models.UserRoleUser && role != models.UserRoleAdmin {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role must be user or admin"})
		return
	}

	if err := h.userSvc.UpdateRole(uint(userID), role); err != nil {
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
