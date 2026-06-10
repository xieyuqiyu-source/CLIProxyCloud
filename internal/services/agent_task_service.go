package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const AgentOfflineAfter = time.Minute

type AgentTaskService struct {
	db *gorm.DB
}

type AgentStatus struct {
	Heartbeat           *models.AgentHeartbeat `json:"heartbeat"`
	Online              bool                   `json:"online"`
	OfflineAfterSeconds int                    `json:"offlineAfterSeconds"`
}

func NewAgentTaskService(db *gorm.DB) *AgentTaskService {
	return &AgentTaskService{db: db}
}

func (s *AgentTaskService) Heartbeat(userID uint, deviceID string, deviceName string) (*models.AgentHeartbeat, error) {
	now := time.Now()
	heartbeat := models.AgentHeartbeat{
		UserID:     userID,
		DeviceID:   strings.TrimSpace(deviceID),
		DeviceName: strings.TrimSpace(deviceName),
		Status:     "online",
		LastPollAt: now,
	}
	if heartbeat.DeviceName == "" {
		heartbeat.DeviceName = "CPSwitch"
	}

	err := s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"device_id":    heartbeat.DeviceID,
			"device_name":  heartbeat.DeviceName,
			"status":       heartbeat.Status,
			"last_poll_at": heartbeat.LastPollAt,
			"updated_at":   now,
		}),
	}).Create(&heartbeat).Error
	if err != nil {
		return nil, err
	}
	return s.GetHeartbeat(userID)
}

func (s *AgentTaskService) GetHeartbeat(userID uint) (*models.AgentHeartbeat, error) {
	var heartbeat models.AgentHeartbeat
	if err := s.db.Where("user_id = ?", userID).First(&heartbeat).Error; err != nil {
		return nil, err
	}
	return &heartbeat, nil
}

func (s *AgentTaskService) Status(userID uint) (AgentStatus, error) {
	heartbeat, err := s.GetHeartbeat(userID)
	if err == gorm.ErrRecordNotFound {
		return AgentStatus{OfflineAfterSeconds: int(AgentOfflineAfter.Seconds())}, nil
	}
	if err != nil {
		return AgentStatus{}, err
	}
	return AgentStatus{
		Heartbeat:           heartbeat,
		Online:              time.Since(heartbeat.LastPollAt) <= AgentOfflineAfter,
		OfflineAfterSeconds: int(AgentOfflineAfter.Seconds()),
	}, nil
}

func (s *AgentTaskService) CreateCheckSharedPoolTask(userID uint, payload datatypes.JSON) (*models.AgentTask, error) {
	now := time.Now()
	if len(payload) == 0 {
		payload = datatypes.JSON([]byte("{}"))
	}

	var existing models.AgentTask
	err := s.db.
		Where("user_id = ? AND type = ? AND status IN ? AND expires_at > ?", userID, models.AgentTaskTypeCheckSharedPool, []models.AgentTaskStatus{
			models.AgentTaskStatusPending,
			models.AgentTaskStatusRunning,
		}, now).
		Order("id desc").
		First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}

	task := &models.AgentTask{
		UserID:    userID,
		Type:      models.AgentTaskTypeCheckSharedPool,
		Status:    models.AgentTaskStatusPending,
		Payload:   payload,
		ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := s.db.Create(task).Error; err != nil {
		return nil, err
	}
	return task, nil
}

func (s *AgentTaskService) Poll(userID uint, deviceID string, deviceName string) (*models.AgentTask, AgentStatus, error) {
	heartbeat, err := s.Heartbeat(userID, deviceID, deviceName)
	if err != nil {
		return nil, AgentStatus{}, err
	}

	now := time.Now()
	if err := s.db.Model(&models.AgentTask{}).
		Where("user_id = ? AND status IN ? AND expires_at <= ?", userID, []models.AgentTaskStatus{
			models.AgentTaskStatusPending,
			models.AgentTaskStatusRunning,
		}, now).
		Updates(map[string]interface{}{
			"status":       models.AgentTaskStatusExpired,
			"completed_at": now,
			"updated_at":   now,
		}).Error; err != nil {
		return nil, AgentStatus{}, err
	}

	var task models.AgentTask
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND status = ? AND expires_at > ?", userID, models.AgentTaskStatusPending, now).
			Order("id asc").
			First(&task).Error; err != nil {
			return err
		}
		task.Status = models.AgentTaskStatusRunning
		task.ClaimedAt = &now
		return tx.Save(&task).Error
	})
	if err == gorm.ErrRecordNotFound {
		return nil, AgentStatus{
			Heartbeat:           heartbeat,
			Online:              true,
			OfflineAfterSeconds: int(AgentOfflineAfter.Seconds()),
		}, nil
	}
	if err != nil {
		return nil, AgentStatus{}, err
	}

	return &task, AgentStatus{
		Heartbeat:           heartbeat,
		Online:              true,
		OfflineAfterSeconds: int(AgentOfflineAfter.Seconds()),
	}, nil
}

func (s *AgentTaskService) SubmitResult(userID uint, taskID uint, status models.AgentTaskStatus, result datatypes.JSON, errorMessage string) (*models.AgentTask, error) {
	if status != models.AgentTaskStatusCompleted && status != models.AgentTaskStatusFailed {
		return nil, fmt.Errorf("status must be completed or failed")
	}
	if len(result) == 0 {
		result = datatypes.JSON([]byte("{}"))
	}

	now := time.Now()
	var task models.AgentTask
	if err := s.db.Where("id = ? AND user_id = ?", taskID, userID).First(&task).Error; err != nil {
		return nil, err
	}
	task.Status = status
	task.Result = result
	task.ErrorMessage = strings.TrimSpace(errorMessage)
	task.CompletedAt = &now
	if err := s.db.Save(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *AgentTaskService) ListTasks(userID uint, limit int) ([]models.AgentTask, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var tasks []models.AgentTask
	err := s.db.Where("user_id = ?", userID).Order("id desc").Limit(limit).Find(&tasks).Error
	return tasks, err
}

func MarshalAgentJSON(value any) datatypes.JSON {
	if value == nil {
		return datatypes.JSON([]byte("{}"))
	}
	bytes, err := json.Marshal(value)
	if err != nil {
		return datatypes.JSON([]byte("{}"))
	}
	return datatypes.JSON(bytes)
}
