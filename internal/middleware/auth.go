package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/models"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/services"
)

const userContextKey = "auth_user"

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
		userID, err := m.authSvc.ParseToken(token)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid token"})
			return
		}

		user, err := m.userSvc.FindByID(userID)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "user not found"})
			return
		}

		c.Set(userContextKey, user)
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
