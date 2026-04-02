package services

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type FeatureFlags struct {
	MaxEnabledAuthFiles    int  `json:"max_enabled_auth_files"`
	AllowAutoRotation      bool `json:"allow_auto_rotation"`
	AllowPersonalCloudSync bool `json:"allow_personal_cloud_sync"`
	AllowSharedPool        bool `json:"allow_shared_pool"`
	MaxDevices             int  `json:"max_devices"`
}

type PlanService struct {
	db *gorm.DB
}

func NewPlanService(db *gorm.DB) *PlanService {
	return &PlanService{db: db}
}

func DefaultPlans() map[string]struct {
	Name        string
	Description string
	Features    FeatureFlags
} {
	return map[string]struct {
		Name        string
		Description string
		Features    FeatureFlags
	}{
		"free": {
			Name:        "免费版",
			Description: "只能启用一个本地认证文件，不支持自动切换和云同步",
			Features: FeatureFlags{
				MaxEnabledAuthFiles:    1,
				AllowAutoRotation:      false,
				AllowPersonalCloudSync: false,
				AllowSharedPool:        false,
				MaxDevices:             1,
			},
		},
		"vip1": {
			Name:        "VIP 1",
			Description: "支持多个认证文件、自动切换、个人云同步",
			Features: FeatureFlags{
				MaxEnabledAuthFiles:    999,
				AllowAutoRotation:      true,
				AllowPersonalCloudSync: true,
				AllowSharedPool:        false,
				MaxDevices:             1,
			},
		},
		"vip2": {
			Name:        "VIP 2",
			Description: "支持 VIP1 全部功能，并可下载共享认证文件",
			Features: FeatureFlags{
				MaxEnabledAuthFiles:    999,
				AllowAutoRotation:      true,
				AllowPersonalCloudSync: true,
				AllowSharedPool:        true,
				MaxDevices:             1,
			},
		},
		"admin": {
			Name:        "管理员",
			Description: "系统管理员，拥有全部权限",
			Features: FeatureFlags{
				MaxEnabledAuthFiles:    999,
				AllowAutoRotation:      true,
				AllowPersonalCloudSync: true,
				AllowSharedPool:        true,
				MaxDevices:             999,
			},
		},
	}
}

func (s *PlanService) SeedDefaults() error {
	for code, plan := range DefaultPlans() {
		blob, err := json.Marshal(plan.Features)
		if err != nil {
			return err
		}

		var current models.Plan
		err = s.db.Where("plan_code = ?", code).First(&current).Error
		if err == nil {
			current.Name = plan.Name
			current.Description = plan.Description
			current.FeatureFlags = datatypes.JSON(blob)
			if err := s.db.Save(&current).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}

		if err := s.db.Create(&models.Plan{
			PlanCode:     code,
			Name:         plan.Name,
			Description:  plan.Description,
			FeatureFlags: datatypes.JSON(blob),
		}).Error; err != nil {
			return err
		}
	}

	return nil
}

func (s *PlanService) FindByCode(planCode string) (*models.Plan, error) {
	var plan models.Plan
	if err := s.db.Where("plan_code = ?", planCode).First(&plan).Error; err != nil {
		return nil, err
	}
	return &plan, nil
}

func (s *PlanService) ListPlans() ([]models.Plan, error) {
	var plans []models.Plan
	if err := s.db.Order("id asc").Find(&plans).Error; err != nil {
		return nil, err
	}
	return plans, nil
}

func (s *PlanService) FeaturesForPlan(plan *models.Plan) (FeatureFlags, error) {
	if plan == nil {
		return FeatureFlags{}, fmt.Errorf("plan is required")
	}
	var features FeatureFlags
	if len(plan.FeatureFlags) == 0 {
		return features, nil
	}
	if err := json.Unmarshal(plan.FeatureFlags, &features); err != nil {
		return FeatureFlags{}, err
	}
	return features, nil
}

func (s *PlanService) GetActiveSubscription(userID uint) (*models.UserSubscription, *models.Plan, error) {
	var sub models.UserSubscription
	err := s.db.
		Where("user_id = ? AND status = ?", userID, models.SubscriptionStatusActive).
		Order("id desc").
		First(&sub).
		Error
	if err != nil {
		return nil, nil, err
	}

	if sub.ExpiresAt != nil && sub.ExpiresAt.Before(time.Now()) {
		sub.Status = models.SubscriptionStatusExpired
		_ = s.db.Save(&sub).Error
		return nil, nil, gorm.ErrRecordNotFound
	}

	var plan models.Plan
	if err := s.db.First(&plan, sub.PlanID).Error; err != nil {
		return nil, nil, err
	}

	return &sub, &plan, nil
}

func (s *PlanService) ResolveUserPlan(user *models.User) (*models.Plan, FeatureFlags, error) {
	if user.Role == models.UserRoleAdmin {
		plan, err := s.FindByCode("admin")
		if err != nil {
			return nil, FeatureFlags{}, err
		}
		features, err := s.FeaturesForPlan(plan)
		return plan, features, err
	}

	_, plan, err := s.GetActiveSubscription(user.ID)
	if err == nil {
		features, ferr := s.FeaturesForPlan(plan)
		return plan, features, ferr
	}
	if err != gorm.ErrRecordNotFound {
		return nil, FeatureFlags{}, err
	}

	plan, err = s.FindByCode("free")
	if err != nil {
		return nil, FeatureFlags{}, err
	}
	features, err := s.FeaturesForPlan(plan)
	return plan, features, err
}

func (s *PlanService) AssignPlan(userID uint, planCode string, expiresAt *time.Time) error {
	plan, err := s.FindByCode(planCode)
	if err != nil {
		return err
	}

	if err := s.db.Model(&models.UserSubscription{}).
		Where("user_id = ? AND status = ?", userID, models.SubscriptionStatusActive).
		Update("status", models.SubscriptionStatusCanceled).Error; err != nil {
		return err
	}

	return s.db.Create(&models.UserSubscription{
		UserID:    userID,
		PlanID:    plan.ID,
		Status:    models.SubscriptionStatusActive,
		StartsAt:  time.Now(),
		ExpiresAt: expiresAt,
	}).Error
}
