package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/AiMarketool/f2v-promote/internal/pkg/feishu"
)

// NotifierService sends webhook notifications and Feishu interactive cards.
type NotifierService struct {
	webhookURL   string
	appName      string
	client       *http.Client
	feishuClient *feishu.Client // 飞书应用客户端（可选，用于发送交互卡片）
	feishuChatID string        // 飞书群组 ID
}

// NewNotifierService creates a new NotifierService.
func NewNotifierService(webhookURL, appName string) *NotifierService {
	return &NotifierService{
		webhookURL: webhookURL,
		appName:    appName,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

// WithFeishu 注入飞书应用客户端，启用交互卡片能力
func (n *NotifierService) WithFeishu(feishuClient *feishu.Client, chatID string) *NotifierService {
	n.feishuClient = feishuClient
	n.feishuChatID = chatID
	return n
}

// SendPromoteCard 发送投放审核交互卡片到飞书群（优先使用飞书应用 API）
func (n *NotifierService) SendPromoteCard(info feishu.PromoteCardInfo) error {
	// 优先飞书应用 API（支持按钮回调）
	if n.feishuClient != nil && n.feishuChatID != "" {
		card := feishu.BuildPromoteApprovalCard(info)
		_, err := n.feishuClient.SendCardToChat(n.feishuChatID, card)
		if err != nil {
			log.Printf("service=notifier action=send_card error=%v fallback=webhook", err)
			// 降级为 webhook 纯文本
			return n.Send(fmt.Sprintf("🎯 自动投放检测命中\n作者: %s\n视频: %s\n类型: %s\n播放增量: %d/h\n请前往管理页面确认投放",
				info.AuthorName, info.Description, info.PromoteType, info.HourlyPlay))
		}
		return nil
	}

	// 降级为 webhook 纯文本推送
	return n.Send(fmt.Sprintf("🎯 自动投放检测命中\n作者: %s\n视频: %s\n类型: %s\n播放增量: %d/h\n请前往管理页面确认投放",
		info.AuthorName, info.Description, info.PromoteType, info.HourlyPlay))
}

// SendPromoteInfoCard 发送自动投放通知卡片（无按钮，仅告知已自动投放）
func (n *NotifierService) SendPromoteInfoCard(info feishu.PromoteCardInfo) error {
	if n.feishuClient != nil && n.feishuChatID != "" {
		card := feishu.BuildPromoteInfoCard(info)
		_, err := n.feishuClient.SendCardToChat(n.feishuChatID, card)
		if err != nil {
			log.Printf("service=notifier action=send_info_card error=%v fallback=webhook", err)
			return n.Send(fmt.Sprintf("🚀 已自动投放\n作者: %s\n视频: %s\n类型: %s\n播放增量: %d/h",
				info.AuthorName, info.Description, info.PromoteType, info.HourlyPlay))
		}
		return nil
	}

	return n.Send(fmt.Sprintf("🚀 已自动投放\n作者: %s\n视频: %s\n类型: %s\n播放增量: %d/h",
		info.AuthorName, info.Description, info.PromoteType, info.HourlyPlay))
}

// SendStopCard 发送自动关停通知卡片到飞书群
func (n *NotifierService) SendStopCard(info feishu.StopCardInfo) error {
	if n.feishuClient != nil && n.feishuChatID != "" {
		card := feishu.BuildPromoteStopCard(info)
		_, err := n.feishuClient.SendCardToChat(n.feishuChatID, card)
		if err != nil {
			log.Printf("service=notifier action=send_stop_card error=%v fallback=webhook", err)
			return n.Send(fmt.Sprintf("⛔ 自动关停投放\n作者: %s\n视频: %s\n原因: %s\n订单: %d",
				info.AuthorName, info.Description, info.Reason, info.OrderID))
		}
		return nil
	}

	return n.Send(fmt.Sprintf("⛔ 自动关停投放\n作者: %s\n视频: %s\n原因: %s\n订单: %d",
		info.AuthorName, info.Description, info.Reason, info.OrderID))
}

// Send posts a notification message to the configured webhook.
// It auto-detects the platform (feishu, dingtalk, wechat) by the URL.
func (n *NotifierService) Send(message string) error {
	if n.webhookURL == "" {
		log.Println("service=notifier action=send result=skip reason=no_webhook_url")
		return nil
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fullMsg := fmt.Sprintf("[%s] %s\n%s", n.appName, timestamp, message)

	var payload map[string]interface{}

	switch {
	case strings.Contains(n.webhookURL, "feishu"):
		payload = map[string]interface{}{
			"msg_type": "text",
			"content": map[string]string{
				"text": fullMsg,
			},
		}
	case strings.Contains(n.webhookURL, "dingtalk") || strings.Contains(n.webhookURL, "oapi.dingtalk.com"):
		payload = map[string]interface{}{
			"msgtype": "text",
			"text": map[string]string{
				"content": fullMsg,
			},
		}
	case strings.Contains(n.webhookURL, "qyapi.weixin.qq.com"):
		payload = map[string]interface{}{
			"msgtype": "text",
			"text": map[string]string{
				"content": fullMsg,
			},
		}
	default:
		// Generic fallback.
		payload = map[string]interface{}{
			"text": fullMsg,
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("notifier: marshal: %w", err)
	}

	resp, err := n.client.Post(n.webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notifier: send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notifier: webhook returned status %d", resp.StatusCode)
	}

	return nil
}
