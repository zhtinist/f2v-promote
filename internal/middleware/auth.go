package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/AiMarketool/f2v-promote/internal/model"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/AiMarketool/f2v-promote/internal/service"
	"github.com/gin-gonic/gin"
)

const sessionCookieName = "session_token"

// AuthMiddleware reads the session_token cookie, decodes it, loads the user,
// and stores both in the Gin context.
//
// For HTML page requests (Accept contains "text/html") failures redirect to /login.
// For API requests failures return 401 JSON.
func AuthMiddleware(authService *service.AuthService, userRepo *repository.UserRepo) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(sessionCookieName)
		if err != nil || token == "" {
			handleUnauth(c)
			return
		}

		userIDStr, err := authService.DecodeSessionToken(token)
		if err != nil {
			handleUnauth(c)
			return
		}

		userID, _ := strconv.Atoi(userIDStr)

		user, err := userRepo.GetByID(int64(userID))
		if err != nil || user == nil {
			handleUnauth(c)
			return
		}

		c.Set("user", user)
		c.Set("session_token", token)
		c.Next()
	}
}

// RequireAuth is a lightweight middleware that returns 401 if no user is
// present in the context. It is intended for routes that are already behind
// AuthMiddleware but need an explicit guard.
func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if GetCurrentUser(c) == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
			return
		}
		c.Next()
	}
}

// GetCurrentUser extracts the *model.User stored by AuthMiddleware.
// Returns nil when no user is present.
func GetCurrentUser(c *gin.Context) *model.User {
	val, exists := c.Get("user")
	if !exists {
		return nil
	}
	user, ok := val.(*model.User)
	if !ok {
		return nil
	}
	return user
}

// handleUnauth aborts the request with a redirect (HTML) or 401 JSON (API).
func handleUnauth(c *gin.Context) {
	if isHTMLRequest(c) {
		c.Redirect(http.StatusFound, "/login")
		c.Abort()
		return
	}
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
}

// isHTMLRequest returns true when the client is likely a browser requesting a page.
func isHTMLRequest(c *gin.Context) bool {
	accept := c.GetHeader("Accept")
	// Check prefix of path as a fallback: page routes never start with /api or /zhuge etc.
	if strings.Contains(accept, "text/html") {
		return true
	}
	// Heuristic: non-API paths are page requests.
	path := c.Request.URL.Path
	if !strings.HasPrefix(path, "/zhuge") &&
		!strings.HasPrefix(path, "/campaigns") &&
		!strings.HasPrefix(path, "/prompts") &&
		!strings.HasPrefix(path, "/api") {
		return true
	}
	return false
}
