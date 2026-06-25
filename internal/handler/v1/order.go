package v1

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/AiMarketool/f2v-promote/internal/middleware"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/AiMarketool/f2v-promote/internal/service"
	"github.com/gin-gonic/gin"
)

type OrderHandler struct {
	campaignService *service.CampaignService
	orderRepo       *repository.OrderRepo
	campaignRepo    *repository.CampaignRepo
	promoteLogRepo  *repository.AutoPromoteLogRepo
}

func NewOrderHandler(
	campaignService *service.CampaignService,
	orderRepo *repository.OrderRepo,
	campaignRepo *repository.CampaignRepo,
	promoteLogRepo *repository.AutoPromoteLogRepo,
) *OrderHandler {
	return &OrderHandler{
		campaignService: campaignService,
		orderRepo:       orderRepo,
		campaignRepo:    campaignRepo,
		promoteLogRepo:  promoteLogRepo,
	}
}

func (h *OrderHandler) Submit(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	orderID, err := strconv.ParseInt(c.Param("order_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order_id"})
		return
	}
	videoJSON := c.PostForm("video_json")
	authorJSON := c.PostForm("author_json")

	if !json.Valid([]byte(videoJSON)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid video_json"})
		return
	}
	if !json.Valid([]byte(authorJSON)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid author_json"})
		return
	}

	result, err := h.campaignService.SubmitOrder(user.ID, orderID, json.RawMessage(videoJSON), json.RawMessage(authorJSON))
	if err != nil {
		log.Printf("service=order-handler action=submit order=%d error=%v", orderID, err)
		status := http.StatusInternalServerError
		msg := err.Error()
		switch msg {
		case "order not found":
			status = http.StatusNotFound
		case "forbidden":
			status = http.StatusForbidden
		}
		c.JSON(status, gin.H{"error": msg})
		return
	}

	c.JSON(http.StatusOK, result)
}

func (h *OrderHandler) CheckPayment(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	orderID, err := strconv.ParseInt(c.Param("order_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order_id"})
		return
	}
	result, err := h.campaignService.CheckPayment(user.ID, orderID)
	if err != nil {
		log.Printf("service=order-handler action=check_payment order=%d error=%v", orderID, err)
		status := http.StatusInternalServerError
		msg := err.Error()
		switch msg {
		case "order not found":
			status = http.StatusNotFound
		case "forbidden":
			status = http.StatusForbidden
		}
		c.JSON(status, gin.H{"error": msg})
		return
	}

	c.JSON(http.StatusOK, result)
}

func (h *OrderHandler) UpdateStatus(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	orderID, err := strconv.ParseInt(c.Param("order_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order_id"})
		return
	}

	var body struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status is required"})
		return
	}

	allowed := map[string]bool{
		"active": true, "closed": true, "error": true, "expired": true,
	}
	if !allowed[body.Status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不允许的状态: " + body.Status})
		return
	}

	order, err := h.orderRepo.GetByID(orderID)
	if err != nil {
		log.Printf("service=order-handler action=get_order order=%d error=%v", orderID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	if order == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "订单不存在"})
		return
	}
	if order.UserID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权操作"})
		return
	}

	if order.Status == body.Status {
		c.JSON(http.StatusOK, gin.H{"order_id": orderID, "status": body.Status, "changed": false})
		return
	}

	if err := h.orderRepo.UpdateStatus(orderID, body.Status, nil); err != nil {
		log.Printf("service=order-handler action=update_status order=%d error=%v", orderID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}

	log.Printf("service=order-handler action=update_status order=%d from=%s to=%s", orderID, order.Status, body.Status)
	c.JSON(http.StatusOK, gin.H{"order_id": orderID, "status": body.Status, "changed": true})
}

func (h *OrderHandler) GetDetail(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	orderID, err := strconv.ParseInt(c.Param("order_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order_id"})
		return
	}
	result, err := h.campaignService.GetDetail(user.ID, orderID)
	if err != nil {
		log.Printf("service=order-handler action=get_detail order=%d error=%v", orderID, err)
		status := http.StatusInternalServerError
		msg := err.Error()
		switch msg {
		case "order not found":
			status = http.StatusNotFound
		case "forbidden":
			status = http.StatusForbidden
		}
		c.JSON(status, gin.H{"error": msg})
		return
	}

	c.JSON(http.StatusOK, result)
}

func (h *OrderHandler) GetRecord(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	orderID, err := strconv.ParseInt(c.Param("order_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order_id"})
		return
	}
	result, err := h.campaignService.GetRecord(user.ID, orderID)
	if err != nil {
		log.Printf("service=order-handler action=get_record order=%d error=%v", orderID, err)
		status := http.StatusInternalServerError
		msg := err.Error()
		switch msg {
		case "order not found":
			status = http.StatusNotFound
		case "forbidden":
			status = http.StatusForbidden
		}
		c.JSON(status, gin.H{"error": msg})
		return
	}

	c.JSON(http.StatusOK, result)
}

func (h *OrderHandler) ListOrders(c *gin.Context) {
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

	orders, total, err := h.orderRepo.PaginateByUserID(user.ID, status, filterID, startDate, endDate, page, pageSize)
	if err != nil {
		log.Printf("service=order-handler action=list_orders error=%v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}

	items := make([]gin.H, 0, len(orders))

	// 批量查关联 promote_log_id
	orderIDs := make([]int64, len(orders))
	for i, o := range orders {
		orderIDs[i] = o.ID
	}
	logIDMap := make(map[int64]int64)
	if h.promoteLogRepo != nil {
		logIDMap, _ = h.promoteLogRepo.GetLogIDsByOrderIDs(orderIDs)
	}

	for _, o := range orders {
		var tagGroup any
		_ = json.Unmarshal(o.TagGroup, &tagGroup)

		// 从 latest_detail 提取消耗金额
		var tfMoney any
		if len(o.LatestDetail) > 0 {
			var detail map[string]any
			if json.Unmarshal(o.LatestDetail, &detail) == nil {
				tfMoney = detail["tf_money"]
			}
		}

		item := gin.H{
			"id": o.ID, "campaign_id": o.CampaignID, "user_id": o.UserID,
			"tag_group": tagGroup, "platform_order_id": o.PlatformOrderID,
			"status": o.Status, "close_reason": o.CloseReason,
			"batch_id": o.BatchID, "pay_url": o.PayURL,
			"source": o.Source, "created_at": o.CreatedAt,
			"tf_money": tfMoney,
		}
		if lid, ok := logIDMap[o.ID]; ok {
			item["promote_log_id"] = lid
		}
		items = append(items, item)
	}

	c.JSON(http.StatusOK, gin.H{"orders": items, "total": total, "page": page, "page_size": pageSize})
}

func (h *OrderHandler) ExportCSV(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	campaigns, err := h.campaignRepo.GetByUserID(user.ID)
	if err != nil {
		log.Printf("service=order-handler action=export_csv error=%v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=orders.csv")
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

func (h *OrderHandler) Stop(c *gin.Context) {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	orderID, err := strconv.ParseInt(c.Param("order_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order_id"})
		return
	}

	if err := h.campaignService.StopOrder(user.ID, orderID); err != nil {
		log.Printf("service=order-handler action=stop order=%d error=%v", orderID, err)
		status := http.StatusInternalServerError
		msg := err.Error()
		switch msg {
		case "order not found":
			status = http.StatusNotFound
		case "forbidden":
			status = http.StatusForbidden
		}
		c.JSON(status, gin.H{"error": msg})
		return
	}

	c.JSON(http.StatusOK, gin.H{"order_id": orderID, "status": "closed", "message": "投放已停止"})
}

func csvEscape(s string) string {
	needsQuote := false
	for _, ch := range s {
		if ch == ',' || ch == '"' || ch == '\n' || ch == '\r' {
			needsQuote = true
			break
		}
	}
	if !needsQuote {
		return s
	}
	escaped := ""
	for _, ch := range s {
		if ch == '"' {
			escaped += `""`
		} else {
			escaped += string(ch)
		}
	}
	return `"` + escaped + `"`
}
