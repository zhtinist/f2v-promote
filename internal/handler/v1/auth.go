package v1

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/AiMarketool/f2v-promote/internal/config"
	"github.com/AiMarketool/f2v-promote/internal/pkg"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/AiMarketool/f2v-promote/internal/service"
	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	authService *service.AuthService
	userRepo    *repository.UserRepo
	cfg         *config.Config
}

func NewAuthHandler(authService *service.AuthService, userRepo *repository.UserRepo, cfg *config.Config) *AuthHandler {
	return &AuthHandler{authService: authService, userRepo: userRepo, cfg: cfg}
}

func (h *AuthHandler) renderLogin(c *gin.Context, errMsg string) {
	pkg.Render(c, http.StatusOK, "login.html", pkg.PageData{Error: errMsg})
}

func (h *AuthHandler) LoginPage(c *gin.Context) {
	h.renderLogin(c, "")
}

func (h *AuthHandler) Login(c *gin.Context) {
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")

	if username == "" || password == "" {
		h.renderLogin(c, "请填写用户名和密码")
		return
	}

	user, err := h.userRepo.GetByUsername(username)
	if err != nil {
		log.Printf("service=auth action=login error=%v", err)
		h.renderLogin(c, "服务器错误")
		return
	}
	if user == nil || !service.VerifyPassword(password, user.HashedPassword) {
		h.renderLogin(c, "用户名或密码错误")
		return
	}

	token, err := h.authService.CreateSessionToken(user.ID)
	if err != nil {
		log.Printf("service=auth action=create_token error=%v", err)
		h.renderLogin(c, "服务器错误")
		return
	}

	c.SetCookie("session_token", token, 24*3600, "/", "", false, true)
	c.Redirect(http.StatusSeeOther, "/")
}

func (h *AuthHandler) Register(c *gin.Context) {
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")
	adminToken := c.PostForm("admin_token")

	renderErr := func(msg string) {
		h.renderLogin(c, msg)
	}

	if adminToken != h.cfg.AdminToken {
		renderErr("注册令牌无效")
		return
	}
	if username == "" || password == "" {
		renderErr("请填写用户名和密码")
		return
	}
	if len(password) < 6 {
		renderErr("密码至少 6 位")
		return
	}

	count, err := h.userRepo.Count()
	if err != nil {
		renderErr("服务器错误")
		return
	}
	if int(count) >= h.cfg.MaxUsers {
		renderErr(fmt.Sprintf("已达最大用户数 (%d)", h.cfg.MaxUsers))
		return
	}

	existing, err := h.userRepo.GetByUsername(username)
	if err != nil {
		renderErr("服务器错误")
		return
	}
	if existing != nil {
		renderErr("用户名已存在")
		return
	}

	hashed, err := service.HashPassword(password)
	if err != nil {
		renderErr("服务器错误")
		return
	}

	user, err := h.userRepo.Create(username, hashed)
	if err != nil {
		renderErr("服务器错误")
		return
	}

	token, err := h.authService.CreateSessionToken(user.ID)
	if err != nil {
		renderErr("服务器错误")
		return
	}

	c.SetCookie("session_token", token, 24*3600, "/", "", false, true)
	c.Redirect(http.StatusSeeOther, "/")
}

func (h *AuthHandler) Logout(c *gin.Context) {
	c.SetCookie("session_token", "", -1, "/", "", false, true)
	c.Redirect(http.StatusSeeOther, "/login")
}
