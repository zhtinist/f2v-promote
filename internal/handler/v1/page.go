package v1

import (
	"net/http"

	"github.com/AiMarketool/f2v-promote/internal/middleware"
	"github.com/AiMarketool/f2v-promote/internal/pkg"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/gin-gonic/gin"
)

type PageHandler struct {
	userRepo *repository.UserRepo
}

func NewPageHandler(userRepo *repository.UserRepo) *PageHandler {
	return &PageHandler{userRepo: userRepo}
}

func (h *PageHandler) pageData(c *gin.Context, group, page string) (*pkg.PageData, bool) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.Redirect(http.StatusFound, "/login")
		return nil, false
	}
	return &pkg.PageData{
		User:        &pkg.UserInfo{ID: user.ID, Username: user.Username},
		ActivePage:  page,
		ActiveGroup: group,
		Threshold:   user.CostThreshold,
	}, true
}

func (h *PageHandler) Dashboard(c *gin.Context) {
	data, ok := h.pageData(c, "dashboard", "dashboard")
	if !ok {
		return
	}
	pkg.Render(c, http.StatusOK, "dashboard.html", *data)
}

func (h *PageHandler) CreatePage(c *gin.Context) {
	data, ok := h.pageData(c, "promote", "create")
	if !ok {
		return
	}
	pkg.Render(c, http.StatusOK, "create.html", *data)
}

func (h *PageHandler) CampaignsPage(c *gin.Context) {
	data, ok := h.pageData(c, "promote", "campaigns")
	if !ok {
		return
	}
	pkg.Render(c, http.StatusOK, "campaigns.html", *data)
}

func (h *PageHandler) OrdersPage(c *gin.Context) {
	data, ok := h.pageData(c, "promote", "orders")
	if !ok {
		return
	}
	pkg.Render(c, http.StatusOK, "orders.html", *data)
}

func (h *PageHandler) AccountsPage(c *gin.Context) {
	data, ok := h.pageData(c, "platform", "accounts")
	if !ok {
		return
	}
	pkg.Render(c, http.StatusOK, "accounts.html", *data)
}

func (h *PageHandler) AuthorsPage(c *gin.Context) {
	data, ok := h.pageData(c, "author", "authors")
	if !ok {
		return
	}
	pkg.Render(c, http.StatusOK, "authors.html", *data)
}

func (h *PageHandler) PromptsPage(c *gin.Context) {
	data, ok := h.pageData(c, "settings", "prompts")
	if !ok {
		return
	}
	pkg.Render(c, http.StatusOK, "settings.html", *data)
}

func (h *PageHandler) StrategiesPage(c *gin.Context) {
	data, ok := h.pageData(c, "author", "strategies")
	if !ok {
		return
	}
	pkg.Render(c, http.StatusOK, "strategies.html", *data)
}

func (h *PageHandler) PromoteLogsPage(c *gin.Context) {
	data, ok := h.pageData(c, "promote", "promote-logs")
	if !ok {
		return
	}
	pkg.Render(c, http.StatusOK, "promote_logs.html", *data)
}

func (h *PageHandler) VideosPage(c *gin.Context) {
	data, ok := h.pageData(c, "author", "videos")
	if !ok {
		return
	}
	pkg.Render(c, http.StatusOK, "videos.html", *data)
}

func (h *PageHandler) VideoStatsPage(c *gin.Context) {
	data, ok := h.pageData(c, "author", "video-stats")
	if !ok {
		return
	}
	pkg.Render(c, http.StatusOK, "video_stats.html", *data)
}

func (h *PageHandler) TagsPage(c *gin.Context) {
	data, ok := h.pageData(c, "tags", "tags")
	if !ok {
		return
	}
	pkg.Render(c, http.StatusOK, "tags.html", *data)
}
