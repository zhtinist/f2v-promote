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

var syncSvc *service.StatsSyncService

func initService() {
	cfg := config.Load()

	db, err := gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect to MySQL: %v", err)
	}

	feishuClient := feishu.NewClient(cfg.FeishuAppID, cfg.FeishuAppSecret)

	authorRepo := repository.NewAuthorRepo(db)
	videoStatRepo := repository.NewVideoStatRepo(db)
	authorVideoRepo := repository.NewAuthorVideoRepo(db)

	weixinClient := weixin.NewClient(cfg, repository.NewPlatformAccountRepo(db), repository.NewZhugeTagRepo(db))
	matchService := service.NewAuthorVideoMatchService(authorVideoRepo, videoStatRepo, authorRepo, weixinClient)

	syncSvc = service.NewStatsSyncService(
		authorRepo,
		authorVideoRepo,
		videoStatRepo,
		repository.NewFeishuSyncCursorRepo(db),
		repository.NewFeishuSpreadsheetRepo(db),
		repository.NewFeishuFolderRepo(db),
		repository.NewFeishuSheetTabRepo(db),
		repository.NewStrategyRepo(db),
		feishuClient,
		matchService,
	)
}

// initialize FC 初始化钩子（实例启动时调用一次）
func initialize(ctx context.Context) {
	initService()
}

// preStop FC 预停止钩子（实例销毁前调用）
func preStop(ctx context.Context) {
}

// HandleRequest FC 定时触发器入口
func HandleRequest(ctx context.Context, event events.Data) (string, error) {
	if syncSvc == nil {
		initService()
	}
	return syncSvc.Run(), nil
}

func main() {
	if os.Getenv("FC_RUNTIME_API") == "" {
		fmt.Println("本地运行模式，开始同步 video_stats 到飞书...")
		initService()
		for {
			result := syncSvc.Run()
			fmt.Println(result)
			time.Sleep(30 * time.Minute)
		}
	} else {
		fc.RegisterInitializerFunction(initialize)
		fc.RegisterPreStopFunction(preStop)
		fc.Start(HandleRequest)
	}
}
