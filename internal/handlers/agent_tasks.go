package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/middleware"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/models"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/services"
)

func (h *Handler) AdminCreateAgentTask(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if err := h.userSvc.RequireAdmin(user); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	var req struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	taskType := models.AgentTaskType(strings.TrimSpace(req.Type))
	if taskType != models.AgentTaskTypeCheckSharedPool {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported agent task type"})
		return
	}

	task, err := h.agentTaskSvc.CreateCheckSharedPoolTask(user.ID, services.MarshalAgentJSON(req.Payload))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	status, err := h.agentTaskSvc.Status(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"task": task, "agent": status})
}

func (h *Handler) AdminListAgentTasks(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if err := h.userSvc.RequireAdmin(user); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	limit := 20
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	tasks, err := h.agentTaskSvc.ListTasks(user.ID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	status, err := h.agentTaskSvc.Status(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": tasks, "agent": status})
}

func (h *Handler) AdminAgentStatus(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if err := h.userSvc.RequireAdmin(user); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	status, err := h.agentTaskSvc.Status(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"agent": status})
}

func (h *Handler) AgentPollTask(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if err := h.userSvc.RequireAdmin(user); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	task, status, err := h.agentTaskSvc.Poll(user.ID, c.Query("device_id"), c.Query("device_name"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": task, "agent": status})
}

func (h *Handler) AgentSubmitTaskResult(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if err := h.userSvc.RequireAdmin(user); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	taskID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task id"})
		return
	}

	var req struct {
		Status string         `json:"status"`
		Result map[string]any `json:"result"`
		Error  string         `json:"error"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	status := models.AgentTaskStatus(strings.TrimSpace(req.Status))
	if status != models.AgentTaskStatusCompleted && status != models.AgentTaskStatusFailed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status must be completed or failed"})
		return
	}
	task, err := h.agentTaskSvc.SubmitResult(user.ID, uint(taskID), status, services.MarshalAgentJSON(req.Result), req.Error)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": task})
}
