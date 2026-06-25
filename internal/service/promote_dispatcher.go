package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/AiMarketool/f2v-promote/internal/config"
	"github.com/AiMarketool/f2v-promote/internal/model"
	"github.com/AiMarketool/f2v-promote/internal/pkg/mns"
	"github.com/AiMarketool/f2v-promote/internal/repository"
)

const (
	dispatcherExecTimeout  = 58 * time.Second // 单次执行超时
	dispatcherPollInterval = 15 * time.Second // 空闲轮询间隔
	dispatcherBatchSize    = 50               // 每批扫描上限
	dispatcherCooldown     = 5 * time.Second  // 批次间冷却
)

// PromoteDispatcherService 推送服务（Dispatcher Cron 使用）
// 扫描 confirmed 状态 → CAS 推 MNS → 失败回退
type PromoteDispatcherService struct {
	promoteLogRepo *repository.AutoPromoteLogRepo
	mnsClient      *mns.Client
	cfg            *config.Config
}

func NewPromoteDispatcherService(
	promoteLogRepo *repository.AutoPromoteLogRepo,
	mnsClient *mns.Client,
	cfg *config.Config,
) *PromoteDispatcherService {
	return &PromoteDispatcherService{
		promoteLogRepo: promoteLogRepo,
		mnsClient:      mnsClient,
		cfg:            cfg,
	}
}

// Run Dispatcher 主入口（Cron 每分钟调一次）
func (s *PromoteDispatcherService) Run() string {
	ctx, cancel := context.WithTimeout(context.Background(), dispatcherExecTimeout)
	defer cancel()

	totalDispatched := 0

	for {
		select {
		case <-ctx.Done():
			return s.summary(totalDispatched)
		default:
		}

		// 1. 查所有 status=confirmed 的 log（限制批次大小）
		logs, err := s.promoteLogRepo.ListConfirmed(dispatcherBatchSize)
		if err != nil {
			log.Printf("service=promote-dispatcher action=list_confirmed error=%v", err)
			select {
			case <-ctx.Done():
				return s.summary(totalDispatched)
			case <-time.After(dispatcherPollInterval):
				continue
			}
		}

		if len(logs) == 0 {
			select {
			case <-ctx.Done():
				return s.summary(totalDispatched)
			case <-time.After(dispatcherPollInterval):
				continue
			}
		}

		for _, promoteLog := range logs {
			select {
			case <-ctx.Done():
				return s.summary(totalDispatched)
			default:
			}

			// 2. CAS 原子更新 confirmed → queued + set queued_at
			affected, err := s.promoteLogRepo.MarkAsQueued(promoteLog.ID)
			if err != nil || affected == 0 {
				continue // 已被其他实例处理或状态已变
			}

			// 3. 获取更新后的 log（含 queued_at）
			updatedLog, _ := s.promoteLogRepo.GetByID(promoteLog.ID)
			queuedAt := time.Now()
			if updatedLog != nil && updatedLog.QueuedAt != nil {
				queuedAt = *updatedLog.QueuedAt
			}

			// 4. 推 MNS 消息 {promote_log_id, queued_at}
			msgBody, _ := json.Marshal(mns.PromoteMessage{
				PromoteLogID: promoteLog.ID,
				QueuedAt:     queuedAt,
			})
			if _, err := s.mnsClient.SendMessage(msgBody); err != nil {
				log.Printf("service=promote-dispatcher action=send_mns log_id=%d error=%v", promoteLog.ID, err)
				// 推送失败 → 回退 queued → confirmed（下次重试）
				_ = s.promoteLogRepo.UpdateStatus(promoteLog.ID, model.PromoteLogConfirmed, map[string]any{
					"queued_at": nil,
				})
				continue
			}

			totalDispatched++
			log.Printf("service=promote-dispatcher action=dispatched log_id=%d queued_at=%v", promoteLog.ID, queuedAt)
		}

		select {
		case <-ctx.Done():
			return s.summary(totalDispatched)
		case <-time.After(dispatcherCooldown):
		}
	}
}

// summary 生成执行摘要
func (s *PromoteDispatcherService) summary(dispatched int) string {
	msg := fmt.Sprintf(`{"dispatched":%d}`, dispatched)
	log.Printf("service=promote-dispatcher action=summary %s", msg)
	return msg
}
