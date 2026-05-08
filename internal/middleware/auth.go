package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/models"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/services"
)

const userContextKey = "auth_user"
const authClaimsContextKey = "auth_claims"

type AuthMiddleware struct {
	authSvc *services.AuthService
	userSvc *services.UserService
}

func NewAuthMiddleware(authSvc *services.AuthService, userSvc *services.UserService) *AuthMiddleware {
	return &AuthMiddleware{authSvc: authSvc, userSvc: userSvc}
}

func (m *AuthMiddleware) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || !strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			c.AbortWithStatusJSON(401, gin.H{"error": "missing bearer token"})
			return
		}

		token := strings.TrimSpace(authHeader[len("Bearer "):])
		claims, err := m.authSvc.ParseToken(token)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid token"})
			return
		}

		user, err := m.userSvc.FindByID(claims.UserID)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "user not found"})
			return
		}
		if user.Role != models.UserRoleAdmin {
			if claims.DeviceID == "" || user.ActiveDeviceID == nil || *user.ActiveDeviceID != claims.DeviceID || user.SessionVersion != claims.SessionVersion {
				c.AbortWithStatusJSON(401, gin.H{"error": "session expired"})
				return
			}
		}

		c.Set(userContextKey, user)
		c.Set(authClaimsContextKey, claims)
		c.Next()
	}
}

func CurrentUser(c *gin.Context) *models.User {
	value, ok := c.Get(userContextKey)
	if !ok {
		return nil
	}
	user, _ := value.(*models.User)
	return user
}

func CurrentClaims(c *gin.Context) *services.SessionClaims {
	value, ok := c.Get(authClaimsContextKey)
	if !ok {
		return nil
	}
	claims, _ := value.(*services.SessionClaims)
	return claims
}
