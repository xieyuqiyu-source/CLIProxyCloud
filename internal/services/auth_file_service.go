package services

import (
	"fmt"
	"io"
	"math/rand"
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

func (s *AuthFileService) ListSharedByStrategy(features FeatureFlags) ([]models.AuthFile, error) {
	files, err := s.ListShared()
	if err != nil {
		return nil, err
	}

	if !features.AllowSharedPool || features.SharedPoolMode == "none" {
		return []models.AuthFile{}, nil
	}

	if features.SharedPoolMode != "sample" || features.SharedPoolMaxFiles <= 0 || len(files) <= features.SharedPoolMaxFiles {
		return files, nil
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	shuffled := append([]models.AuthFile(nil), files...)
	rng.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	return shuffled[:features.SharedPoolMaxFiles], nil
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
	displayName := strings.TrimSuffix(filepath.Base(fileHeader.Filename), filepath.Ext(fileHeader.Filename))
	provider := detectProvider(fileHeader.Filename)

	var existing models.AuthFile
	query := s.db.Where("owner_type = ? AND file_name = ?", ownerType, fileHeader.Filename)
	if ownerUserID == nil {
		query = query.Where("owner_user_id IS NULL")
	} else {
		query = query.Where("owner_user_id = ?", *ownerUserID)
	}

	err = query.First(&existing).Error
	if err == nil {
		oldPath := existing.StoragePath
		existing.Provider = provider
		existing.StoragePath = path
		existing.FileHash = hash
		existing.Encrypted = true
		existing.Status = "active"
		existing.SourceType = sourceType
		existing.PlanRequired = planRequired
		existing.DisplayName = displayName
		if err := s.db.Save(&existing).Error; err != nil {
			return nil, err
		}

		version := 1
		var lastVersion models.AuthFileVersion
		if err := s.db.Where("auth_file_id = ?", existing.ID).Order("version desc").First(&lastVersion).Error; err == nil {
			version = lastVersion.Version + 1
		}
		if err := s.db.Create(&models.AuthFileVersion{
			AuthFileID:  existing.ID,
			Version:     version,
			StoragePath: path,
			FileHash:    hash,
			CreatedAt:   time.Now(),
		}).Error; err != nil {
			return nil, err
		}
		_ = s.storage.Delete(oldPath)
		return &existing, nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}

	authFile := &models.AuthFile{
		OwnerType:    ownerType,
		OwnerUserID:  ownerUserID,
		Provider:     provider,
		FileName:     fileHeader.Filename,
		StoragePath:  path,
		FileHash:     hash,
		Encrypted:    true,
		Status:       "active",
		SourceType:   sourceType,
		PlanRequired: planRequired,
		DisplayName:  displayName,
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
	file, err := s.FindPersonal(userID, authFileID)
	if err != nil {
		return err
	}
	return s.deleteAuthFile(file)
}

func (s *AuthFileService) DeleteAllPersonal(userID uint) (int64, error) {
	var files []models.AuthFile
	if err := s.db.Where("owner_type = ? AND owner_user_id = ?", models.AuthOwnerTypeUser, userID).Find(&files).Error; err != nil {
		return 0, err
	}
	var deleted int64
	for index := range files {
		if err := s.deleteAuthFile(&files[index]); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func (s *AuthFileService) DeleteShared(authFileID uint) error {
	file, err := s.FindShared(authFileID)
	if err != nil {
		return err
	}
	return s.deleteAuthFile(file)
}

func (s *AuthFileService) DeleteAllShared() (int64, error) {
	var files []models.AuthFile
	if err := s.db.Where("owner_type = ?", models.AuthOwnerTypeShared).Find(&files).Error; err != nil {
		return 0, err
	}
	var deleted int64
	for index := range files {
		if err := s.deleteAuthFile(&files[index]); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func (s *AuthFileService) deleteAuthFile(file *models.AuthFile) error {
	if file == nil {
		return nil
	}
	if err := s.db.Where("auth_file_id = ?", file.ID).Delete(&models.AuthFileVersion{}).Error; err != nil {
		return err
	}
	if err := s.db.Delete(&models.AuthFile{}, file.ID).Error; err != nil {
		return err
	}
	_ = s.storage.Delete(file.StoragePath)
	return nil
}
