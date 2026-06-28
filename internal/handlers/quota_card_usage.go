package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type quotaCardAccessRequest struct {
	QuotaToken string `json:"quotaToken"`
	Units      int64  `json:"units"`
}

func (h *Handler) CheckQuotaCard(c *gin.Context) {
	authFileID, req, ok := parseQuotaCardAccess(c)
	if !ok {
		return
	}
	file, err := h.authFileSvc.FindSharedQuotaCardWithToken(authFileID, req.QuotaToken)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	units := req.Units
	if units <= 0 {
		units = 1
	}
	remaining := int64(0)
	if file.QuotaLimit > 0 {
		remaining = file.QuotaLimit - file.QuotaUsed
		if remaining < 0 {
			remaining = 0
		}
	}
	allowed := file.QuotaLimit <= 0 || file.QuotaUsed+units <= file.QuotaLimit
	c.JSON(http.StatusOK, gin.H{
		"allowed":    allowed,
		"remaining":  remaining,
		"quota_unit": "usd_micro",
		"file":       file,
	})
}

func (h *Handler) ReportQuotaCardUsage(c *gin.Context) {
	authFileID, req, ok := parseQuotaCardAccess(c)
	if !ok {
		return
	}
	file, err := h.authFileSvc.ConsumeQuotaCardWithToken(authFileID, req.QuotaToken, req.Units)
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

func parseQuotaCardAccess(c *gin.Context) (uint, quotaCardAccessRequest, bool) {
	authFileID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, quotaCardAccessRequest{}, false
	}
	var req quotaCardAccessRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return 0, quotaCardAccessRequest{}, false
	}
	token := strings.TrimSpace(req.QuotaToken)
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quota token is required"})
		return 0, quotaCardAccessRequest{}, false
	}
	req.QuotaToken = token
	return uint(authFileID), req, true
}
