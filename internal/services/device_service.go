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
	_ = features

	var device models.Device
	err := s.db.Where("user_id = ? AND device_id = ?", user.ID, deviceID).First(&device).Error
	if err == nil {
		device.UserID = user.ID
		device.Status = models.DeviceStatusActive
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
