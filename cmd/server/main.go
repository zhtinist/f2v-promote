package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	promote "github.com/AiMarketool/f2v-promote"
	"github.com/AiMarketool/f2v-promote/internal/center/weixin"
	"github.com/AiMarketool/f2v-promote/internal/config"
	"github.com/AiMarketool/f2v-promote/internal/handler"
	v1 "github.com/AiMarketool/f2v-promote/internal/handler/v1"
	"github.com/AiMarketool/f2v-promote/internal/model"
	"github.com/AiMarketool/f2v-promote/internal/pkg"
	"github.com/AiMarketool/f2v-promote/internal/pkg/feishu"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/AiMarketool/f2v-promote/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func main() {
	// ── Config ──
	cfg := config.Load()

	if !cfg.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	// ── Database ──
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                      cfg.DSN(),
		DisableDatetimePrecision: true,
	}))
	if err != nil {
		log.Fatalf("failed to connect to MySQL: %v", err)
	}

	// Auto-migrate tables.
	if err := db.AutoMigrate(
		&model.User{},
		&model.Campaign{},
		&model.Order{},
		&model.PromptTemplate{},
		&model.PlatformAccount{},
		&model.Author{},
		&model.ZhugeTag{},
		&model.PerformanceLog{},
		&model.AuditLog{},
		&model.VideoStat{},
		&model.FeishuSpreadsheet{},
		&model.FeishuSyncCursor{},
		&model.FeishuFolder{},
		&model.AuthorVideo{},
		&model.FeishuSheetTab{},
		&model.AuthorPromoteStrategy{},
		&model.AutoPromoteLog{},
	); err != nil {
		log.Fatalf("auto-migrate failed: %v", err)
	}

	// ── Repositories ──
	userRepo := repository.NewUserRepo(db)
	campaignRepo := repository.NewCampaignRepo(db)
	orderRepo := repository.NewOrderRepo(db)
	promptRepo := repository.NewPromptRepo(db)
	auditRepo := repository.NewAuditRepo(db)
	accountRepo := repository.NewPlatformAccountRepo(db)
	authorRepo := repository.NewAuthorRepo(db)
	zhugeTagRepo := repository.NewZhugeTagRepo(db)
	videoStatRepo := repository.NewVideoStatRepo(db)
	strategyRepo := repository.NewStrategyRepo(db)
	promoteLogRepo := repository.NewAutoPromoteLogRepo(db)
	authorVideoRepo := repository.NewAuthorVideoRepo(db)

	// ── Services ──
	authService := service.NewAuthService(cfg.SessionSecret)
	weixinClient := weixin.NewClient(cfg, accountRepo, zhugeTagRepo)
	openAIService := service.NewOpenAIService(cfg, promptRepo, zhugeTagRepo)
	notifier := service.NewNotifierService(cfg.WebhookURL, cfg.AppName)

	// 飞书应用客户端（交互卡片 + 回调）
	var feishuClient *feishu.Client
	var feishuCallbackHandler *v1.FeishuCallbackHandler
	if cfg.FeishuAppID != "" && cfg.FeishuAppSecret != "" {
		feishuClient = feishu.NewClient(cfg.FeishuAppID, cfg.FeishuAppSecret)
		notifier.WithFeishu(feishuClient, cfg.FeishuChatID)
		feishuCallbackHandler = v1.NewFeishuCallbackHandler(promoteLogRepo, authorRepo, feishuClient, cfg.FeishuChatID, cfg.FeishuVerificationToken)
	}

	campaignService := service.NewCampaignService(
		campaignRepo, orderRepo, auditRepo, promoteLogRepo,
		weixinClient, openAIService, notifier, cfg,
	)

	// ── Gin Engine ──
	engine := gin.Default()
	pkg.SetupTemplates(engine, promote.TemplatesFS)

	// ── Register Routes ──
	r := &handler.Router{
		Auth:           v1.NewAuthHandler(authService, userRepo, cfg),
		Page:           v1.NewPageHandler(userRepo),
		Zhuge:          v1.NewZhugeHandler(campaignService, weixinClient),
		Order:          v1.NewOrderHandler(campaignService, orderRepo, campaignRepo, promoteLogRepo),
		Campaign:       v1.NewCampaignHandler(campaignRepo, orderRepo, promoteLogRepo),
		Account:        v1.NewAccountHandler(weixinClient, accountRepo, authorRepo),
		Dashboard:      v1.NewDashboardHandler(campaignRepo, orderRepo, authorRepo, authorVideoRepo, videoStatRepo, strategyRepo, promoteLogRepo),
		Prompt:         v1.NewPromptHandler(promptRepo),
		VideoStat:      v1.NewVideoStatHandler(videoStatRepo),
		Strategy:       v1.NewStrategyHandler(strategyRepo, promoteLogRepo, authorRepo, authorVideoRepo),
		Tag:            v1.NewTagHandler(zhugeTagRepo),
		AuthorVideo:    v1.NewAuthorVideoHandler(authorVideoRepo, videoStatRepo, authorRepo),
		FeishuCallback: feishuCallbackHandler,
		AuthService:    authService,
		UserRepo:       userRepo,
		Cfg:            cfg,
		StaticFS:       promote.StaticFS,
	}
	handler.RegisterRoutes(engine, r)

	// ── HTTP Server with graceful shutdown ──
	srv := &http.Server{
		Addr:    config.Load().AppPort,
		Handler: engine,
	}

	go func() {
		log.Printf("service=%s action=start addr=%s", cfg.AppName, srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Printf("service=%s action=shutdown", cfg.AppName)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}

	log.Printf("service=%s action=stopped", cfg.AppName)
}
