package v1

import (
	"net/http"
	"strconv"

	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/gin-gonic/gin"
)

// AuthorVideoHandler 视频管理
type AuthorVideoHandler struct {
	authorVideoRepo *repository.AuthorVideoRepo
	videoStatRepo   *repository.VideoStatRepo
	authorRepo      *repository.AuthorRepo
}

func NewAuthorVideoHandler(
	authorVideoRepo *repository.AuthorVideoRepo,
	videoStatRepo *repository.VideoStatRepo,
	authorRepo *repository.AuthorRepo,
) *AuthorVideoHandler {
	return &AuthorVideoHandler{
		authorVideoRepo: authorVideoRepo,
		videoStatRepo:   videoStatRepo,
		authorRepo:      authorRepo,
	}
}

// List 视频列表（分页 + 筛选）
func (h *AuthorVideoHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var authorID int64
	if v, err := strconv.ParseInt(c.Query("author_id"), 10, 64); err == nil {
		authorID = v
	}
	platform := c.Query("platform")

	videos, total, err := h.authorVideoRepo.ListPaged(authorID, platform, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	// 批量关联作者昵称
	authorIDs := make([]int64, 0, len(videos))
	for _, v := range videos {
		authorIDs = append(authorIDs, v.AuthorID)
	}
	nameMap, _ := h.authorRepo.GetNicknameMap(authorIDs)

	type videoResp struct {
		ID          int64  `json:"id"`
		AuthorID    int64  `json:"author_id"`
		AuthorName  string `json:"author_name"`
		ExportID    string `json:"export_id"`
		Description string `json:"description"`
		CoverURL    string `json:"cover_url"`
		PublishTime string `json:"publish_time"`
		CreatedAt   string `json:"created_at"`
	}

	resp := make([]videoResp, 0, len(videos))
	for _, v := range videos {
		resp = append(resp, videoResp{
			ID:          v.ID,
			AuthorID:    v.AuthorID,
			AuthorName:  nameMap[v.AuthorID],
			ExportID:    v.ExportID,
			Description: v.Description,
			CoverURL:    v.CoverURL,
			PublishTime: v.PublishTime,
			CreatedAt:   v.CreatedAt.Format("2006-01-02 15:04"),
		})
	}

	c.JSON(http.StatusOK, gin.H{"list": resp, "total": total, "page": page, "page_size": pageSize})
}

// Get 视频详情
func (h *AuthorVideoHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	video, err := h.authorVideoRepo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}
	if video == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "视频不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"video": video})
}

// Stats 视频关联的统计数据
func (h *AuthorVideoHandler) Stats(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	// 通过 author_video_id 查找关联 video_stats
	stats, err := h.videoStatRepo.ListByAuthorVideoID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"stats": stats})
}
