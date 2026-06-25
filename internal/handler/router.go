package handler

import (
	"io/fs"
	"net/http"

	"github.com/AiMarketool/f2v-promote/internal/config"
	v1 "github.com/AiMarketool/f2v-promote/internal/handler/v1"
	"github.com/AiMarketool/f2v-promote/internal/middleware"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/AiMarketool/f2v-promote/internal/service"
	"github.com/gin-gonic/gin"
)

// Router 持有所有 handler，负责路由注册
type Router struct {
	Auth           *v1.AuthHandler
	Page           *v1.PageHandler
	Zhuge          *v1.ZhugeHandler
	Order          *v1.OrderHandler
	Campaign       *v1.CampaignHandler
	Account        *v1.AccountHandler
	Dashboard      *v1.DashboardHandler
	Prompt         *v1.PromptHandler
	VideoStat      *v1.VideoStatHandler
	Strategy       *v1.StrategyHandler
	Tag            *v1.TagHandler
	AuthorVideo    *v1.AuthorVideoHandler
	FeishuCallback *v1.FeishuCallbackHandler

	AuthService *service.AuthService
	UserRepo    *repository.UserRepo
	Cfg         *config.Config
	StaticFS    fs.FS // embed.FS for static files
}

// RegisterRoutes 注册全部路由到 gin.Engine
func RegisterRoutes(engine *gin.Engine, r *Router) {
	authMW := middleware.AuthMiddleware(r.AuthService, r.UserRepo)

	// ── Basic Auth for HTML pages (optional, controlled by env) ──
	var basicAuthMW gin.HandlerFunc
	if r.Cfg != nil && r.Cfg.BasicAuthUser != "" && r.Cfg.BasicAuthPass != "" {
		basicAuthMW = gin.BasicAuth(gin.Accounts{
			r.Cfg.BasicAuthUser: r.Cfg.BasicAuthPass,
		})
	}

	// ── Public: 登录/注册 (also behind basic auth if enabled) ──
	loginGroup := engine.Group("/")
	if basicAuthMW != nil {
		loginGroup.Use(basicAuthMW)
	}
	{
		loginGroup.GET("/login", r.Auth.LoginPage)
		loginGroup.POST("/login", r.Auth.Login)
		loginGroup.POST("/register", r.Auth.Register)
		loginGroup.GET("/logout", r.Auth.Logout)
	}

	// ── 认证页面 (basic auth + session auth) ──
	pages := engine.Group("/")
	if basicAuthMW != nil {
		pages.Use(basicAuthMW)
	}
	pages.Use(authMW)
	{
		pages.GET("/", r.Page.Dashboard)
		pages.GET("/create", r.Page.CreatePage)
		pages.GET("/campaigns-page", r.Page.CampaignsPage)
		pages.GET("/orders-page", r.Page.OrdersPage)
		pages.GET("/accounts-page", r.Page.AccountsPage)
		pages.GET("/authors-page", r.Page.AuthorsPage)
		pages.GET("/strategies-page", r.Page.StrategiesPage)
		pages.GET("/promote-logs-page", r.Page.PromoteLogsPage)
		pages.GET("/prompts-page", r.Page.PromptsPage)
		// 新增页面路由
		pages.GET("/videos-page", r.Page.VideosPage)
		pages.GET("/video-stats-page", r.Page.VideoStatsPage)
		pages.GET("/tags-page", r.Page.TagsPage)
	}

	// ── Static files (behind basic auth if enabled, served from embed.FS) ──
	staticGroup := engine.Group("/static")
	if basicAuthMW != nil {
		staticGroup.Use(basicAuthMW)
	}
	staticSub, _ := fs.Sub(r.StaticFS, "static")
	staticGroup.GET("/*filepath", gin.WrapH(http.StripPrefix("/static", http.FileServer(http.FS(staticSub)))))

	// ── /zhuge API ──
	zhuge := engine.Group("/zhuge")
	zhuge.Use(authMW)
	{
		zhuge.GET("", r.Zhuge.ListCampaigns)
		zhuge.GET("/authors", r.Account.ListAllAuthors)
		zhuge.POST("/videos", r.Zhuge.GetVideos)
		zhuge.POST("/create", r.Zhuge.Create)
		zhuge.POST("/:campaign_id/confirm", r.Zhuge.Confirm)

		// 账号管理
		zhuge.GET("/accounts", r.Account.List)
		zhuge.POST("/accounts", r.Account.Create)
		zhuge.PUT("/accounts/:id", r.Account.Update)
		zhuge.DELETE("/accounts/:id", r.Account.Delete)
		zhuge.POST("/accounts/:id/refresh-authors", r.Account.RefreshAuthors)
		zhuge.GET("/accounts/:id/authors", r.Account.ListAuthors)

		// 订单级别
		zhuge.GET("/orders", r.Order.ListOrders)
		zhuge.GET("/orders/export", r.Order.ExportCSV)
		zhuge.POST("/order/:order_id/submit", r.Order.Submit)
		zhuge.GET("/order/:order_id/check-payment", r.Order.CheckPayment)
		zhuge.POST("/order/:order_id/update-status", r.Order.UpdateStatus)
		zhuge.GET("/order/:order_id/detail", r.Order.GetDetail)
		zhuge.GET("/order/:order_id/record", r.Order.GetRecord)
		zhuge.POST("/order/:order_id/stop", r.Order.Stop)
	}

	// ── /api/dashboard ──
	dashboard := engine.Group("/api/dashboard")
	dashboard.Use(authMW)
	{
		dashboard.GET("/stats", r.Dashboard.Stats)
		dashboard.GET("/overview", r.Dashboard.Overview)
		dashboard.GET("/recent-orders", r.Dashboard.RecentOrders)
	}

	// ── /campaigns API ──
	campaigns := engine.Group("/campaigns")
	campaigns.Use(authMW)
	{
		campaigns.GET("", r.Campaign.List)
		campaigns.GET("/export", r.Campaign.ExportCSV)
		campaigns.GET("/:id/orders", r.Campaign.ListOrders)
	}

	// ── /prompts API ──
	prompts := engine.Group("/prompts")
	prompts.Use(authMW)
	{
		prompts.GET("", r.Prompt.List)
		prompts.GET("/:name", r.Prompt.Get)
		prompts.POST("", r.Prompt.Save)
		prompts.DELETE("/:name", r.Prompt.Delete)
	}

	// ── /video-stats API ──
	vs := engine.Group("/video-stats")
	vs.Use(authMW)
	{
		vs.GET("", r.VideoStat.List)
		vs.GET("/:id", r.VideoStat.Get)
		vs.POST("", r.VideoStat.Create)
		vs.POST("/import", r.VideoStat.ImportCSV)
		vs.DELETE("/:id", r.VideoStat.Delete)
	}

	// ── /strategies API ──
	strategies := engine.Group("/strategies")
	strategies.Use(authMW)
	{
		strategies.GET("", r.Strategy.List)
		strategies.GET("/pending", r.Strategy.Pending)
		strategies.GET("/:id", r.Strategy.Get)
		strategies.POST("", r.Strategy.Create)
		strategies.PUT("/:id", r.Strategy.Update)
		strategies.DELETE("/:id", r.Strategy.Delete)
		strategies.GET("/:id/logs", r.Strategy.ListLogs)
		strategies.POST("/logs/:id/confirm", r.Strategy.Confirm)
		strategies.POST("/logs/:id/reject", r.Strategy.Reject)
	}

	// ── /promote-logs API ──
	promoteLogs := engine.Group("/promote-logs")
	promoteLogs.Use(authMW)
	{
		promoteLogs.GET("", r.Strategy.ListAllLogs)
		promoteLogs.PATCH("/:id/status", r.Strategy.UpdateLogStatus)
	}

	// ── /tags API ──
	tags := engine.Group("/tags")
	tags.Use(authMW)
	{
		tags.GET("", r.Tag.List)
		tags.GET("/tree", r.Tag.Tree)
		tags.POST("/sync", r.Tag.Sync)
	}

	// ── /author-videos API ──
	authorVideos := engine.Group("/author-videos")
	authorVideos.Use(authMW)
	{
		authorVideos.GET("", r.AuthorVideo.List)
		authorVideos.GET("/:id", r.AuthorVideo.Get)
		authorVideos.GET("/:id/stats", r.AuthorVideo.Stats)
	}

	// ── /api/public (no auth) ──
	pub := engine.Group("/api/public")
	{
		pub.POST("/video-stats", r.VideoStat.Create)
	}

	// ── /feishu 飞书回调（无业务 auth，走飞书 token 验证）──
	if r.FeishuCallback != nil {
		feishuGroup := engine.Group("/feishu")
		{
			feishuGroup.POST("/card/callback", r.FeishuCallback.HandleCardCallback)
		}
	}
}
