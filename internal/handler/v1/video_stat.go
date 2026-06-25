package v1

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/AiMarketool/f2v-promote/internal/model"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/gin-gonic/gin"
)

type VideoStatHandler struct {
	repo *repository.VideoStatRepo
}

func NewVideoStatHandler(repo *repository.VideoStatRepo) *VideoStatHandler {
	return &VideoStatHandler{repo: repo}
}

func (h *VideoStatHandler) List(c *gin.Context) {
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
	var authorVideoID int64
	if v, err := strconv.ParseInt(c.Query("author_video_id"), 10, 64); err == nil {
		authorVideoID = v
	}
	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")

	var feishuSynced *bool
	if v := c.Query("feishu_synced"); v != "" {
		b := v == "true"
		feishuSynced = &b
	}

	list, total, err := h.repo.ListFiltered(authorID, authorVideoID, dateFrom, dateTo, feishuSynced, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": list, "total": total, "page": page, "page_size": pageSize})
}

func (h *VideoStatHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	stat, err := h.repo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if stat == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, stat)
}

func (h *VideoStatHandler) Create(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var stats []model.VideoStat
	if err := json.Unmarshal(body, &stats); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(stats) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "空数据"})
		return
	}

	count, err := h.repo.BulkUpsert(stats)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "count": count})
}

func (h *VideoStatHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.repo.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *VideoStatHandler) ImportCSV(c *gin.Context) {
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请上传 CSV 文件"})
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	if _, err := reader.Read(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CSV 格式错误"})
		return
	}

	var stats []model.VideoStat
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		if len(record) < 15 {
			continue
		}

		stat := model.VideoStat{
			Description:     strings.TrimSpace(record[0]),
			ExportID:        strings.TrimSpace(record[1]),
			PublishDate:     strings.TrimSpace(record[2]),
			CompletionRate:  strings.TrimSpace(record[3]),
			AvgPlayDuration: strings.TrimSpace(record[4]),
			PlayCount:       parseInt(record[5]),
			RecommendCount:  parseInt(record[6]),
			LikeCount:       parseInt(record[7]),
			CommentCount:    parseInt(record[8]),
			ShareCount:      parseInt(record[9]),
			FollowCount:     parseInt(record[10]),
			ForwardCount:    parseInt(record[11]),
			RingtoneCount:   parseInt(record[12]),
			StatusCount:     parseInt(record[13]),
			CoverCount:      parseInt(record[14]),
		}
		stats = append(stats, stat)
	}

	if len(stats) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CSV 中没有有效数据"})
		return
	}

	count, err := h.repo.BulkUpsert(stats)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "imported": count})
}

func parseInt(s string) int64 {
	s = strings.TrimSpace(s)
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}
