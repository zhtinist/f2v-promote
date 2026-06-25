package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/AiMarketool/f2v-promote/internal/center/weixin"
	"github.com/AiMarketool/f2v-promote/internal/config"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/AiMarketool/f2v-promote/internal/service"
	"github.com/aliyun/fc-runtime-go-sdk/events"
	fc "github.com/aliyun/fc-runtime-go-sdk/fc"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// StructEvent 阿里云函数计算定时触发器事件
type StructEvent struct {
	TriggerName string `json:"triggerName"`
	TriggerTime string `json:"triggerTime"`
	Payload     string `json:"payload"`
}

var checkOrderSvc *service.CheckOrderService

func initService() {
	cfg := config.Load()

	db, err := gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect to MySQL: %v", err)
	}

	orderRepo := repository.NewOrderRepo(db)
	campaignRepo := repository.NewCampaignRepo(db)
	accountRepo := repository.NewPlatformAccountRepo(db)
	zhugeTagRepo := repository.NewZhugeTagRepo(db)
	promoteLogRepo := repository.NewAutoPromoteLogRepo(db)
	weixinClient := weixin.NewClient(cfg, accountRepo, zhugeTagRepo)

	checkOrderSvc = service.NewCheckOrderService(orderRepo, campaignRepo, promoteLogRepo, weixinClient, cfg)
}

// initialize 初始化
func initialize(ctx context.Context) {
	initService()
}

// preStop 关闭db
func preStop(ctx context.Context) {
}

func HandleRequest(ctx context.Context, event events.Data) (string, error) {
	if checkOrderSvc == nil {
		initService()
	}

	return checkOrderSvc.Run(), nil
}

func main() {
	if os.Getenv("FC_RUNTIME_API") == "" {
		// 本地运行模式
		fmt.Println("本地运行模式，开始轮询订单...")
		initService()
		for {
			result := checkOrderSvc.Run()
			fmt.Println(result)
			time.Sleep(5 * time.Second)
		}
	} else {
		fc.RegisterInitializerFunction(initialize)
		fc.RegisterPreStopFunction(preStop)
		fc.Start(HandleRequest)
	}
}
