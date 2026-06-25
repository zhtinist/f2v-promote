package v1

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/AiMarketool/f2v-promote/internal/middleware"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/gin-gonic/gin"
)

type CampaignHandler struct {
	campaignRepo   *repository.CampaignRepo
	orderRepo      *repository.OrderRepo
	promoteLogRepo *repository.AutoPromoteLogRepo
}

func NewCampaignHandler(campaignRepo *repository.CampaignRepo, orderRepo *repository.OrderRepo, promoteLogRepo *repository.AutoPromoteLogRepo) *CampaignHandler {
	return &CampaignHandler{campaignRepo: campaignRepo, orderRepo: orderRepo, promoteLogRepo: promoteLogRepo}
}

func (h *CampaignHandler) List(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	page := 1
	if v, err := strconv.Atoi(c.DefaultQuery("page", "1")); err == nil && v > 0 {
		page = v
	}
	pageSize := 20
	if v, err := strconv.Atoi(c.DefaultQuery("page_size", "20")); err == nil && v > 0 && v <= 100 {
		pageSize = v
	}
	status := c.Query("status")
	var filterID int64
	if v, err := strconv.ParseInt(c.Query("id"), 10, 64); err == nil && v > 0 {
		filterID = v
	}
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	campaigns, total, err := h.campaignRepo.PaginateByUserID(user.ID, status, filterID, startDate, endDate, page, pageSize)
	if err != nil {
		log.Printf("service=campaign-handler action=list error=%v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"campaigns": campaigns, "total": total, "page": page, "page_size": pageSize})
}

func (h *CampaignHandler) ListOrders(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	campaignID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid campaign id"})
		return
	}

	campaign, err := h.campaignRepo.GetByID(campaignID)
	if err != nil {
		log.Printf("service=campaign-handler action=get_by_id campaign=%d error=%v", campaignID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	if campaign == nil || campaign.UserID != user.ID {
		c.JSON(http.StatusNotFound, gin.H{"error": "活动不存在"})
		return
	}

	orders, err := h.orderRepo.GetByCampaignID(campaignID)
	if err != nil {
		log.Printf("service=campaign-handler action=list_orders campaign=%d error=%v", campaignID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}

	// 批量查关联 promote_log_id
	orderIDs := make([]int64, len(orders))
	for i, o := range orders {
		orderIDs[i] = o.ID
	}
	logIDMap := make(map[int64]int64)
	if h.promoteLogRepo != nil {
		logIDMap, _ = h.promoteLogRepo.GetLogIDsByOrderIDs(orderIDs)
	}

	items := make([]gin.H, 0, len(orders))
	for _, o := range orders {
		item := gin.H{
			"id": o.ID, "campaign_id": o.CampaignID,
			"tag_group": o.TagGroup, "platform_order_id": o.PlatformOrderID,
			"status": o.Status, "close_reason": o.CloseReason,
			"created_at": o.CreatedAt,
		}
		if lid, ok := logIDMap[o.ID]; ok {
			item["promote_log_id"] = lid
		}
		items = append(items, item)
	}

	c.JSON(http.StatusOK, gin.H{"orders": items})
}

func (h *CampaignHandler) ExportCSV(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	campaigns, err := h.campaignRepo.GetByUserID(user.ID)
	if err != nil {
		log.Printf("service=campaign-handler action=export_csv error=%v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=campaigns.csv")
	_, _ = c.Writer.WriteString("\xEF\xBB\xBF")
	_, _ = c.Writer.WriteString("活动ID,文案,订单ID,标签组,平台订单ID,状态,关闭原因\n")

	for _, camp := range campaigns {
		orders, err := h.orderRepo.GetByCampaignID(camp.ID)
		if err != nil {
			continue
		}
		scriptPreview := camp.Script
		if len(scriptPreview) > 50 {
			scriptPreview = scriptPreview[:50]
		}
		for _, o := range orders {
			var tg map[string]interface{}
			_ = json.Unmarshal(o.TagGroup, &tg)
			tagName := ""
			if name, ok := tg["name"]; ok {
				if s, ok := name.(string); ok {
					tagName = s
				}
			}
			platformID := ""
			if o.PlatformOrderID != nil {
				platformID = *o.PlatformOrderID
			}
			closeReason := ""
			if o.CloseReason != nil {
				closeReason = *o.CloseReason
			}

			line := csvEscape(fmt.Sprintf("%d", camp.ID)) + "," +
				csvEscape(scriptPreview) + "," +
				csvEscape(fmt.Sprintf("%d", o.ID)) + "," +
				csvEscape(tagName) + "," +
				csvEscape(platformID) + "," +
				csvEscape(o.Status) + "," +
				csvEscape(closeReason) + "\n"
			_, _ = c.Writer.WriteString(line)
		}
	}
}
