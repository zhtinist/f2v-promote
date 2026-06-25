package mns

import (
	"encoding/json"
	"time"
)

// PromoteMessage MNS 推送的消息体
// Dispatcher 推送时填充 QueuedAt，Consumer 校验一致性
type PromoteMessage struct {
	PromoteLogID int64     `json:"promote_log_id"` // auto_promote_logs.id
	QueuedAt     time.Time `json:"queued_at"`       // 推入 MNS 的时间戳，用于幂等校验
}

// Encode 序列化为 JSON 字节
func (m PromoteMessage) Encode() ([]byte, error) {
	return json.Marshal(m)
}

// DecodePromoteMessage 从 JSON 字节反序列化
func DecodePromoteMessage(data []byte) (PromoteMessage, error) {
	var msg PromoteMessage
	err := json.Unmarshal(data, &msg)
	return msg, err
}
