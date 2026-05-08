package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	trustedDeviceTTL      = 7 * 24 * time.Hour
	loginCodeTTL          = 10 * time.Minute
	loginCodeLength       = 6
	defaultDeviceName     = "CPSwitch"
	defaultDevicePlatform = "desktop"
)

type SessionClaims struct {
	UserID         uint
	Email          string
	Role           models.UserRole
	PlanCode       string
	DeviceID       string
	SessionVersion uint64
}

type LoginSuccess struct {
	User         *models.User
	Token        string
	Device       *models.Device
	TrustedToken string
	TrustedUntil *time.Time
}

type LoginChallengeResult struct {
	ChallengeID string
	MaskedEmail string
	ExpiresAt   time.Time
	DebugCode   string
}

type RegistrationChallengeResult struct {
	ChallengeID string
	MaskedEmail string
	ExpiresAt   time.Time
	DebugCode   string
}

type DeviceConflict struct {
	DeviceID   string     `json:"deviceId"`
	DeviceName string     `json:"deviceName"`
	Platform   string     `json:"platform"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
}

type AuthService struct {
	db        *gorm.DB
	jwtSecret []byte
	planSvc   *PlanService
	emailSvc  *EmailService
	appEnv    string
}

func NewAuthService(db *gorm.DB, jwtSecret string, planSvc *PlanService, emailSvc *EmailService, appEnv string) *AuthService {
	return &AuthService{
		db:        db,
		jwtSecret: []byte(jwtSecret),
		planSvc:   planSvc,
		emailSvc:  emailSvc,
		appEnv:    strings.TrimSpace(strings.ToLower(appEnv)),
	}
}

func (s *AuthService) BeginRegister(email string, password string) (*RegistrationChallengeResult, error) {
	if email == "" || password == "" {
		return nil, fmt.Errorf("email and password are required")
	}
	if len(password) < 6 {
		return nil, fmt.Errorf("password must be at least 6 characters")
	}
	var existing models.User
	if err := s.db.Where("email = ?", email).First(&existing).Error; err == nil {
		return nil, fmt.Errorf("email already exists")
	} else if err != gorm.ErrRecordNotFound {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	code, err := generateNumericCode(loginCodeLength)
	if err != nil {
		return nil, err
	}
	challenge := &models.RegistrationVerification{
		ChallengeID:  uuid.NewString(),
		Email:        email,
		PasswordHash: string(hash),
		CodeHash:     hashToken(code),
		ExpiresAt:    time.Now().Add(loginCodeTTL),
	}
	if err := s.db.Where("email = ? AND consumed_at IS NULL", email).Delete(&models.RegistrationVerification{}).Error; err != nil {
		return nil, err
	}
	if err := s.db.Create(challenge).Error; err != nil {
		return nil, err
	}
	if s.emailSvc != nil && s.emailSvc.Configured() {
		if err := s.emailSvc.SendLoginCode(email, code, "新账号注册"); err != nil {
			return nil, err
		}
		return &RegistrationChallengeResult{
			ChallengeID: challenge.ChallengeID,
			MaskedEmail: maskEmail(email),
			ExpiresAt:   challenge.ExpiresAt,
		}, nil
	}
	if s.appEnv == "production" {
		return nil, fmt.Errorf("email verification is not configured")
	}
	return &RegistrationChallengeResult{
		ChallengeID: challenge.ChallengeID,
		MaskedEmail: maskEmail(email),
		ExpiresAt:   challenge.ExpiresAt,
		DebugCode:   code,
	}, nil
}

func (s *AuthService) VerifyRegister(email string, challengeID string, code string) (*models.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	challengeID = strings.TrimSpace(challengeID)
	code = strings.TrimSpace(code)
	if email == "" || challengeID == "" || code == "" {
		return nil, fmt.Errorf("email, challenge_id and code are required")
	}

	var existing models.User
	if err := s.db.Where("email = ?", email).First(&existing).Error; err == nil {
		return nil, fmt.Errorf("email already exists")
	} else if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}

	var challenge models.RegistrationVerification
	if err := s.db.Where("challenge_id = ? AND email = ?", challengeID, email).First(&challenge).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("verification challenge not found")
		}
		return nil, err
	}
	if challenge.ConsumedAt != nil {
		return nil, fmt.Errorf("verification challenge already used")
	}
	if time.Now().After(challenge.ExpiresAt) {
		return nil, fmt.Errorf("verification code expired")
	}
	if hashToken(code) != challenge.CodeHash {
		return nil, fmt.Errorf("verification code is invalid")
	}

	now := time.Now()
	var user *models.User
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&challenge).Updates(map[string]any{
			"consumed_at": now,
		}).Error; err != nil {
			return err
		}
		created := &models.User{
			Email:        email,
			PasswordHash: challenge.PasswordHash,
			Role:         models.UserRoleUser,
			Status:       "active",
		}
		if err := tx.Create(created).Error; err != nil {
			return err
		}
		user = created
		return nil
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *AuthService) BeginPasswordLogin(email string, password string, deviceID string, deviceName string, platform string, trustDevice bool) (*LoginSuccess, *LoginChallengeResult, error) {
	user, err := s.authenticateUser(email, password)
	if err != nil {
		return nil, nil, err
	}
	deviceID, deviceName, platform = normalizeDeviceFields(deviceID, deviceName, platform)

	if user.Role == models.UserRoleAdmin {
		login, err := s.finalizeLogin(user, deviceID, deviceName, platform, trustDevice, true)
		return login, nil, err
	}

	device, _ := s.findDevice(user.ID, deviceID)
	if device != nil && device.TrustedUntil != nil && device.TrustedTokenHash != nil && device.TrustedUntil.After(time.Now()) {
		login, err := s.finalizeLogin(user, deviceID, deviceName, platform, trustDevice, true)
		return login, nil, err
	}

	challenge, debugCode, err := s.createLoginChallenge(user, deviceID, deviceName, platform)
	if err != nil {
		return nil, nil, err
	}
	return nil, &LoginChallengeResult{
		ChallengeID: challenge.ChallengeID,
		MaskedEmail: maskEmail(user.Email),
		ExpiresAt:   challenge.ExpiresAt,
		DebugCode:   debugCode,
	}, nil
}

func (s *AuthService) VerifyLoginChallenge(email string, challengeID string, code string, trustDevice bool, forceLogoutExisting bool) (*LoginSuccess, *DeviceConflict, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	challengeID = strings.TrimSpace(challengeID)
	code = strings.TrimSpace(code)
	if email == "" || challengeID == "" || code == "" {
		return nil, nil, fmt.Errorf("email, challenge_id and code are required")
	}

	var challenge models.LoginVerification
	if err := s.db.Where("challenge_id = ? AND email = ?", challengeID, email).First(&challenge).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, fmt.Errorf("verification challenge not found")
		}
		return nil, nil, err
	}
	if challenge.ConsumedAt != nil {
		return nil, nil, fmt.Errorf("verification challenge already used")
	}
	if time.Now().After(challenge.ExpiresAt) {
		return nil, nil, fmt.Errorf("verification code expired")
	}
	if hashToken(code) != challenge.CodeHash {
		return nil, nil, fmt.Errorf("verification code is invalid")
	}

	var user models.User
	if err := s.db.First(&user, challenge.UserID).Error; err != nil {
		return nil, nil, err
	}

	if user.Role != models.UserRoleAdmin && user.ActiveDeviceID != nil && *user.ActiveDeviceID != challenge.DeviceID && !forceLogoutExisting {
		conflict, err := s.lookupDeviceConflict(*user.ActiveDeviceID)
		if err != nil {
			return nil, nil, err
		}
		return nil, conflict, nil
	}

	now := time.Now()
	if err := s.db.Model(&challenge).Updates(map[string]any{
		"consumed_at": now,
	}).Error; err != nil {
		return nil, nil, err
	}

	login, err := s.finalizeLogin(&user, challenge.DeviceID, challenge.DeviceName, challenge.Platform, trustDevice, true)
	if err != nil {
		return nil, nil, err
	}
	return login, nil, nil
}

func (s *AuthService) LoginTrustedDevice(email string, deviceID string, trustedToken string, deviceName string, platform string) (*LoginSuccess, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	deviceID, deviceName, platform = normalizeDeviceFields(deviceID, deviceName, platform)
	trustedToken = strings.TrimSpace(trustedToken)
	if email == "" || deviceID == "" || trustedToken == "" {
		return nil, fmt.Errorf("trusted device credentials are required")
	}

	var user models.User
	if err := s.db.Where("email = ?", email).First(&user).Error; err != nil {
		return nil, fmt.Errorf("trusted device login expired")
	}
	device, err := s.findDevice(user.ID, deviceID)
	if err != nil || device == nil {
		return nil, fmt.Errorf("trusted device login expired")
	}
	if device.TrustedTokenHash == nil || device.TrustedUntil == nil || time.Now().After(*device.TrustedUntil) {
		return nil, fmt.Errorf("trusted device login expired")
	}
	if hashToken(trustedToken) != *device.TrustedTokenHash {
		return nil, fmt.Errorf("trusted device login expired")
	}

	return s.finalizeLogin(&user, deviceID, deviceName, platform, true, false)
}

func (s *AuthService) ChangePassword(userID uint, currentPassword string, newPassword string) error {
	if currentPassword == "" || newPassword == "" {
		return fmt.Errorf("current password and new password are required")
	}
	if len(newPassword) < 6 {
		return fmt.Errorf("new password must be at least 6 characters")
	}

	var user models.User
	if err := s.db.First(&user, userID).Error; err != nil {
		return err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
		return fmt.Errorf("current password is invalid")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
			"password_hash":    string(hash),
			"active_device_id": nil,
			"session_version":  gorm.Expr("session_version + 1"),
		}).Error; err != nil {
			return err
		}
		return tx.Model(&models.Device{}).Where("user_id = ?", userID).Updates(map[string]any{
			"trusted_token_hash": nil,
			"trusted_until":      nil,
		}).Error
	})
}

func (s *AuthService) Logout(userID uint, deviceID string) error {
	deviceID = strings.TrimSpace(deviceID)
	return s.db.Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.First(&user, userID).Error; err != nil {
			return err
		}
		updates := map[string]any{
			"session_version": gorm.Expr("session_version + 1"),
		}
		if user.ActiveDeviceID != nil && *user.ActiveDeviceID == deviceID {
			updates["active_device_id"] = nil
		}
		if err := tx.Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
			return err
		}
		if deviceID == "" {
			return nil
		}
		return tx.Model(&models.Device{}).Where("user_id = ? AND device_id = ?", userID, deviceID).Updates(map[string]any{
			"trusted_token_hash": nil,
			"trusted_until":      nil,
		}).Error
	})
}

func (s *AuthService) ParseToken(raw string) (*SessionClaims, error) {
	token, err := jwt.Parse(raw, func(token *jwt.Token) (interface{}, error) {
		return s.jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid token claims")
	}
	sub, ok := claims["sub"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid token subject")
	}
	result := &SessionClaims{
		UserID:   uint(sub),
		Email:    stringClaim(claims, "email"),
		Role:     models.UserRole(stringClaim(claims, "role")),
		PlanCode: stringClaim(claims, "plan_code"),
		DeviceID: stringClaim(claims, "device_id"),
	}
	if sessionVersion, ok := uint64Claim(claims, "session_version"); ok {
		result.SessionVersion = sessionVersion
	}
	return result, nil
}

func (s *AuthService) authenticateUser(email string, password string) (*models.User, error) {
	var user models.User
	if err := s.db.Where("email = ?", email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("invalid credentials")
		}
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	return &user, nil
}

func (s *AuthService) finalizeLogin(user *models.User, deviceID string, deviceName string, platform string, trustDevice bool, verified bool) (*LoginSuccess, error) {
	plan, features, err := s.planSvc.ResolveUserPlan(user)
	if err != nil {
		return nil, err
	}
	deviceID, deviceName, platform = normalizeDeviceFields(deviceID, deviceName, platform)

	var login *LoginSuccess
	err = s.db.Transaction(func(tx *gorm.DB) error {
		device, rawTrustedToken, err := s.upsertDevice(tx, user.ID, features.MaxDevices, deviceID, deviceName, platform, trustDevice, verified)
		if err != nil {
			return err
		}

		var sessionVersion uint64
		activeDeviceID := deviceID
		if user.Role == models.UserRoleAdmin {
			sessionVersion = user.SessionVersion
		} else {
			sessionVersion = user.SessionVersion + 1
			if err := tx.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
				"active_device_id": activeDeviceID,
				"session_version":  sessionVersion,
			}).Error; err != nil {
				return err
			}
			user.ActiveDeviceID = &activeDeviceID
			user.SessionVersion = sessionVersion
		}

		token, err := s.issueToken(user, plan, deviceID, sessionVersion)
		if err != nil {
			return err
		}

		login = &LoginSuccess{
			User:         user,
			Token:        token,
			Device:       device,
			TrustedToken: rawTrustedToken,
			TrustedUntil: device.TrustedUntil,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return login, nil
}

func (s *AuthService) upsertDevice(tx *gorm.DB, userID uint, maxDevices int, deviceID string, deviceName string, platform string, trustDevice bool, verified bool) (*models.Device, string, error) {
	var device models.Device
	err := tx.Where("user_id = ? AND device_id = ?", userID, deviceID).First(&device).Error
	now := time.Now()
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", err
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := s.trimDevices(tx, userID, maxDevices, deviceID); err != nil {
			return nil, "", err
		}
		device = models.Device{
			UserID:     userID,
			DeviceID:   deviceID,
			DeviceName: deviceName,
			Platform:   platform,
			Status:     models.DeviceStatusActive,
			LastSeenAt: now,
		}
	}

	device.UserID = userID
	device.DeviceName = deviceName
	device.Platform = platform
	device.Status = models.DeviceStatusActive
	device.LastSeenAt = now
	device.LastLoginAt = &now
	if verified {
		device.LastVerifiedAt = &now
	}

	rawTrustedToken := ""
	if trustDevice {
		rawToken, tokenHash, trustedUntil, err := generateTrustedToken()
		if err != nil {
			return nil, "", err
		}
		device.TrustedTokenHash = &tokenHash
		device.TrustedUntil = &trustedUntil
		rawTrustedToken = rawToken
	} else {
		device.TrustedTokenHash = nil
		device.TrustedUntil = nil
	}

	if device.ID == 0 {
		if err := tx.Create(&device).Error; err != nil {
			return nil, "", err
		}
	} else {
		if err := tx.Save(&device).Error; err != nil {
			return nil, "", err
		}
	}

	return &device, rawTrustedToken, nil
}

func (s *AuthService) trimDevices(tx *gorm.DB, userID uint, maxDevices int, keepDeviceID string) error {
	if maxDevices <= 0 {
		return nil
	}
	var devices []models.Device
	if err := tx.Where("user_id = ?", userID).Order("last_seen_at asc, id asc").Find(&devices).Error; err != nil {
		return err
	}
	if len(devices) < maxDevices {
		return nil
	}
	excess := len(devices) - maxDevices + 1
	for _, device := range devices {
		if excess <= 0 {
			break
		}
		if device.DeviceID == keepDeviceID {
			continue
		}
		if err := tx.Delete(&device).Error; err != nil {
			return err
		}
		excess--
	}
	return nil
}

func (s *AuthService) createLoginChallenge(user *models.User, deviceID string, deviceName string, platform string) (*models.LoginVerification, string, error) {
	code, err := generateNumericCode(loginCodeLength)
	if err != nil {
		return nil, "", err
	}
	challenge := &models.LoginVerification{
		ChallengeID: uuid.NewString(),
		UserID:      user.ID,
		Email:       user.Email,
		DeviceID:    deviceID,
		DeviceName:  deviceName,
		Platform:    platform,
		CodeHash:    hashToken(code),
		ExpiresAt:   time.Now().Add(loginCodeTTL),
	}
	if err := s.db.Where("user_id = ? AND device_id = ? AND consumed_at IS NULL", user.ID, deviceID).Delete(&models.LoginVerification{}).Error; err != nil {
		return nil, "", err
	}
	if err := s.db.Create(challenge).Error; err != nil {
		return nil, "", err
	}
	if s.emailSvc != nil && s.emailSvc.Configured() {
		if err := s.emailSvc.SendLoginCode(user.Email, code, deviceName); err != nil {
			return nil, "", err
		}
		return challenge, "", nil
	}
	if s.appEnv == "production" {
		return nil, "", fmt.Errorf("email verification is not configured")
	}
	return challenge, code, nil
}

func (s *AuthService) issueToken(user *models.User, plan *models.Plan, deviceID string, sessionVersion uint64) (string, error) {
	claims := jwt.MapClaims{
		"sub":       user.ID,
		"email":     user.Email,
		"role":      user.Role,
		"plan_code": plan.PlanCode,
		"exp":       time.Now().Add(7 * 24 * time.Hour).Unix(),
		"iat":       time.Now().Unix(),
	}
	if user.Role != models.UserRoleAdmin {
		claims["device_id"] = deviceID
		claims["session_version"] = sessionVersion
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

func (s *AuthService) findDevice(userID uint, deviceID string) (*models.Device, error) {
	var device models.Device
	if err := s.db.Where("user_id = ? AND device_id = ?", userID, deviceID).First(&device).Error; err != nil {
		return nil, err
	}
	return &device, nil
}

func (s *AuthService) lookupDeviceConflict(deviceID string) (*DeviceConflict, error) {
	if deviceID == "" {
		return nil, nil
	}
	var device models.Device
	if err := s.db.Where("device_id = ?", deviceID).First(&device).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &DeviceConflict{
		DeviceID:   device.DeviceID,
		DeviceName: device.DeviceName,
		Platform:   device.Platform,
		LastSeenAt: &device.LastSeenAt,
	}, nil
}

func normalizeDeviceFields(deviceID string, deviceName string, platform string) (string, string, string) {
	deviceID = strings.TrimSpace(deviceID)
	deviceName = strings.TrimSpace(deviceName)
	platform = strings.TrimSpace(platform)
	if deviceName == "" {
		deviceName = defaultDeviceName
	}
	if platform == "" {
		platform = defaultDevicePlatform
	}
	return deviceID, deviceName, platform
}

func hashToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func generateTrustedToken() (string, string, time.Time, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", time.Time{}, err
	}
	raw := hex.EncodeToString(buffer)
	expiresAt := time.Now().Add(trustedDeviceTTL)
	return raw, hashToken(raw), expiresAt, nil
}

func generateNumericCode(length int) (string, error) {
	var builder strings.Builder
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		builder.WriteByte(byte('0' + n.Int64()))
	}
	return builder.String(), nil
}

func maskEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return email
	}
	local := parts[0]
	if len(local) <= 2 {
		return local[:1] + "***@" + parts[1]
	}
	return local[:1] + "***" + local[len(local)-1:] + "@" + parts[1]
}

func stringClaim(claims jwt.MapClaims, key string) string {
	value, _ := claims[key].(string)
	return value
}

func uint64Claim(claims jwt.MapClaims, key string) (uint64, bool) {
	value, ok := claims[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		return uint64(typed), true
	case int64:
		return uint64(typed), true
	case uint64:
		return typed, true
	default:
		return 0, false
	}
}
