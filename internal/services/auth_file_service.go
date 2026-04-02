package services

import (
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/models"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/storage"
	"gorm.io/gorm"
)

type AuthFileService struct {
	db      *gorm.DB
	storage *storage.Storage
}

func NewAuthFileService(db *gorm.DB, storage *storage.Storage) *AuthFileService {
	return &AuthFileService{db: db, storage: storage}
}

func detectProvider(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "claude"):
		return "claude"
	case strings.Contains(lower, "codex"), strings.Contains(lower, "openai"):
		return "codex"
	case strings.Contains(lower, "gemini"):
		return "gemini-cli"
	case strings.Contains(lower, "antigravity"):
		return "antigravity"
	case strings.Contains(lower, "kimi"), strings.Contains(lower, "moonshot"):
		return "kimi"
	default:
		return "unknown"
	}
}

func (s *AuthFileService) ListPersonal(userID uint) ([]models.AuthFile, error) {
	var files []models.AuthFile
	err := s.db.
		Where("owner_type = ? AND owner_user_id = ?", models.AuthOwnerTypeUser, userID).
		Order("id desc").
		Find(&files).Error
	return files, err
}

func (s *AuthFileService) ListShared() ([]models.AuthFile, error) {
	var files []models.AuthFile
	err := s.db.
		Where("owner_type = ?", models.AuthOwnerTypeShared).
		Order("id desc").
		Find(&files).Error
	return files, err
}

func (s *AuthFileService) Upload(ownerType models.AuthOwnerType, ownerUserID *uint, sourceType models.AuthSourceType, planRequired *string, fileHeader *multipart.FileHeader) (*models.AuthFile, error) {
	if fileHeader == nil {
		return nil, fmt.Errorf("file is required")
	}

	file, err := fileHeader.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	scope := string(ownerType)
	if ownerUserID != nil {
		scope = fmt.Sprintf("%s/%d", ownerType, *ownerUserID)
	}
	path, hash, err := s.storage.Save(scope, fileHeader.Filename, content)
	if err != nil {
		return nil, err
	}

	authFile := &models.AuthFile{
		OwnerType:    ownerType,
		OwnerUserID:  ownerUserID,
		Provider:     detectProvider(fileHeader.Filename),
		FileName:     fileHeader.Filename,
		StoragePath:  path,
		FileHash:     hash,
		Encrypted:    true,
		Status:       "active",
		SourceType:   sourceType,
		PlanRequired: planRequired,
		DisplayName:  strings.TrimSuffix(filepath.Base(fileHeader.Filename), filepath.Ext(fileHeader.Filename)),
	}
	if err := s.db.Create(authFile).Error; err != nil {
		return nil, err
	}

	if err := s.db.Create(&models.AuthFileVersion{
		AuthFileID:  authFile.ID,
		Version:     1,
		StoragePath: path,
		FileHash:    hash,
		CreatedAt:   time.Now(),
	}).Error; err != nil {
		return nil, err
	}

	return authFile, nil
}

func (s *AuthFileService) FindPersonal(userID uint, authFileID uint) (*models.AuthFile, error) {
	var file models.AuthFile
	err := s.db.
		Where("id = ? AND owner_type = ? AND owner_user_id = ?", authFileID, models.AuthOwnerTypeUser, userID).
		First(&file).Error
	if err != nil {
		return nil, err
	}
	return &file, nil
}

func (s *AuthFileService) FindShared(authFileID uint) (*models.AuthFile, error) {
	var file models.AuthFile
	err := s.db.Where("id = ? AND owner_type = ?", authFileID, models.AuthOwnerTypeShared).First(&file).Error
	if err != nil {
		return nil, err
	}
	return &file, nil
}

func (s *AuthFileService) ReadContent(file *models.AuthFile) ([]byte, error) {
	if file == nil {
		return nil, fmt.Errorf("auth file is required")
	}
	return s.storage.Read(file.StoragePath)
}

func (s *AuthFileService) DeletePersonal(userID uint, authFileID uint) error {
	return s.db.Where("id = ? AND owner_type = ? AND owner_user_id = ?", authFileID, models.AuthOwnerTypeUser, userID).Delete(&models.AuthFile{}).Error
}
