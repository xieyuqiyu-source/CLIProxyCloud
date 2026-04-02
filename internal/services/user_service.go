package services

import (
	"fmt"
	"strings"

	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type UserService struct {
	db *gorm.DB
}

func NewUserService(db *gorm.DB) *UserService {
	return &UserService{db: db}
}

func (s *UserService) FindByID(id uint) (*models.User, error) {
	var user models.User
	if err := s.db.First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *UserService) EnsureAdmin(email string, password string) error {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || password == "" {
		return nil
	}

	var existing models.User
	err := s.db.Where("email = ?", email).First(&existing).Error
	if err == nil {
		if existing.Role != models.UserRoleAdmin {
			existing.Role = models.UserRoleAdmin
			return s.db.Save(&existing).Error
		}
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.db.Create(&models.User{
		Email:        email,
		PasswordHash: string(hash),
		Role:         models.UserRoleAdmin,
		Status:       "active",
	}).Error
}

func (s *UserService) UpdateRole(userID uint, role models.UserRole) error {
	return s.db.Model(&models.User{}).Where("id = ?", userID).Update("role", role).Error
}

func (s *UserService) RequireAdmin(user *models.User) error {
	if user == nil || user.Role != models.UserRoleAdmin {
		return fmt.Errorf("admin permission required")
	}
	return nil
}
