package v1

import (
	"log"
	"net/http"
	"strconv"

	"github.com/AiMarketool/f2v-promote/internal/model"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/gin-gonic/gin"
)

// StrategyHandler 投放策略管理
type StrategyHandler struct {
	strategyRepo     *repository.StrategyRepo
	promoteLogRepo   *repository.AutoPromoteLogRepo
	authorRepo       *repository.AuthorRepo
	authorVideoRepo  *repository.AuthorVideoRepo
}

func NewStrategyHandler(strategyRepo *repository.StrategyRepo, promoteLogRepo *repository.AutoPromoteLogRepo, authorRepo *repository.AuthorRepo, authorVideoRepo *repository.AuthorVideoRepo) *StrategyHandler {
	return &StrategyHandler{strategyRepo: strategyRepo, promoteLogRepo: promoteLogRepo, authorRepo: authorRepo, authorVideoRepo: authorVideoRepo}
}

// strategyResp 策略响应 DTO，附带作者名称
type strategyResp struct {
	model.AuthorPromoteStrategy
	AuthorName string `json:"author_name"`
}

// List 查询全部策略
// @Summary 获取全部投放策略列表
// @Success 200 {object} gin.H{strategies=[]model.AuthorPromoteStrategy}
func (h *StrategyHandler) List(c *gin.Context) {
	list, err := h.strategyRepo.List()
	if err != nil {
		log.Printf("service=strategy-handler action=list error=%v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}

	// 批量查询作者名称
	ids := make([]int64, len(list))
	for i, s := range list {
		ids[i] = s.AuthorID
	}
	nameMap, _ := h.authorRepo.GetNicknameMap(ids)

	resp := make([]strategyResp, len(list))
	for i, s := range list {
		resp[i] = strategyResp{AuthorPromoteStrategy: s, AuthorName: nameMap[s.AuthorID]}
	}
	c.JSON(http.StatusOK, gin.H{"strategies": resp})
}

// Get 按 ID 查询策略
// @Summary 获取单个投放策略
// @Success 200 {object} gin.H{strategy=model.AuthorPromoteStrategy}
func (h *StrategyHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	s, err := h.strategyRepo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "策略不存在"})
		return
	}

	nameMap, _ := h.authorRepo.GetNicknameMap([]int64{s.AuthorID})
	c.JSON(http.StatusOK, gin.H{"strategy": strategyResp{
		AuthorPromoteStrategy: *s,
		AuthorName:            nameMap[s.AuthorID],
	}})
}

// createStrategyReq 创建策略请求体
type createStrategyReq struct {
	AuthorID                int64   `json:"author_id" binding:"required"`
	AccountID               int64   `json:"account_id" binding:"required"`
	Platform                string  `json:"platform"`
	NotifyFeishu            *bool   `json:"notify_feishu"`
	HourlyPlayThreshold     int     `json:"hourly_play_threshold" binding:"required"`
	LikeRateThreshold       float64 `json:"like_rate_threshold"`
	CommentRateThreshold    float64 `json:"comment_rate_threshold"`
	FollowRateThreshold     float64 `json:"follow_rate_threshold"`
	ShareRateThreshold      float64 `json:"share_rate_threshold"`
	CompletionRateThreshold float64 `json:"completion_rate_threshold"`
}

// Create 创建策略
// @Summary 创建投放策略
// @Accept json
// @Param body body createStrategyReq true "策略参数"
// @Success 200 {object} gin.H{strategy=model.AuthorPromoteStrategy}
func (h *StrategyHandler) Create(c *gin.Context) {
	var req createStrategyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 检查唯一性
	existing, _ := h.strategyRepo.GetByAuthorID(req.AuthorID)
	if existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "该作者已有策略"})
		return
	}

	platform := req.Platform
	if platform == "" {
		platform = "weixin"
	}

	notifyFeishu := false
	if req.NotifyFeishu != nil {
		notifyFeishu = *req.NotifyFeishu
	}

	s := &model.AuthorPromoteStrategy{
		AuthorID:                req.AuthorID,
		AccountID:               req.AccountID,
		Platform:                platform,
		Enabled:                 true,
		NotifyFeishu:            notifyFeishu,
		HourlyPlayThreshold:     req.HourlyPlayThreshold,
		LikeRateThreshold:       req.LikeRateThreshold,
		CommentRateThreshold:    req.CommentRateThreshold,
		FollowRateThreshold:     req.FollowRateThreshold,
		ShareRateThreshold:      req.ShareRateThreshold,
		CompletionRateThreshold: req.CompletionRateThreshold,
	}

	if err := h.strategyRepo.Create(s); err != nil {
		log.Printf("service=strategy-handler action=create error=%v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"strategy": s})
}

// updateStrategyReq 更新策略请求体
type updateStrategyReq struct {
	Enabled                 *bool    `json:"enabled"`
	NotifyFeishu            *bool    `json:"notify_feishu"`
	HourlyPlayThreshold     *int     `json:"hourly_play_threshold"`
	LikeRateThreshold       *float64 `json:"like_rate_threshold"`
	CommentRateThreshold    *float64 `json:"comment_rate_threshold"`
	FollowRateThreshold     *float64 `json:"follow_rate_threshold"`
	ShareRateThreshold      *float64 `json:"share_rate_threshold"`
	CompletionRateThreshold *float64 `json:"completion_rate_threshold"`
}

// Update 更新策略
// @Summary 更新投放策略
// @Accept json
// @Param id path int true "策略ID"
// @Param body body updateStrategyReq true "更新字段"
// @Success 200 {object} gin.H{strategy=model.AuthorPromoteStrategy}
func (h *StrategyHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	s, err := h.strategyRepo.GetByID(id)
	if err != nil || s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "策略不存在"})
		return
	}

	var req updateStrategyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Enabled != nil {
		s.Enabled = *req.Enabled
	}
	if req.NotifyFeishu != nil {
		s.NotifyFeishu = *req.NotifyFeishu
	}
	if req.HourlyPlayThreshold != nil {
		s.HourlyPlayThreshold = *req.HourlyPlayThreshold
	}
	if req.LikeRateThreshold != nil {
		s.LikeRateThreshold = *req.LikeRateThreshold
	}
	if req.CommentRateThreshold != nil {
		s.CommentRateThreshold = *req.CommentRateThreshold
	}
	if req.FollowRateThreshold != nil {
		s.FollowRateThreshold = *req.FollowRateThreshold
	}
	if req.ShareRateThreshold != nil {
		s.ShareRateThreshold = *req.ShareRateThreshold
	}
	if req.CompletionRateThreshold != nil {
		s.CompletionRateThreshold = *req.CompletionRateThreshold
	}

	if err := h.strategyRepo.Update(s); err != nil {
		log.Printf("service=strategy-handler action=update error=%v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"strategy": s})
}

// Delete 删除策略
// @Summary 删除投放策略
// @Param id path int true "策略ID"
// @Success 200 {object} gin.H{message=string}
func (h *StrategyHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.strategyRepo.Delete(id); err != nil {
		log.Printf("service=strategy-handler action=delete error=%v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

// logResp 日志响应 DTO，附带视频信息
type logResp struct {
	model.AutoPromoteLog
	ExportID         string `json:"export_id"`
	VideoDescription string `json:"video_description"`
	AuthorName       string `json:"author_name"`
}

// ListLogs 查询策略的投放日志
// @Summary 获取策略投放日志
// @Param id path int true "策略ID"
// @Success 200 {object} gin.H{logs=[]logResp,total=int64}
func (h *StrategyHandler) ListLogs(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
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

	logs, total, err := h.promoteLogRepo.ListByStrategyID(id, page, pageSize)
	if err != nil {
		log.Printf("service=strategy-handler action=list_logs error=%v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}

	// 批量关联视频信息
	videoIDs := make([]int64, len(logs))
	for i, l := range logs {
		videoIDs[i] = l.AuthorVideoID
	}
	videoMap, _ := h.authorVideoRepo.GetVideoInfoMap(videoIDs)

	resp := make([]logResp, len(logs))
	for i, l := range logs {
		resp[i] = logResp{AutoPromoteLog: l}
		if v, ok := videoMap[l.AuthorVideoID]; ok {
			resp[i].ExportID = v.ExportID
			resp[i].VideoDescription = v.Description
		}
	}
	c.JSON(http.StatusOK, gin.H{"logs": resp, "total": total, "page": page, "page_size": pageSize})
}

// ListAllLogs 全局投放日志列表（支持作者/视频筛选）
// @Summary 获取全部投放日志
// @Param author_id query int false "作者ID"
// @Param video_keyword query string false "视频描述关键词"
// @Success 200 {object} gin.H{logs=[]logResp,total=int64}
func (h *StrategyHandler) ListAllLogs(c *gin.Context) {
	page := 1
	if v, err := strconv.Atoi(c.DefaultQuery("page", "1")); err == nil && v > 0 {
		page = v
	}
	pageSize := 20
	if v, err := strconv.Atoi(c.DefaultQuery("page_size", "20")); err == nil && v > 0 && v <= 100 {
		pageSize = v
	}

	var authorID int64
	if v, err := strconv.ParseInt(c.Query("author_id"), 10, 64); err == nil {
		authorID = v
	}
	var filterID int64
	if v, err := strconv.ParseInt(c.Query("id"), 10, 64); err == nil && v > 0 {
		filterID = v
	}
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	// 视频描述筛选 → 先查 author_video IDs
	var videoIDs []int64
	videoKeyword := c.Query("video_keyword")
	if videoKeyword != "" {
		ids, _ := h.authorVideoRepo.SearchIDsByDescription(videoKeyword)
		if len(ids) == 0 {
			// 无匹配视频，直接返回空
			c.JSON(http.StatusOK, gin.H{"logs": []logResp{}, "total": 0, "page": page, "page_size": pageSize})
			return
		}
		videoIDs = ids
	}

	logs, total, err := h.promoteLogRepo.ListAllLogs(page, pageSize, authorID, videoIDs, c.Query("status"), filterID, startDate, endDate)
	if err != nil {
		log.Printf("service=strategy-handler action=list_all_logs error=%v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}

	// 批量关联视频信息 + 作者名称
	avIDs := make([]int64, len(logs))
	aIDs := make([]int64, len(logs))
	for i, l := range logs {
		avIDs[i] = l.AuthorVideoID
		aIDs[i] = l.AuthorID
	}
	videoMap, _ := h.authorVideoRepo.GetVideoInfoMap(avIDs)
	nameMap, _ := h.authorRepo.GetNicknameMap(aIDs)

	resp := make([]logResp, len(logs))
	for i, l := range logs {
		resp[i] = logResp{AutoPromoteLog: l}
		if v, ok := videoMap[l.AuthorVideoID]; ok {
			resp[i].ExportID = v.ExportID
			resp[i].VideoDescription = v.Description
		}
		resp[i].AuthorName = nameMap[l.AuthorID]
	}
	c.JSON(http.StatusOK, gin.H{"logs": resp, "total": total, "page": page, "page_size": pageSize})
}

// Pending 待审核列表（detected 状态）
// @Summary 获取待审核投放日志列表
// @Success 200 {object} gin.H{logs=[]model.AutoPromoteLog,total=int64}
func (h *StrategyHandler) Pending(c *gin.Context) {
	page := 1
	if v, err := strconv.Atoi(c.DefaultQuery("page", "1")); err == nil && v > 0 {
		page = v
	}
	pageSize := 20
	if v, err := strconv.Atoi(c.DefaultQuery("page_size", "20")); err == nil && v > 0 && v <= 100 {
		pageSize = v
	}

	logs, total, err := h.promoteLogRepo.ListDetected(page, pageSize)
	if err != nil {
		log.Printf("service=strategy-handler action=pending error=%v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs, "total": total, "page": page, "page_size": pageSize})
}

// Confirm 人工确认投放
// @Summary 确认投放（detected → confirmed）
// @Param id path int true "日志ID"
// @Success 200 {object} gin.H{message=string}
func (h *StrategyHandler) Confirm(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	ok, err := h.promoteLogRepo.ConfirmByID(id)
	if err != nil {
		log.Printf("service=strategy-handler action=confirm error=%v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "确认失败"})
		return
	}
	if !ok {
		c.JSON(http.StatusConflict, gin.H{"error": "该记录不在待审核状态"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已确认"})
}

// Reject 人工拒绝投放
// @Summary 拒绝投放（detected → rejected）
// @Param id path int true "日志ID"
// @Success 200 {object} gin.H{message=string}
func (h *StrategyHandler) Reject(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	ok, err := h.promoteLogRepo.RejectByID(id)
	if err != nil {
		log.Printf("service=strategy-handler action=reject error=%v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "拒绝失败"})
		return
	}
	if !ok {
		c.JSON(http.StatusConflict, gin.H{"error": "该记录不在待审核状态"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已拒绝"})
}

// UpdateLogStatus 手动修改投放日志状态
// @Summary 修改投放日志状态
// @Param id path int true "日志ID"
// @Param body body object{status=string} true "新状态"
// @Success 200 {object} gin.H{message=string}
func (h *StrategyHandler) UpdateLogStatus(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var body struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status 必填"})
		return
	}

	// 校验状态值合法性
	validStatuses := map[string]bool{
		model.PromoteLogDetected:  true,
		model.PromoteLogConfirmed: true,
		model.PromoteLogRejected:  true,
		model.PromoteLogQueued:    true,
		model.PromoteLogRunning:   true,
		model.PromoteLogCompleted: true,
		model.PromoteLogFailed:    true,
	}
	if !validStatuses[body.Status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的状态值"})
		return
	}

	if err := h.promoteLogRepo.UpdateStatus(id, body.Status, nil); err != nil {
		log.Printf("service=strategy-handler action=update_log_status id=%d status=%s error=%v", id, body.Status, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}

	log.Printf("service=strategy-handler action=update_log_status id=%d status=%s result=success", id, body.Status)
	c.JSON(http.StatusOK, gin.H{"message": "状态已更新"})
}
