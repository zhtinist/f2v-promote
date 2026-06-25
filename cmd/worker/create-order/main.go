package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/AiMarketool/f2v-promote/internal/center/weixin"
	"github.com/AiMarketool/f2v-promote/internal/config"
	"github.com/AiMarketool/f2v-promote/internal/pkg/mns"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/AiMarketool/f2v-promote/internal/service"
	fc "github.com/aliyun/fc-runtime-go-sdk/fc"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var (
	executorSvc *service.PromoteExecutorService
	mnsClient   *mns.Client
)

func initService() {
	cfg := config.Load()

	db, err := gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect to MySQL: %v", err)
	}

	accountRepo := repository.NewPlatformAccountRepo(db)
	zhugeTagRepo := repository.NewZhugeTagRepo(db)
	authorRepo := repository.NewAuthorRepo(db)
	campaignRepo := repository.NewCampaignRepo(db)
	orderRepo := repository.NewOrderRepo(db)
	promptRepo := repository.NewPromptRepo(db)
	promoteLogRepo := repository.NewAutoPromoteLogRepo(db)

	weixinClient := weixin.NewClient(cfg, accountRepo, zhugeTagRepo)
	openAIService := service.NewOpenAIService(cfg, promptRepo, zhugeTagRepo)

	executorSvc = service.NewPromoteExecutorService(
		promoteLogRepo,
		campaignRepo,
		orderRepo,
		authorRepo,
		weixinClient,
		openAIService,
		cfg,
	)

	mnsClient = mns.NewClient(cfg.MNSEndpoint, cfg.MNSAccessKeyID, cfg.MNSAccessKeySecret, cfg.MNSQueueName)
}

// ── FC 模式 ──

// MNSEvent MNS 触发器事件结构
type MNSEvent struct {
	Context     *MNSContext `json:"context"`
	MessageBody string      `json:"messageBody"`
	MessageID   string      `json:"messageId"`
}

type MNSContext struct {
	AccountID string `json:"accountId"`
	QueueName string `json:"queueName"`
	Region    string `json:"region"`
}

func initialize(ctx context.Context) { initService() }
func preStop(ctx context.Context)    {}

// HandleRequest FC MNS 触发器入口
func HandleRequest(ctx context.Context, event json.RawMessage) (string, error) {
	if executorSvc == nil {
		initService()
	}

	var mnsEvent []MNSEvent
	if err := json.Unmarshal(event, &mnsEvent); err != nil {
		return "", fmt.Errorf("unmarshal mns event: %w", err)
	}

	for _, mnsEvent := range mnsEvent {
		bodyBytes, err := base64.StdEncoding.DecodeString(mnsEvent.MessageBody)
		if err != nil {
			return "", fmt.Errorf("decode message body: %w", err)
		}

		msg, err := mns.DecodePromoteMessage(bodyBytes)
		if err != nil {
			return "", fmt.Errorf("decode promote message: %w", err)
		}

		log.Printf("service=mns-consumer action=received promote_log_id=%d", msg.PromoteLogID)

		if err := executorSvc.Execute(ctx, msg); err != nil {
			log.Printf("service=mns-consumer action=execute error=%v", err)
			return "", err
		}

		log.Printf("service=mns-consumer action=success promote_log_id=%d", msg.PromoteLogID)

	}
	return "ok", nil
}

// ── 本地轮询模式 ──

// runPolling 本地长轮询消费 MNS 消息
func runPolling() {
	log.Println("service=mns-consumer mode=polling 开始轮询消费 MNS 消息...")

	ctx := context.Background()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-quit:
			log.Println("service=mns-consumer action=shutdown")
			return
		default:
		}

		// 长轮询拉取（等待最多 10 秒）
		received, err := mnsClient.ReceiveMessage(10)
		if err != nil {
			// MessageNotExist = 队列为空，正常继续
			if isQueueEmpty(err) {
				continue
			}
			log.Printf("service=mns-consumer action=receive error=%v", err)
			time.Sleep(3 * time.Second)
			continue
		}

		msg, err := mns.DecodePromoteMessage(received.Body)
		if err != nil {
			log.Printf("service=mns-consumer action=decode error=%v message_id=%s", err, received.MessageID)
			// 解码失败也要删除消息，避免死循环
			_ = mnsClient.DeleteMessage(received.ReceiptHandle)
			continue
		}

		log.Printf("service=mns-consumer action=received promote_log_id=%d message_id=%s", msg.PromoteLogID, received.MessageID)

		if err := executorSvc.Execute(ctx, msg); err != nil {
			log.Printf("service=mns-consumer action=execute error=%v promote_log_id=%d", err, msg.PromoteLogID)
			// 执行失败不删除消息，等待 MNS 可见性超时后重投
			continue
		}

		// 执行成功 → 删除消息（消费确认）
		if err := mnsClient.DeleteMessage(received.ReceiptHandle); err != nil {
			log.Printf("service=mns-consumer action=delete_msg error=%v message_id=%s", err, received.MessageID)
		}
	}
}

// isQueueEmpty 判断是否为队列空错误
func isQueueEmpty(err error) bool {
	return strings.Contains(err.Error(), "MessageNotExist")
}

func main() {
	if os.Getenv("FC_RUNTIME_API") == "" {
		// 本地模式：轮询消费
		initService()
		runPolling()
	} else {
		// FC 模式：MNS 触发器
		fc.RegisterInitializerFunction(initialize)
		fc.RegisterPreStopFunction(preStop)
		fc.Start(HandleRequest)
	}
}
