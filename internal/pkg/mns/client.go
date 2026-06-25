package mns

import (
	"encoding/base64"
	"fmt"

	ali_mns "github.com/aliyun/aliyun-mns-go-sdk"
)

// Client 阿里云 MNS 队列客户端（基于官方 SDK）
type Client struct {
	queue ali_mns.AliMNSQueue
}

// NewClient 创建 MNS 客户端
func NewClient(endpoint, accessKeyID, accessKeySecret, queueName string) *Client {
	mnsClient := ali_mns.NewAliMNSClientWithToken(endpoint, accessKeyID, accessKeySecret, "")
	queue := ali_mns.NewMNSQueue(queueName, mnsClient)
	return &Client{queue: queue}
}

// SendMessage 发送消息到队列，body 为原始 JSON 字节
func (c *Client) SendMessage(body []byte) (string, error) {
	msg := ali_mns.MessageSendRequest{
		MessageBody:  base64.StdEncoding.EncodeToString(body),
		DelaySeconds: 0,
		Priority:     8,
	}

	resp, err := c.queue.SendMessage(msg)
	if err != nil {
		return "", fmt.Errorf("mns: send message: %w", err)
	}
	return resp.MessageId, nil
}

// ReceivedMessage 接收到的消息
type ReceivedMessage struct {
	MessageID     string
	ReceiptHandle string
	Body          []byte // 已解码的消息体
}

// ReceiveMessage 长轮询拉取一条消息（waitSeconds: 0-30）
func (c *Client) ReceiveMessage(waitSeconds int64) (*ReceivedMessage, error) {
	respChan := make(chan ali_mns.MessageReceiveResponse, 1)
	errChan := make(chan error, 1)

	c.queue.ReceiveMessage(respChan, errChan, waitSeconds)

	select {
	case resp := <-respChan:
		body, err := base64.StdEncoding.DecodeString(resp.MessageBody)
		if err != nil {
			return nil, fmt.Errorf("mns: decode body: %w", err)
		}
		return &ReceivedMessage{
			MessageID:     resp.MessageId,
			ReceiptHandle: resp.ReceiptHandle,
			Body:          body,
		}, nil
	case err := <-errChan:
		return nil, err
	}
}

// DeleteMessage 消费确认（删除已处理的消息）
func (c *Client) DeleteMessage(receiptHandle string) error {
	return c.queue.DeleteMessage(receiptHandle)
}

