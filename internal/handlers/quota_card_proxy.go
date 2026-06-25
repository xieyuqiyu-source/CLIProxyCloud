package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/middleware"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/models"
)

const quotaCardAPICallTimeout = 60 * time.Second

type quotaCardAPICallRequest struct {
	Method       string            `json:"method"`
	URL          string            `json:"url"`
	Header       map[string]string `json:"header"`
	Data         string            `json:"data"`
	ConsumeUnits int64             `json:"consume_units"`
}

type quotaCardAPICallResponse struct {
	StatusCode int                 `json:"status_code"`
	Header     map[string][]string `json:"header"`
	Body       string              `json:"body"`
	File       *models.AuthFile    `json:"file"`
}

func (h *Handler) SharedQuotaCardAPICall(c *gin.Context) {
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

	var req quotaCardAPICallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	if !isAllowedQuotaCardMethod(method) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported method"})
		return
	}
	target, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil || target == nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid url"})
		return
	}

	file, err := h.authFileSvc.FindShared(uint(authFileID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "shared auth file not found"})
		return
	}
	if file.DistributionMode != models.AuthDistributionQuotaCard {
		c.JSON(http.StatusBadRequest, gin.H{"error": "shared auth file is not a quota card"})
		return
	}

	content, err := h.authFileSvc.ReadContent(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	placeholders := quotaCardPlaceholdersFromAuthJSON(content)
	headers := replaceQuotaCardHeaderPlaceholders(req.Header, placeholders)

	consumedFile, err := h.authFileSvc.ConsumeQuotaCard(uint(authFileID), req.ConsumeUnits)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "exceeded") {
			status = http.StatusPaymentRequired
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	httpReq, err := http.NewRequest(method, target.String(), strings.NewReader(req.Data))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	for key, value := range headers {
		if strings.EqualFold(key, "Host") {
			httpReq.Host = value
			continue
		}
		httpReq.Header.Set(key, value)
	}

	client := &http.Client{Timeout: quotaCardAPICallTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "file": consumedFile})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "file": consumedFile})
		return
	}

	c.JSON(http.StatusOK, quotaCardAPICallResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       string(body),
		File:       consumedFile,
	})
}

func isAllowedQuotaCardMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func replaceQuotaCardHeaderPlaceholders(headers map[string]string, placeholders map[string]string) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}
	result := make(map[string]string, len(headers))
	for key, value := range headers {
		replaced := value
		for placeholder, replacement := range placeholders {
			replaced = strings.ReplaceAll(replaced, placeholder, replacement)
		}
		result[key] = replaced
	}
	return result
}

func quotaCardPlaceholdersFromAuthJSON(content []byte) map[string]string {
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return map[string]string{"$TOKEN$": ""}
	}

	fields := map[string]string{}
	for _, key := range []string{"accessToken", "access_token", "api_key", "token", "id_token", "cookie"} {
		if value := stringFromAny(payload[key]); value != "" {
			fields[key] = value
		}
	}
	for _, key := range []string{"metadata", "attributes", "token"} {
		if nested, ok := payload[key].(map[string]any); ok {
			for _, nestedKey := range []string{"accessToken", "access_token", "api_key", "token", "id_token", "cookie", "account_id", "chatgpt_account_id"} {
				if value := stringFromAny(nested[nestedKey]); value != "" {
					fields[nestedKey] = value
				}
			}
		}
	}

	token := firstNonEmptyField(fields, "accessToken", "access_token", "api_key", "token", "id_token", "cookie")
	accountID := firstNonEmptyField(fields, "account_id", "chatgpt_account_id")
	return map[string]string{
		"$TOKEN$":              token,
		"$ACCOUNT_ID$":         accountID,
		"$CHATGPT_ACCOUNT_ID$": accountID,
	}
}

func firstNonEmptyField(fields map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(fields[key]); value != "" {
			return value
		}
	}
	return ""
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	default:
		return ""
	}
}
