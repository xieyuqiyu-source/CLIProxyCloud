package services

import (
	"fmt"
	"time"

	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/models"
	"gorm.io/gorm"
)

type DeviceService struct {
	db *gorm.DB
}

func NewDeviceService(db *gorm.DB) *DeviceService {
	return &DeviceService{db: db}
}

func (s *DeviceService) RegisterOrTouch(user *models.User, features FeatureFlags, deviceID string, deviceName string, platform string) (*models.Device, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("device_id is required")
	}

	var device models.Device
	err := s.db.Where("device_id = ?", deviceID).First(&device).Error
	if err == nil {
		if device.UserID != user.ID && user.Role != models.UserRoleAdmin {
			return nil, fmt.Errorf("device already belongs to another user")
		}
		device.DeviceName = deviceName
		device.Platform = platform
		device.LastSeenAt = time.Now()
		if err := s.db.Save(&device).Error; err != nil {
			return nil, err
		}
		return &device, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}

	if user.Role != models.UserRoleAdmin {
		var count int64
		if err := s.db.Model(&models.Device{}).Where("user_id = ? AND status = ?", user.ID, models.DeviceStatusActive).Count(&count).Error; err != nil {
			return nil, err
		}
		if int(count) >= features.MaxDevices {
			return nil, fmt.Errorf("device limit exceeded")
		}
	}

	device = models.Device{
		UserID:     user.ID,
		DeviceID:   deviceID,
		DeviceName: deviceName,
		Platform:   platform,
		Status:     models.DeviceStatusActive,
		LastSeenAt: time.Now(),
	}
	if err := s.db.Create(&device).Error; err != nil {
		return nil, err
	}
	return &device, nil
}

func (s *DeviceService) FindByDeviceID(deviceID string) (*models.Device, error) {
	var device models.Device
	if err := s.db.Where("device_id = ?", deviceID).First(&device).Error; err != nil {
		return nil, err
	}
	return &device, nil
}
