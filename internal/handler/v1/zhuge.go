package v1

import (
	"log"
	"net/http"
	"strconv"

	"github.com/AiMarketool/f2v-promote/internal/center/weixin"
	"github.com/AiMarketool/f2v-promote/internal/middleware"
	"github.com/AiMarketool/f2v-promote/internal/service"
	"github.com/AiMarketool/f2v-promote/pkg/utils"
	"github.com/gin-gonic/gin"
)

type ZhugeHandler struct {
	campaignService *service.CampaignService
	weixinClient    *weixin.Client
}

func NewZhugeHandler(campaignService *service.CampaignService, weixinClient *weixin.Client) *ZhugeHandler {
	return &ZhugeHandler{campaignService: campaignService, weixinClient: weixinClient}
}

func (h *ZhugeHandler) GetAuthors(c *gin.Context) {
	accountID := c.Query("account_id")
	if accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account_id is required"})
		return
	}
	authors, err := h.weixinClient.GetHistoryAuthors(accountID)
	if err != nil {
		log.Printf("service=zhuge-handler action=get_authors account=%s error=%v", accountID, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"authors": authors})
}

func (h *ZhugeHandler) GetVideos(c *gin.Context) {
	username := c.PostForm("username")
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username is required"})
		return
	}
	accountID := c.PostForm("account_id")
	if accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account_id is required"})
		return
	}
	beginDate := c.PostForm("begin_date")
	endDate := c.PostForm("end_date")
	videos, err := h.weixinClient.GetAuthorVideos(accountID, username, beginDate, endDate)
	if err != nil {
		log.Printf("service=zhuge-handler action=get_videos account=%s username=%s error=%v", accountID, username, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"videos": videos})
}

func (h *ZhugeHandler) Create(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	script := c.PostForm("script")
	if script == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "script is required"})
		return
	}

	accountIDStr := c.PostForm("account_id")
	if accountIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account_id is required"})
		return
	}
	accountID, err := strconv.ParseInt(accountIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account_id"})
		return
	}

	costThreshold := 1.0
	if v := c.PostForm("cost_threshold"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			costThreshold = f
		}
	}

	tagGroupCount := 3
	if v := c.PostForm("tag_group_count"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			tagGroupCount = n
		}
	}

	campaign, tagGroups, flatTags, err := h.campaignService.CreateWithAITags(user.ID, accountID, accountIDStr, script, costThreshold, tagGroupCount)
	if err != nil {
		log.Printf("service=zhuge-handler action=create_campaign error=%v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI 生成标签失败: " + err.Error()})
		return
	}

	tagMap := make(map[string]string, len(flatTags))
	for text, meta := range flatTags {
		tagMap[meta.ID] = text
	}

	c.JSON(http.StatusOK, gin.H{
		"campaign_id": campaign.ID,
		"status":      campaign.Status,
		"tag_groups":  tagGroups,
		"tag_map":     tagMap,
	})
}

func (h *ZhugeHandler) Confirm(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	campaignID, err := strconv.ParseInt(c.Param("campaign_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid campaign_id"})
		return
	}

	orders, err := h.campaignService.Confirm(user.ID, campaignID)
	if err != nil {
		log.Printf("service=zhuge-handler action=confirm_campaign campaign=%d error=%v", campaignID, err)
		status := http.StatusInternalServerError
		errMsg := err.Error()
		if utils.Contains(errMsg, "not found") {
			status = http.StatusNotFound
		} else if utils.Contains(errMsg, "unauthorized") {
			status = http.StatusForbidden
		}
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"campaign_id": campaignID,
		"total":       len(orders),
		"orders":      orders,
	})
}

func (h *ZhugeHandler) ListCampaigns(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"campaigns": []interface{}{}})
}
