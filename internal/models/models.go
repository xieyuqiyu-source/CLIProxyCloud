package models

import (
	"time"

	"gorm.io/datatypes"
)

type UserRole string

const (
	UserRoleUser  UserRole = "user"
	UserRoleAdmin UserRole = "admin"
)

type SubscriptionStatus string

const (
	SubscriptionStatusActive   SubscriptionStatus = "active"
	SubscriptionStatusExpired  SubscriptionStatus = "expired"
	SubscriptionStatusCanceled SubscriptionStatus = "canceled"
)

type AuthOwnerType string

const (
	AuthOwnerTypeUser   AuthOwnerType = "user"
	AuthOwnerTypeShared AuthOwnerType = "shared"
)

type AuthSourceType string

const (
	AuthSourcePersonal AuthSourceType = "personal"
	AuthSourceShared   AuthSourceType = "shared"
)

type DeviceStatus string

const (
	DeviceStatusActive DeviceStatus = "active"
)

type SyncAction string

const (
	SyncActionUpload         SyncAction = "upload"
	SyncActionDownload       SyncAction = "download"
	SyncActionDelete         SyncAction = "delete"
	SyncActionAssignPlan     SyncAction = "assign_plan"
	SyncActionRegisterDevice SyncAction = "register_device"
)

type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Email        string    `gorm:"size:190;uniqueIndex;not null" json:"email"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	Role         UserRole  `gorm:"size:32;not null;default:user" json:"role"`
	Status       string    `gorm:"size:32;not null;default:active" json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type Plan struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	PlanCode     string         `gorm:"size:64;uniqueIndex;not null" json:"planCode"`
	Name         string         `gorm:"size:128;not null" json:"name"`
	Description  string         `gorm:"type:text" json:"description"`
	FeatureFlags datatypes.JSON `gorm:"type:json" json:"featureFlags"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

type UserSubscription struct {
	ID        uint               `gorm:"primaryKey" json:"id"`
	UserID    uint               `gorm:"index;not null" json:"userId"`
	PlanID    uint               `gorm:"index;not null" json:"planId"`
	Status    SubscriptionStatus `gorm:"size:32;not null;default:active" json:"status"`
	StartsAt  time.Time          `gorm:"not null" json:"startsAt"`
	ExpiresAt *time.Time         `json:"expiresAt"`
	CreatedAt time.Time          `json:"createdAt"`
	UpdatedAt time.Time          `json:"updatedAt"`
}

type Device struct {
	ID         uint         `gorm:"primaryKey" json:"id"`
	UserID     uint         `gorm:"index;not null" json:"userId"`
	DeviceID   string       `gorm:"size:191;not null;uniqueIndex:idx_user_device" json:"deviceId"`
	DeviceName string       `gorm:"size:191;not null" json:"deviceName"`
	Platform   string       `gorm:"size:64;not null" json:"platform"`
	Status     DeviceStatus `gorm:"size:32;not null;default:active" json:"status"`
	LastSeenAt time.Time    `gorm:"not null" json:"lastSeenAt"`
	CreatedAt  time.Time    `json:"createdAt"`
	UpdatedAt  time.Time    `json:"updatedAt"`
}

type AuthFile struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	OwnerType    AuthOwnerType  `gorm:"size:32;index;not null" json:"ownerType"`
	OwnerUserID  *uint          `gorm:"index" json:"ownerUserId"`
	Provider     string         `gorm:"size:64;not null" json:"provider"`
	FileName     string         `gorm:"size:255;not null" json:"fileName"`
	StoragePath  string         `gorm:"size:512;not null" json:"storagePath"`
	FileHash     string         `gorm:"size:128;not null" json:"fileHash"`
	Encrypted    bool           `gorm:"not null;default:true" json:"encrypted"`
	Status       string         `gorm:"size:32;not null;default:active" json:"status"`
	SourceType   AuthSourceType `gorm:"size:32;not null" json:"sourceType"`
	PlanRequired *string        `gorm:"size:64" json:"planRequired"`
	DisplayName  string         `gorm:"size:255;not null" json:"displayName"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

type AuthFileVersion struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	AuthFileID  uint      `gorm:"index;not null" json:"authFileId"`
	Version     int       `gorm:"not null" json:"version"`
	StoragePath string    `gorm:"size:512;not null" json:"storagePath"`
	FileHash    string    `gorm:"size:128;not null" json:"fileHash"`
	CreatedAt   time.Time `json:"createdAt"`
}

type SyncLog struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	UserID     *uint      `gorm:"index" json:"userId"`
	DeviceID   *uint      `gorm:"index" json:"deviceId"`
	Action     SyncAction `gorm:"size:64;index;not null" json:"action"`
	TargetType string     `gorm:"size:64;not null" json:"targetType"`
	TargetID   string     `gorm:"size:128;not null" json:"targetId"`
	Result     string     `gorm:"size:64;not null" json:"result"`
	Message    string     `gorm:"type:text" json:"message"`
	CreatedAt  time.Time  `json:"createdAt"`
}
