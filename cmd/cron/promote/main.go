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

var detectorSvc *service.PromoteDetectorService

func initService() {
	cfg := config.Load()

	db, err := gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect to MySQL: %v", err)
	}

	accountRepo := repository.NewPlatformAccountRepo(db)
	zhugeTagRepo := repository.NewZhugeTagRepo(db)
	authorRepo := repository.NewAuthorRepo(db)
	authorVideoRepo := repository.NewAuthorVideoRepo(db)
	videoStatRepo := repository.NewVideoStatRepo(db)
	strategyRepo := repository.NewStrategyRepo(db)
	promoteLogRepo := repository.NewAutoPromoteLogRepo(db)
	sheetTabRepo := repository.NewFeishuSheetTabRepo(db)

	weixinClient := weixin.NewClient(cfg, accountRepo, zhugeTagRepo)
	notifier := service.NewNotifierService(cfg.WebhookURL, cfg.AppName)
	if cfg.FeishuAppID != "" && cfg.FeishuAppSecret != "" {
		feishuClient := feishu.NewClient(cfg.FeishuAppID, cfg.FeishuAppSecret)
		notifier.WithFeishu(feishuClient, cfg.FeishuChatID)
	}

	detectorSvc = service.NewPromoteDetectorService(
		strategyRepo,
		authorVideoRepo,
		videoStatRepo,
		promoteLogRepo,
		authorRepo,
		sheetTabRepo,
		weixinClient,
		notifier,
		cfg,
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
	if detectorSvc == nil {
		initService()
	}
	return detectorSvc.Run(), nil
}

func main() {
	if os.Getenv("FC_RUNTIME_API") == "" {
		fmt.Println("本地运行模式，开始自动投放检测...")
		initService()
		for {
			result := detectorSvc.Run()
			fmt.Println(result)
			time.Sleep(1 * time.Minute)
		}
	} else {
		fc.RegisterInitializerFunction(initialize)
		fc.RegisterPreStopFunction(preStop)
		fc.Start(HandleRequest)
	}
}
