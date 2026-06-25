package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/AiMarketool/f2v-promote/internal/center/weixin"
	"github.com/AiMarketool/f2v-promote/internal/config"
	"github.com/AiMarketool/f2v-promote/internal/pkg/feishu"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/AiMarketool/f2v-promote/internal/service"
	"github.com/aliyun/fc-runtime-go-sdk/events"
	fc "github.com/aliyun/fc-runtime-go-sdk/fc"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var autoStopSvc *service.AutoStopService

func initService() {
	cfg := config.Load()

	db, err := gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect to MySQL: %v", err)
	}

	orderRepo := repository.NewOrderRepo(db)
	campaignRepo := repository.NewCampaignRepo(db)
	promoteLogRepo := repository.NewAutoPromoteLogRepo(db)
	strategyRepo := repository.NewStrategyRepo(db)
	videoStatRepo := repository.NewVideoStatRepo(db)
	authorVideoRepo := repository.NewAuthorVideoRepo(db)
	authorRepo := repository.NewAuthorRepo(db)
	sheetTabRepo := repository.NewFeishuSheetTabRepo(db)
	accountRepo := repository.NewPlatformAccountRepo(db)
	zhugeTagRepo := repository.NewZhugeTagRepo(db)
	weixinClient := weixin.NewClient(cfg, accountRepo, zhugeTagRepo)

	notifier := service.NewNotifierService(cfg.WebhookURL, cfg.AppName)
	if cfg.FeishuAppID != "" && cfg.FeishuAppSecret != "" {
		feishuClient := feishu.NewClient(cfg.FeishuAppID, cfg.FeishuAppSecret)
		notifier.WithFeishu(feishuClient, cfg.FeishuChatID)
	}

	autoStopSvc = service.NewAutoStopService(
		promoteLogRepo, orderRepo, campaignRepo, strategyRepo, videoStatRepo, authorVideoRepo,
		authorRepo, sheetTabRepo, weixinClient, notifier, cfg,
	)
}

// initialize FC 初始化钩子
func initialize(ctx context.Context) {
	initService()
}

// preStop FC 预停止钩子
func preStop(ctx context.Context) {
}

// HandleRequest FC 定时触发器入口（每分钟调用一次）
func HandleRequest(ctx context.Context, event events.Data) (string, error) {
	if autoStopSvc == nil {
		initService()
	}
	return autoStopSvc.Run(), nil
}

func main() {
	if os.Getenv("FC_RUNTIME_API") == "" {
		fmt.Println("本地运行模式，开始自动关停评估...")
		initService()
		for {
			result := autoStopSvc.Run()
			fmt.Println(result)
			time.Sleep(1 * time.Minute)
		}
	} else {
		fc.RegisterInitializerFunction(initialize)
		fc.RegisterPreStopFunction(preStop)
		fc.Start(HandleRequest)
	}
}
