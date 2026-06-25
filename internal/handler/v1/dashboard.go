package v1

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/AiMarketool/f2v-promote/internal/middleware"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/gin-gonic/gin"
)

type DashboardHandler struct {
	campaignRepo    *repository.CampaignRepo
	orderRepo       *repository.OrderRepo
	authorRepo      *repository.AuthorRepo
	authorVideoRepo *repository.AuthorVideoRepo
	videoStatRepo   *repository.VideoStatRepo
	strategyRepo    *repository.StrategyRepo
	promoteLogRepo  *repository.AutoPromoteLogRepo
}

func NewDashboardHandler(
	campaignRepo *repository.CampaignRepo,
	orderRepo *repository.OrderRepo,
	authorRepo *repository.AuthorRepo,
	authorVideoRepo *repository.AuthorVideoRepo,
	videoStatRepo *repository.VideoStatRepo,
	strategyRepo *repository.StrategyRepo,
	promoteLogRepo *repository.AutoPromoteLogRepo,
) *DashboardHandler {
	return &DashboardHandler{
		campaignRepo:    campaignRepo,
		orderRepo:       orderRepo,
		authorRepo:      authorRepo,
		authorVideoRepo: authorVideoRepo,
		videoStatRepo:   videoStatRepo,
		strategyRepo:    strategyRepo,
		promoteLogRepo:  promoteLogRepo,
	}
}

// Overview 全局统计面板数据 (替代原 /stats)
func (h *DashboardHandler) Overview(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	// 订单维度 (保留原有)
	orderStats, _ := h.orderRepo.GetStatsByUserID(user.ID)
	campaigns, _ := h.campaignRepo.CountByUserID(user.ID)
	orderStats["campaigns"] = campaigns

	// 新增维度
	authorCount, _ := h.authorRepo.Count()
	videoCount, _ := h.authorVideoRepo.Count()
	todayStats, _ := h.videoStatRepo.TodayCount()
	enabledStrategies, _ := h.strategyRepo.CountEnabled()
	todayPromotes, _ := h.promoteLogRepo.TodayCount()
	pendingPromotes, _ := h.promoteLogRepo.PendingCount()

	c.JSON(http.StatusOK, gin.H{
		"orders":             orderStats,
		"author_count":       authorCount,
		"video_count":        videoCount,
		"today_stats_count":  todayStats,
		"enabled_strategies": enabledStrategies,
		"today_promotes":     todayPromotes,
		"pending_promotes":   pendingPromotes,
	})
}

// Stats 保留兼容 (废弃,重定向到 Overview)
func (h *DashboardHandler) Stats(c *gin.Context) {
	h.Overview(c)
}

// RecentOrders returns the most recent N orders for the current user.
func (h *DashboardHandler) RecentOrders(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	orders, err := h.orderRepo.GetRecentByUserID(user.ID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	// 构造返回（包含 detail 中的 cost/roi 等）
	result := make([]gin.H, 0, len(orders))
	for _, o := range orders {
		item := gin.H{
			"id":         o.ID,
			"status":     o.Status,
			"created_at": o.CreatedAt,
		}

		// 从 tag_group 取 name
		var tg map[string]any
		if json.Unmarshal(o.TagGroup, &tg) == nil {
			item["tag_name"] = tg["name"]
		}

		// 从 latest_detail 取关键指标
		if len(o.LatestDetail) > 0 {
			var d struct {
				Cost     string `json:"cost"`
				ROI      string `json:"roi"`
				ViewNum  int    `json:"view_num"`
				FocusNum int    `json:"focus_num"`
				ClickNum int    `json:"click_num"`
			}
			if json.Unmarshal(o.LatestDetail, &d) == nil {
				item["cost"] = d.Cost
				item["roi"] = d.ROI
				item["view_num"] = d.ViewNum
				item["focus_num"] = d.FocusNum
				item["click_num"] = d.ClickNum
			}
		}

		result = append(result, item)
	}

	c.JSON(http.StatusOK, gin.H{"orders": result})
}
