package services

import (
	"archive/zip"
	"bytes"
	"encoding/json"
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
	"gorm.io/gorm/clause"
)

type AuthFileService struct {
	db      *gorm.DB
	storage *storage.Storage
}

type AuthFileUploadResult struct {
	Files   []models.AuthFile `json:"files"`
	Skipped []string          `json:"skipped"`
}

type AuthFileUploadOptions struct {
	DistributionMode models.AuthDistributionMode
	QuotaLimit       int64
	QuotaResetAt     *time.Time
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

	if features.SharedPoolMaxFiles <= 0 || len(files) <= features.SharedPoolMaxFiles {
		return files, nil
	}

	if features.SharedPoolMode != "sample" {
		return files[:features.SharedPoolMaxFiles], nil
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	shuffled := append([]models.AuthFile(nil), files...)
	rng.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	return shuffled[:features.SharedPoolMaxFiles], nil
}

func (s *AuthFileService) Upload(ownerType models.AuthOwnerType, ownerUserID *uint, sourceType models.AuthSourceType, planRequired *string, fileHeader *multipart.FileHeader) (*models.AuthFile, error) {
	result, err := s.UploadMany(ownerType, ownerUserID, sourceType, planRequired, fileHeader, AuthFileUploadOptions{})
	if err != nil {
		return nil, err
	}
	if len(result.Files) == 0 {
		if len(result.Skipped) > 0 {
			return nil, fmt.Errorf("no valid JSON credential files found: %s", strings.Join(result.Skipped, "; "))
		}
		return nil, fmt.Errorf("no valid JSON credential files found")
	}
	return &result.Files[0], nil
}

func (s *AuthFileService) UploadMany(ownerType models.AuthOwnerType, ownerUserID *uint, sourceType models.AuthSourceType, planRequired *string, fileHeader *multipart.FileHeader, options AuthFileUploadOptions) (*AuthFileUploadResult, error) {
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

	entries, skipped, err := extractAuthJSONFiles(fileHeader.Filename, content)
	if err != nil {
		return nil, err
	}
	result := &AuthFileUploadResult{Skipped: skipped}
	for _, entry := range entries {
		authFile, err := s.uploadBytes(ownerType, ownerUserID, sourceType, planRequired, entry.name, entry.content, normalizeAuthUploadOptions(options))
		if err != nil {
			result.Skipped = append(result.Skipped, fmt.Sprintf("%s: %s", entry.name, err.Error()))
			continue
		}
		result.Files = append(result.Files, *authFile)
	}
	if len(result.Files) == 0 {
		return result, fmt.Errorf("no valid JSON credential files found")
	}
	return result, nil
}

func normalizeAuthUploadOptions(options AuthFileUploadOptions) AuthFileUploadOptions {
	switch options.DistributionMode {
	case models.AuthDistributionQuotaCard:
	default:
		options.DistributionMode = models.AuthDistributionPlain
	}
	if options.QuotaLimit < 0 {
		options.QuotaLimit = 0
	}
	return options
}

type extractedAuthFile struct {
	name    string
	content []byte
}

func extractAuthJSONFiles(fileName string, content []byte) ([]extractedAuthFile, []string, error) {
	lowerName := strings.ToLower(fileName)
	if strings.HasSuffix(lowerName, ".zip") {
		return extractAuthJSONFilesFromZip(content)
	}
	if !json.Valid(content) {
		return nil, []string{fmt.Sprintf("%s: invalid JSON", fileName)}, nil
	}
	return []extractedAuthFile{{
		name:    normalizeAuthUploadFileName(filepath.Base(fileName)),
		content: content,
	}}, nil, nil
}

func extractAuthJSONFilesFromZip(content []byte) ([]extractedAuthFile, []string, error) {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read zip file: %w", err)
	}

	var entries []extractedAuthFile
	var skipped []string
	seenNames := map[string]int{}
	for _, item := range reader.File {
		if item.FileInfo().IsDir() {
			continue
		}
		name := filepath.Base(item.Name)
		if name == "." || name == "" {
			continue
		}

		file, err := item.Open()
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %s", item.Name, err.Error()))
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(file, 16*1024*1024+1))
		_ = file.Close()
		if readErr != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %s", item.Name, readErr.Error()))
			continue
		}
		if len(data) > 16*1024*1024 {
			skipped = append(skipped, fmt.Sprintf("%s: file too large", item.Name))
			continue
		}
		if !json.Valid(data) {
			skipped = append(skipped, fmt.Sprintf("%s: invalid JSON", item.Name))
			continue
		}

		uploadName := normalizeAuthUploadFileName(name)
		nameCount := seenNames[uploadName]
		if nameCount > 0 {
			ext := filepath.Ext(uploadName)
			base := strings.TrimSuffix(uploadName, ext)
			uploadName = fmt.Sprintf("%s-%d%s", base, nameCount+1, ext)
		}
		seenNames[normalizeAuthUploadFileName(name)]++
		entries = append(entries, extractedAuthFile{name: uploadName, content: data})
	}
	if len(entries) == 0 {
		skipped = append(skipped, "zip: no valid JSON credential files found")
	}
	return entries, skipped, nil
}

func normalizeAuthUploadFileName(name string) string {
	trimmed := strings.TrimSpace(filepath.Base(name))
	if trimmed == "" || trimmed == "." {
		trimmed = fmt.Sprintf("credential-%d", time.Now().UnixNano())
	}
	if strings.HasSuffix(strings.ToLower(trimmed), ".json") {
		return trimmed
	}
	ext := filepath.Ext(trimmed)
	base := strings.TrimSuffix(trimmed, ext)
	if base == "" {
		base = strings.TrimPrefix(trimmed, ".")
	}
	if base == "" {
		base = "credential"
	}
	return base + ".json"
}

func (s *AuthFileService) uploadBytes(ownerType models.AuthOwnerType, ownerUserID *uint, sourceType models.AuthSourceType, planRequired *string, fileName string, content []byte, options AuthFileUploadOptions) (*models.AuthFile, error) {
	if !json.Valid(content) {
		return nil, fmt.Errorf("invalid JSON")
	}
	fileName = normalizeAuthUploadFileName(fileName)

	scope := string(ownerType)
	if ownerUserID != nil {
		scope = fmt.Sprintf("%s/%d", ownerType, *ownerUserID)
	}
	path, hash, err := s.storage.Save(scope, fileName, content)
	if err != nil {
		return nil, err
	}
	displayName := strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
	provider := detectProvider(fileName)

	var existing models.AuthFile
	query := s.db.Where("owner_type = ? AND file_name = ?", ownerType, fileName)
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
		existing.DistributionMode = options.DistributionMode
		existing.QuotaLimit = options.QuotaLimit
		existing.QuotaResetAt = options.QuotaResetAt
		if existing.DistributionMode != models.AuthDistributionQuotaCard {
			existing.QuotaUsed = 0
			existing.QuotaResetAt = nil
		}
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
		OwnerType:        ownerType,
		OwnerUserID:      ownerUserID,
		Provider:         provider,
		FileName:         fileName,
		StoragePath:      path,
		FileHash:         hash,
		Encrypted:        true,
		Status:           "active",
		SourceType:       sourceType,
		PlanRequired:     planRequired,
		DisplayName:      displayName,
		DistributionMode: options.DistributionMode,
		QuotaLimit:       options.QuotaLimit,
		QuotaResetAt:     options.QuotaResetAt,
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

func (s *AuthFileService) ConsumeQuotaCard(authFileID uint, units int64) (*models.AuthFile, error) {
	if units <= 0 {
		units = 1
	}

	var file models.AuthFile
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND owner_type = ?", authFileID, models.AuthOwnerTypeShared).
			First(&file).Error; err != nil {
			return err
		}
		if file.DistributionMode != models.AuthDistributionQuotaCard {
			return fmt.Errorf("shared auth file is not a quota card")
		}
		if file.QuotaLimit > 0 && file.QuotaUsed+units > file.QuotaLimit {
			return fmt.Errorf("quota card limit exceeded")
		}
		file.QuotaUsed += units
		return tx.Save(&file).Error
	})
	if err != nil {
		return nil, err
	}
	return &file, nil
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
