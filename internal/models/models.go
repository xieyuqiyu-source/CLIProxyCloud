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

type AuthDistributionMode string

const (
	AuthDistributionPlain     AuthDistributionMode = "plain"
	AuthDistributionQuotaCard AuthDistributionMode = "quota_card"
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

type PaymentProvider string

const (
	PaymentProviderXunhu PaymentProvider = "xunhu"
)

type PaymentProductStatus string

const (
	PaymentProductStatusActive   PaymentProductStatus = "active"
	PaymentProductStatusDisabled PaymentProductStatus = "disabled"
)

type PaymentOrderStatus string

const (
	PaymentOrderStatusPending  PaymentOrderStatus = "pending"
	PaymentOrderStatusPaid     PaymentOrderStatus = "paid"
	PaymentOrderStatusClosed   PaymentOrderStatus = "closed"
	PaymentOrderStatusFailed   PaymentOrderStatus = "failed"
	PaymentOrderStatusRefunded PaymentOrderStatus = "refunded"
)

type PaymentPurchaseMode string

const (
	PaymentPurchaseModeStandard            PaymentPurchaseMode = "standard"
	PaymentPurchaseModeUpgradeDiffAll      PaymentPurchaseMode = "upgrade_diff_all"
	PaymentPurchaseModeUpgradeReplaceMonth PaymentPurchaseMode = "upgrade_replace_month"
)

type AgentTaskType string

const (
	AgentTaskTypeCheckSharedPool AgentTaskType = "check_shared_pool"
)

type AgentTaskStatus string

const (
	AgentTaskStatusPending   AgentTaskStatus = "pending"
	AgentTaskStatusRunning   AgentTaskStatus = "running"
	AgentTaskStatusCompleted AgentTaskStatus = "completed"
	AgentTaskStatusFailed    AgentTaskStatus = "failed"
	AgentTaskStatusExpired   AgentTaskStatus = "expired"
)

type User struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	Email          string    `gorm:"size:190;uniqueIndex;not null" json:"email"`
	PasswordHash   string    `gorm:"size:255;not null" json:"-"`
	Role           UserRole  `gorm:"size:32;not null;default:user" json:"role"`
	Status         string    `gorm:"size:32;not null;default:active" json:"status"`
	ActiveDeviceID *string   `gorm:"size:191;index" json:"activeDeviceId"`
	SessionVersion uint64    `gorm:"not null;default:0" json:"sessionVersion"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
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
	ID               uint         `gorm:"primaryKey" json:"id"`
	UserID           uint         `gorm:"index;not null" json:"userId"`
	DeviceID         string       `gorm:"size:191;not null;uniqueIndex:idx_user_device" json:"deviceId"`
	DeviceName       string       `gorm:"size:191;not null" json:"deviceName"`
	Platform         string       `gorm:"size:64;not null" json:"platform"`
	Status           DeviceStatus `gorm:"size:32;not null;default:active" json:"status"`
	TrustedTokenHash *string      `gorm:"size:128" json:"-"`
	TrustedUntil     *time.Time   `json:"trustedUntil"`
	LastVerifiedAt   *time.Time   `json:"lastVerifiedAt"`
	LastLoginAt      *time.Time   `json:"lastLoginAt"`
	LastSeenAt       time.Time    `gorm:"not null" json:"lastSeenAt"`
	CreatedAt        time.Time    `json:"createdAt"`
	UpdatedAt        time.Time    `json:"updatedAt"`
}

type LoginVerification struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	ChallengeID string     `gorm:"size:64;uniqueIndex;not null" json:"challengeId"`
	UserID      uint       `gorm:"index;not null" json:"userId"`
	Email       string     `gorm:"size:190;index;not null" json:"email"`
	DeviceID    string     `gorm:"size:191;index;not null" json:"deviceId"`
	DeviceName  string     `gorm:"size:191;not null" json:"deviceName"`
	Platform    string     `gorm:"size:64;not null" json:"platform"`
	CodeHash    string     `gorm:"size:128;not null" json:"-"`
	ExpiresAt   time.Time  `gorm:"index;not null" json:"expiresAt"`
	ConsumedAt  *time.Time `json:"consumedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type RegistrationVerification struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	ChallengeID  string     `gorm:"size:64;uniqueIndex;not null" json:"challengeId"`
	Email        string     `gorm:"size:190;index;not null" json:"email"`
	PasswordHash string     `gorm:"size:255;not null" json:"-"`
	CodeHash     string     `gorm:"size:128;not null" json:"-"`
	ExpiresAt    time.Time  `gorm:"index;not null" json:"expiresAt"`
	ConsumedAt   *time.Time `json:"consumedAt"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type AuthFile struct {
	ID               uint                 `gorm:"primaryKey" json:"id"`
	OwnerType        AuthOwnerType        `gorm:"size:32;index;not null" json:"ownerType"`
	OwnerUserID      *uint                `gorm:"index" json:"ownerUserId"`
	Provider         string               `gorm:"size:64;not null" json:"provider"`
	FileName         string               `gorm:"size:255;not null" json:"fileName"`
	StoragePath      string               `gorm:"size:512;not null" json:"storagePath"`
	FileHash         string               `gorm:"size:128;not null" json:"fileHash"`
	Encrypted        bool                 `gorm:"not null;default:true" json:"encrypted"`
	Status           string               `gorm:"size:32;not null;default:active" json:"status"`
	SourceType       AuthSourceType       `gorm:"size:32;not null" json:"sourceType"`
	PlanRequired     *string              `gorm:"size:64" json:"planRequired"`
	DisplayName      string               `gorm:"size:255;not null" json:"displayName"`
	DistributionMode AuthDistributionMode `gorm:"size:32;not null;default:plain" json:"distributionMode"`
	QuotaLimit       int64                `gorm:"not null;default:0" json:"quotaLimit"`
	QuotaUsed        int64                `gorm:"not null;default:0" json:"quotaUsed"`
	QuotaResetAt     *time.Time           `json:"quotaResetAt"`
	CreatedAt        time.Time            `json:"createdAt"`
	UpdatedAt        time.Time            `json:"updatedAt"`
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

type PaymentProduct struct {
	ID           uint                 `gorm:"primaryKey" json:"id"`
	ProductCode  string               `gorm:"size:64;uniqueIndex;not null" json:"productCode"`
	Name         string               `gorm:"size:128;not null" json:"name"`
	DisplayName  string               `gorm:"size:128;not null" json:"displayName"`
	PlanCode     string               `gorm:"size:64;index;not null" json:"planCode"`
	PriceAmount  int64                `gorm:"not null" json:"priceAmount"`
	Currency     string               `gorm:"size:16;not null;default:CNY" json:"currency"`
	DurationDays int                  `gorm:"not null;default:30" json:"durationDays"`
	Status       PaymentProductStatus `gorm:"size:32;not null;default:active" json:"status"`
	SortOrder    int                  `gorm:"not null;default:0" json:"sortOrder"`
	Description  string               `gorm:"type:text" json:"description"`
	CreatedAt    time.Time            `json:"createdAt"`
	UpdatedAt    time.Time            `json:"updatedAt"`
}

type PaymentOrder struct {
	ID              uint                `gorm:"primaryKey" json:"id"`
	OrderNo         string              `gorm:"size:64;uniqueIndex;not null" json:"orderNo"`
	UserID          uint                `gorm:"index;not null" json:"userId"`
	ProductID       uint                `gorm:"index;not null" json:"productId"`
	ProductCode     string              `gorm:"size:64;index;not null;default:''" json:"productCode"`
	ProductName     string              `gorm:"size:128;not null;default:''" json:"productName"`
	ProductDisplay  string              `gorm:"size:128;not null;default:''" json:"productDisplayName"`
	ProductDesc     string              `gorm:"type:text" json:"productDescription"`
	PurchaseMode    PaymentPurchaseMode `gorm:"size:64;not null;default:standard" json:"purchaseMode"`
	BillingMonths   int                 `gorm:"not null;default:1" json:"billingMonths"`
	DurationDays    int                 `gorm:"not null;default:0" json:"durationDays"`
	UpgradeFromPlan string              `gorm:"size:64;not null;default:''" json:"upgradeFromPlan"`
	UpgradeMonths   int                 `gorm:"not null;default:0" json:"upgradeMonths"`
	UpgradeBaseAt   *time.Time          `json:"upgradeBaseAt"`
	PlanCode        string              `gorm:"size:64;index;not null" json:"planCode"`
	PaymentProvider PaymentProvider     `gorm:"size:32;index;not null" json:"paymentProvider"`
	Amount          int64               `gorm:"not null" json:"amount"`
	Currency        string              `gorm:"size:16;not null;default:CNY" json:"currency"`
	Status          PaymentOrderStatus  `gorm:"size:32;index;not null;default:pending" json:"status"`
	ProviderOrderID *string             `gorm:"size:128" json:"providerOrderId"`
	ProviderTradeNo *string             `gorm:"size:128" json:"providerTradeNo"`
	ProviderPayload datatypes.JSON      `gorm:"type:json" json:"providerPayload"`
	ExpiresAt       *time.Time          `json:"expiresAt"`
	PaidAt          *time.Time          `json:"paidAt"`
	GrantedAt       *time.Time          `json:"grantedAt"`
	CreatedAt       time.Time           `json:"createdAt"`
	UpdatedAt       time.Time           `json:"updatedAt"`
}

type PaymentCallback struct {
	ID              uint            `gorm:"primaryKey" json:"id"`
	Provider        PaymentProvider `gorm:"size:32;index;not null" json:"provider"`
	OrderNo         string          `gorm:"size:64;index;not null" json:"orderNo"`
	ProviderTradeNo string          `gorm:"size:128;index;not null" json:"providerTradeNo"`
	Payload         datatypes.JSON  `gorm:"type:json" json:"payload"`
	Status          string          `gorm:"size:32;not null" json:"status"`
	CreatedAt       time.Time       `json:"createdAt"`
}

type AgentHeartbeat struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	UserID     uint      `gorm:"uniqueIndex;not null" json:"userId"`
	DeviceID   string    `gorm:"size:191;not null;default:''" json:"deviceId"`
	DeviceName string    `gorm:"size:191;not null;default:''" json:"deviceName"`
	Status     string    `gorm:"size:32;not null;default:online" json:"status"`
	LastPollAt time.Time `gorm:"index;not null" json:"lastPollAt"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type AgentTask struct {
	ID           uint            `gorm:"primaryKey" json:"id"`
	UserID       uint            `gorm:"index;not null" json:"userId"`
	Type         AgentTaskType   `gorm:"size:64;index;not null" json:"type"`
	Status       AgentTaskStatus `gorm:"size:32;index;not null;default:pending" json:"status"`
	Payload      datatypes.JSON  `gorm:"type:json" json:"payload"`
	Result       datatypes.JSON  `gorm:"type:json" json:"result"`
	ErrorMessage string          `gorm:"type:text" json:"errorMessage"`
	ClaimedAt    *time.Time      `json:"claimedAt"`
	CompletedAt  *time.Time      `json:"completedAt"`
	ExpiresAt    time.Time       `gorm:"index;not null" json:"expiresAt"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}
