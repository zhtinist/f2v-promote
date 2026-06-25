package v1

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/AiMarketool/f2v-promote/internal/model"
	"github.com/AiMarketool/f2v-promote/internal/pkg/feishu"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/gin-gonic/gin"
)

// FeishuCallbackHandler 飞书卡片交互回调处理
type FeishuCallbackHandler struct {
	promoteLogRepo    *repository.AutoPromoteLogRepo
	authorRepo        *repository.AuthorRepo
	feishuClient      *feishu.Client
	chatID            string
	verificationToken string
}

func NewFeishuCallbackHandler(promoteLogRepo *repository.AutoPromoteLogRepo, authorRepo *repository.AuthorRepo, feishuClient *feishu.Client, chatID, verificationToken string) *FeishuCallbackHandler {
	return &FeishuCallbackHandler{
		promoteLogRepo:    promoteLogRepo,
		authorRepo:        authorRepo,
		feishuClient:      feishuClient,
		chatID:            chatID,
		verificationToken: verificationToken,
	}
}

// actionValue 按钮回调的 value 结构
type actionValue struct {
	Action string `json:"action"` // "confirm" / "reject"
	LogID  int64  `json:"log_id"` // auto_promote_logs.id
}

// parsedCallback 统一解析后的回调数据
type parsedCallback struct {
	Token       string          // 用于校验的 verification token
	ActionData  json.RawMessage // action.value
	OperatorID  string          // 操作人 open_id
	MessageID   string          // 消息 ID（用于幂等）
	IsV2        bool            // 是否 v2 格式
	IsChallenge bool
	Challenge   string
}

// HandleCardCallback 飞书卡片交互回调入口（支持 v1/v2 格式）
func (h *FeishuCallbackHandler) HandleCardCallback(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body failed"})
		return
	}

	fmt.Printf("service=feishu-callback action=receive body=%s\n", string(body))

	// 统一解析 v1/v2
	parsed, err := h.parseCallback(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	// URL 验证（首次配置回调地址时）
	if parsed.IsChallenge {
		c.JSON(http.StatusOK, gin.H{"challenge": parsed.Challenge})
		return
	}

	// 验证 token
	if !h.verifyToken(parsed.Token) {
		log.Printf("service=feishu-callback action=verify_failed got_token=%s", parsed.Token)
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid token"})
		return
	}

	// 解析按钮 value
	var val actionValue
	if err := json.Unmarshal(parsed.ActionData, &val); err != nil {
		log.Printf("service=feishu-callback action=parse_value error=%v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid action value"})
		return
	}

	log.Printf("service=feishu-callback action=%s log_id=%d operator=%s", val.Action, val.LogID, parsed.OperatorID)

	// 幂等检查
	promoteLog, _ := h.promoteLogRepo.GetByID(val.LogID)
	if promoteLog != nil && promoteLog.Status != model.PromoteLogDetected {
		c.JSON(http.StatusOK, gin.H{
			"toast": map[string]interface{}{
				"type":    "warning",
				"content": "该记录已被处理",
			},
		})
		return
	}

	// 执行操作
	var (
		ok    bool
		opErr error
	)
	switch val.Action {
	case "confirm":
		ok, opErr = h.promoteLogRepo.ConfirmByID(val.LogID)
	case "reject":
		ok, opErr = h.promoteLogRepo.RejectByID(val.LogID)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown action"})
		return
	}

	if opErr != nil {
		log.Printf("service=feishu-callback action=%s log_id=%d error=%v", val.Action, val.LogID, opErr)
		c.JSON(http.StatusOK, gin.H{
			"toast": map[string]interface{}{
				"type":    "error",
				"content": "操作失败，请重试",
			},
		})
		return
	}

	if !ok {
		c.JSON(http.StatusOK, gin.H{
			"toast": map[string]interface{}{
				"type":    "warning",
				"content": "该记录已被处理",
			},
		})
		return
	}

	// 发送新卡片记录操作结果（保留原卡片不覆盖）
	if h.feishuClient != nil && h.chatID != "" {
		operatorName := h.feishuClient.GetUserNameFromChat(h.chatID, parsed.OperatorID)
		now := time.Now().Format("2006-01-02 15:04:05")

		// 查询作者和视频描述
		authorName := ""
		description := ""
		if promoteLog != nil {
			if author, _ := h.authorRepo.GetByID(promoteLog.AuthorID); author != nil {
				authorName = author.Nickname
			}
			if len(promoteLog.VideoRawData) > 0 {
				var videoData map[string]any
				if json.Unmarshal(promoteLog.VideoRawData, &videoData) == nil {
					if desc, ok := videoData["description"].(string); ok {
						description = desc
					}
				}
			}
		}

		resultCard := feishu.BuildPromoteResultCard(val.Action, authorName, description, operatorName, now)
		if _, sendErr := h.feishuClient.SendCardToChat(h.chatID, resultCard); sendErr != nil {
			log.Printf("service=feishu-callback action=send_result_card error=%v", sendErr)
		}
	}

	// 返回 toast
	c.JSON(http.StatusOK, gin.H{
		"toast": map[string]interface{}{
			"type":    "success",
			"content": formatToast(val.Action),
		},
	})
}

// parseCallback 统一解析飞书 v1/v2 回调格式
func (h *FeishuCallbackHandler) parseCallback(body []byte) (*parsedCallback, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	// 检查是否为 URL 验证
	if _, ok := raw["type"]; ok {
		var challenge struct {
			Type      string `json:"type"`
			Challenge string `json:"challenge"`
			Token     string `json:"token"`
		}
		if json.Unmarshal(body, &challenge) == nil && challenge.Type == "url_verification" {
			return &parsedCallback{
				IsChallenge: true,
				Challenge:   challenge.Challenge,
				Token:       challenge.Token,
			}, nil
		}
	}

	// v2 格式：有 schema="2.0" 和 header/event 结构
	if _, ok := raw["schema"]; ok {
		var v2 struct {
			Header struct {
				Token string `json:"token"`
			} `json:"header"`
			Event struct {
				Action struct {
					Value json.RawMessage `json:"value"`
				} `json:"action"`
				Operator struct {
					OpenID string `json:"open_id"`
				} `json:"operator"`
				Context struct {
					OpenMessageID string `json:"open_message_id"`
				} `json:"context"`
			} `json:"event"`
		}
		if err := json.Unmarshal(body, &v2); err != nil {
			return nil, err
		}
		return &parsedCallback{
			Token:      v2.Header.Token,
			ActionData: v2.Event.Action.Value,
			OperatorID: v2.Event.Operator.OpenID,
			MessageID:  v2.Event.Context.OpenMessageID,
			IsV2:       true,
		}, nil
	}

	// v1 格式：action/token 在顶层
	var v1 struct {
		Token         string `json:"token"`
		OpenID        string `json:"open_id"`
		OpenMessageID string `json:"open_message_id"`
		Action        struct {
			Value json.RawMessage `json:"value"`
		} `json:"action"`
	}
	if err := json.Unmarshal(body, &v1); err != nil {
		return nil, err
	}
	return &parsedCallback{
		Token:      v1.Token,
		ActionData: v1.Action.Value,
		OperatorID: v1.OpenID,
		MessageID:  v1.OpenMessageID,
	}, nil
}

// verifyToken 校验飞书回调 token
func (h *FeishuCallbackHandler) verifyToken(token string) bool {
	if h.verificationToken == "" {
		return true // 未配置则跳过验证（开发环境）
	}
	// 飞书 v2 回调直接比对 verification_token
	if token == h.verificationToken {
		return true
	}
	// SHA1 签名比对（兼容）
	expected := fmt.Sprintf("%x", sha1.Sum([]byte(h.verificationToken)))
	return strings.EqualFold(token, expected)
}

func formatToast(action string) string {
	if action == "confirm" {
		return "✅ 已确认投放"
	}
	return "❌ 已拒绝投放"
}
