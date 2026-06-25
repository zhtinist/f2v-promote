package feishu

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// shortTime 从 "2026-04-01 10:47:16" 提取 "10:47"
func shortTime(datetime string) string {
	parts := strings.Split(datetime, " ")
	if len(parts) >= 2 {
		timeParts := strings.Split(parts[1], ":")
		if len(timeParts) >= 2 {
			return timeParts[0] + ":" + timeParts[1]
		}
		return parts[1]
	}
	return datetime
}

// truncateRunes 按 rune 截断字符串，超出追加 "..."
func truncateRunes(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen]) + "..."
}

// fmtDelta 格式化增量：>0 显示 "🔺+N"，<0 显示 "🔻N"，=0 显示 "-"
func fmtDelta(d int64) string {
	if d > 0 {
		return fmt.Sprintf("🔺+%d", d)
	}
	if d < 0 {
		return fmt.Sprintf("🔻%d", d)
	}
	return "-"
}

// PromoteCardInfo 投放审核卡片所需的业务数据
type PromoteCardInfo struct {
	LogID       int64
	AuthorName  string // 作者名称
	Description string // 视频描述
	PromoteType string // like_rate / followers

	// 计算后的指标
	HourlyPlay int
	LikeRate   float64
	ShareRate  float64

	// 策略阈值（用于卡片展示对比）
	HourlyPlayThreshold int
	LikeRateThreshold   float64
	ShareRateThreshold  float64

	// 飞书表格链接（可选，非空时替换数据对比为可点击链接）
	FeishuSheetURL string

	// 前后数据对比
	CurrentDate string // 当前采集日期
	CurrentPlay int64
	CurrentLike int64
	CurrentShare int64
	CurrentFollow int64
	CurrentComment int64

	PrevDate    string // 上一次采集日期（可能为空）
	PrevPlay    int64
	PrevLike    int64
	PrevShare   int64
	PrevFollow  int64
	PrevComment int64
}

// buildCompareElement 构建数据对比区域：有飞书链接时显示可点击链接，否则显示文本对比
func buildCompareElement(info PromoteCardInfo) map[string]interface{} {
	if info.FeishuSheetURL != "" {
		return map[string]interface{}{
			"tag": "div",
			"text": map[string]interface{}{
				"tag":     "lark_md",
				"content": fmt.Sprintf("📊 [查看数据详情](%s)", info.FeishuSheetURL),
			},
		}
	}

	var compareText string
	if info.PrevDate != "" {
		prevTime := shortTime(info.PrevDate)
		currTime := shortTime(info.CurrentDate)
		compareText = fmt.Sprintf(
			"**📊 数据对比** (%s → %s)\n"+
				"🎬 播放: %d → %d (%s)\n"+
				"👍 点赞: %d → %d (%s)\n"+
				"🔄 分享: %d → %d (%s)\n"+
				"➕ 关注: %d → %d (%s)\n"+
				"💬 评论: %d → %d (%s)",
			prevTime, currTime,
			info.PrevPlay, info.CurrentPlay, fmtDelta(info.CurrentPlay-info.PrevPlay),
			info.PrevLike, info.CurrentLike, fmtDelta(info.CurrentLike-info.PrevLike),
			info.PrevShare, info.CurrentShare, fmtDelta(info.CurrentShare-info.PrevShare),
			info.PrevFollow, info.CurrentFollow, fmtDelta(info.CurrentFollow-info.PrevFollow),
			info.PrevComment, info.CurrentComment, fmtDelta(info.CurrentComment-info.PrevComment),
		)
	} else {
		compareText = fmt.Sprintf(
			"**📊 当前数据** (%s)\n"+
				"🎬 播放: %d | 👍 点赞: %d | 🔄 分享: %d | ➕ 关注: %d | 💬 评论: %d",
			shortTime(info.CurrentDate),
			info.CurrentPlay, info.CurrentLike, info.CurrentShare, info.CurrentFollow, info.CurrentComment,
		)
	}
	return map[string]interface{}{
		"tag": "div",
		"text": map[string]interface{}{
			"tag":     "lark_md",
			"content": compareText,
		},
	}
}

// buildMetricFields 构建指标 fields（含阈值对比）
func buildMetricFields(info PromoteCardInfo) []interface{} {
	promoteTypeLabel := "关注(粉丝)"
	if info.PromoteType == "like_rate" {
		promoteTypeLabel = "推荐(点赞率)"
	}
	return []interface{}{
		map[string]interface{}{
			"is_short": true,
			"text": map[string]interface{}{
				"tag":     "lark_md",
				"content": fmt.Sprintf("**投放类型**\n%s", promoteTypeLabel),
			},
		},
		map[string]interface{}{
			"is_short": true,
			"text": map[string]interface{}{
				"tag":     "lark_md",
				"content": fmt.Sprintf("**播放增量**\n%d/h (≥%d/h)", info.HourlyPlay, info.HourlyPlayThreshold),
			},
		},
		map[string]interface{}{
			"is_short": true,
			"text": map[string]interface{}{
				"tag":     "lark_md",
				"content": fmt.Sprintf("**点赞率**\n%.2f%% (≥%.2f%%)", info.LikeRate, info.LikeRateThreshold),
			},
		},
		map[string]interface{}{
			"is_short": true,
			"text": map[string]interface{}{
				"tag":     "lark_md",
				"content": fmt.Sprintf("**转发率**\n%.2f%% (≥%.2f%%)", info.ShareRate, info.ShareRateThreshold),
			},
		},
	}
}

// BuildPromoteApprovalCard 构建自动投放审核交互卡片
func BuildPromoteApprovalCard(info PromoteCardInfo) map[string]interface{} {
	desc := truncateRunes(info.Description, 20)

	return map[string]interface{}{
		"config": map[string]interface{}{
			"wide_screen_mode": true,
		},
		"header": map[string]interface{}{
			"template": "blue",
			"title": map[string]interface{}{
				"tag":     "plain_text",
				"content": "🎯 自动投放检测命中",
			},
		},
		"elements": []interface{}{
			// 作者 + 视频描述
			map[string]interface{}{
				"tag": "div",
				"text": map[string]interface{}{
					"tag":     "lark_md",
					"content": fmt.Sprintf("**作者**: %s\n**视频**: %s", info.AuthorName, desc),
				},
			},
			// 投放指标（含阈值）
			map[string]interface{}{
				"tag":    "div",
				"fields": buildMetricFields(info),
			},
			// 分割线
			map[string]interface{}{"tag": "hr"},
			// 数据对比 / 飞书链接
			buildCompareElement(info),
			// 分割线
			map[string]interface{}{"tag": "hr"},
			// 操作按钮
			map[string]interface{}{
				"tag": "action",
				"actions": []interface{}{
					map[string]interface{}{
						"tag": "button",
						"text": map[string]interface{}{
							"tag":     "plain_text",
							"content": "✅ 确认投放",
						},
						"type": "primary",
						"value": map[string]interface{}{
							"action": "confirm",
							"log_id": info.LogID,
						},
					},
					map[string]interface{}{
						"tag": "button",
						"text": map[string]interface{}{
							"tag":     "plain_text",
							"content": "❌ 拒绝投放",
						},
						"type": "danger",
						"value": map[string]interface{}{
							"action": "reject",
							"log_id": info.LogID,
						},
					},
				},
			},
		},
	}
}

// BuildPromoteResultCard 构建操作后的结果卡片（新消息发送）
func BuildPromoteResultCard(action, authorName, description, operator, confirmedAt string) map[string]interface{} {
	var emoji, title, color string
	if action == "confirm" {
		emoji = "✅"
		title = "已确认投放"
		color = "green"
	} else {
		emoji = "❌"
		title = "已拒绝投放"
		color = "red"
	}

	desc := truncateRunes(description, 20)
	content := fmt.Sprintf("**操作人**: %s\n**操作时间**: %s", operator, confirmedAt)
	if authorName != "" {
		content = fmt.Sprintf("**作者**: %s\n%s", authorName, content)
	}
	if desc != "" {
		content = fmt.Sprintf("**视频**: %s\n%s", desc, content)
	}

	return map[string]interface{}{
		"config": map[string]interface{}{
			"wide_screen_mode": true,
		},
		"header": map[string]interface{}{
			"template": color,
			"title": map[string]interface{}{
				"tag":     "plain_text",
				"content": fmt.Sprintf("%s %s", emoji, title),
			},
		},
		"elements": []interface{}{
			map[string]interface{}{
				"tag": "div",
				"text": map[string]interface{}{
					"tag":     "lark_md",
					"content": content,
				},
			},
		},
	}
}

// BuildPromoteInfoCard 构建自动投放通知卡片（无按钮，仅告知）
func BuildPromoteInfoCard(info PromoteCardInfo) map[string]interface{} {
	desc := truncateRunes(info.Description, 20)

	return map[string]interface{}{
		"config": map[string]interface{}{
			"wide_screen_mode": true,
		},
		"header": map[string]interface{}{
			"template": "green",
			"title": map[string]interface{}{
				"tag":     "plain_text",
				"content": "🚀 已自动投放",
			},
		},
		"elements": []interface{}{
			// 作者 + 视频描述
			map[string]interface{}{
				"tag": "div",
				"text": map[string]interface{}{
					"tag":     "lark_md",
					"content": fmt.Sprintf("**作者**: %s\n**视频**: %s", info.AuthorName, desc),
				},
			},
			// 投放指标（含阈值）
			map[string]interface{}{
				"tag":    "div",
				"fields": buildMetricFields(info),
			},
			// 分割线
			map[string]interface{}{"tag": "hr"},
			// 数据对比 / 飞书链接
			buildCompareElement(info),
		},
	}
}

// StopCardInfo 关停通知卡片所需的业务数据
type StopCardInfo struct {
	AuthorName     string // 作者名称
	Description    string // 视频描述
	PromoteType    string // like_rate / followers
	OrderID        int64  // 订单 ID
	PromotionID    string // 平台推广 ID
	Reason         string // 关停原因
	Verified       bool   // 是否已二次验证确认关停
	FeishuSheetURL string // 飞书表格页链接（可选）
}

// BuildPromoteStopCard 构建自动关停通知卡片（红色）
func BuildPromoteStopCard(info StopCardInfo) map[string]interface{} {
	promoteTypeLabel := "关注(粉丝)"
	if info.PromoteType == "like_rate" {
		promoteTypeLabel = "推荐(点赞率)"
	}

	verifyStatus := "⏳ 未验证"
	if info.Verified {
		verifyStatus = "✅ 已确认关停"
	}

	desc := truncateRunes(info.Description, 20)

	elements := []interface{}{
		map[string]interface{}{
			"tag": "div",
			"text": map[string]interface{}{
				"tag":     "lark_md",
				"content": fmt.Sprintf("**作者**: %s\n**视频**: %s", info.AuthorName, desc),
			},
		},
		map[string]interface{}{
			"tag": "div",
			"fields": []interface{}{
				map[string]interface{}{
					"is_short": true,
					"text": map[string]interface{}{
						"tag":     "lark_md",
						"content": fmt.Sprintf("**投放类型**\n%s", promoteTypeLabel),
					},
				},
				map[string]interface{}{
					"is_short": true,
					"text": map[string]interface{}{
						"tag":     "lark_md",
						"content": fmt.Sprintf("**订单ID**\n%d", info.OrderID),
					},
				},
			},
		},
		map[string]interface{}{"tag": "hr"},
		map[string]interface{}{
			"tag": "div",
			"text": map[string]interface{}{
				"tag":     "lark_md",
				"content": fmt.Sprintf("**关停原因**\n%s", info.Reason),
			},
		},
		map[string]interface{}{
			"tag": "div",
			"text": map[string]interface{}{
				"tag":     "lark_md",
				"content": fmt.Sprintf("**验证状态**: %s", verifyStatus),
			},
		},
	}

	// 飞书表格链接
	if info.FeishuSheetURL != "" {
		elements = append(elements,
			map[string]interface{}{"tag": "hr"},
			map[string]interface{}{
				"tag": "div",
				"text": map[string]interface{}{
					"tag":     "lark_md",
					"content": fmt.Sprintf("📊 [查看数据详情](%s)", info.FeishuSheetURL),
				},
			},
		)
	}

	return map[string]interface{}{
		"config": map[string]interface{}{
			"wide_screen_mode": true,
		},
		"header": map[string]interface{}{
			"template": "red",
			"title": map[string]interface{}{
				"tag":     "plain_text",
				"content": "⛔ 自动关停投放",
			},
		},
		"elements": elements,
	}
}
