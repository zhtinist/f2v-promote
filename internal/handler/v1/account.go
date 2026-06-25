package v1

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/AiMarketool/f2v-promote/internal/center/weixin"
	"github.com/AiMarketool/f2v-promote/internal/model"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
)

type AccountHandler struct {
	weixinClient *weixin.Client
	accountRepo  *repository.PlatformAccountRepo
	authorRepo   *repository.AuthorRepo
}

func NewAccountHandler(
	weixinClient *weixin.Client,
	accountRepo *repository.PlatformAccountRepo,
	authorRepo *repository.AuthorRepo,
) *AccountHandler {
	return &AccountHandler{weixinClient: weixinClient, accountRepo: accountRepo, authorRepo: authorRepo}
}

func (h *AccountHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	accounts, total, err := h.accountRepo.ListPaged(page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": accounts, "total": total})
}

func (h *AccountHandler) Create(c *gin.Context) {
	var body struct {
		Name          string          `json:"name" binding:"required"`
		Platform      string          `json:"platform"`
		AccountConfig json.RawMessage `json:"account_config" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}

	platform := body.Platform
	if platform == "" {
		platform = model.PlatformWeixin
	}

	account := &model.PlatformAccount{
		Name:          body.Name,
		Platform:      platform,
		AccountConfig: datatypes.JSON(body.AccountConfig),
		Status:        "active",
	}

	// 微信平台：解析配置并验证登录
	if platform == model.PlatformWeixin {
		var wCfg model.WeixinAccountConfig
		if err := json.Unmarshal(body.AccountConfig, &wCfg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "account_config 格式错误: " + err.Error()})
			return
		}
		token, wCfgOut, err := h.weixinClient.EnsureToken(account)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "登录验证失败: " + err.Error()})
			return
		}
		_ = token
		cfgJSON, _ := json.Marshal(wCfgOut)
		account.AccountConfig = datatypes.JSON(cfgJSON)
	}

	if err := h.accountRepo.Create(account); err != nil {
		log.Printf("service=account action=create request={name=%s} error=%v", body.Name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
		return
	}

	c.JSON(http.StatusOK, account)
}

func (h *AccountHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var body struct {
		Name          *string          `json:"name"`
		Status        *string          `json:"status"`
		AccountConfig *json.RawMessage `json:"account_config"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	updates := make(map[string]any)
	if body.Name != nil {
		updates["name"] = *body.Name
	}
	if body.Status != nil {
		updates["status"] = *body.Status
	}
	if body.AccountConfig != nil {
		updates["account_config"] = *body.AccountConfig
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无更新内容"})
		return
	}

	if err := h.accountRepo.Update(id, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": id, "updated": true})
}

func (h *AccountHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.accountRepo.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "deleted": true})
}

func (h *AccountHandler) RefreshAuthors(c *gin.Context) {
	accountIDStr := c.Param("id")
	accountID, err := strconv.ParseInt(accountIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	rawAuthors, err := h.weixinClient.GetHistoryAuthors(accountIDStr)
	if err != nil {
		log.Printf("service=account action=refresh_authors account=%s error=%v", accountIDStr, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "拉取作者失败: " + err.Error()})
		return
	}

	now := time.Now()
	authors := make([]model.Author, 0, len(rawAuthors))
	for _, author := range rawAuthors {
		rawJSON, _ := json.Marshal(author)
		authors = append(authors, model.Author{
			AccountID: accountID,
			Username:  author.Username,
			Nickname:  author.NickName,
			AvatarURL: author.HeadImgURL,
			Platform:  model.PlatformWeixin,
			RawData:   datatypes.JSON(rawJSON),
			CachedAt:  now,
		})
	}

	if err := h.authorRepo.ReplaceByAccountID(accountID, authors); err != nil {
		log.Printf("service=account action=save_authors account=%d error=%v", accountID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存作者失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"count": len(authors), "authors": authors})
}

func (h *AccountHandler) ListAuthors(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 { page = 1 }
	if pageSize < 1 || pageSize > 100 { pageSize = 20 }
	authors, total, err := h.authorRepo.ListByAccountIDPaged(accountID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": authors, "total": total})
}

func (h *AccountHandler) ListAllAuthors(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 { page = 1 }
	if pageSize < 1 || pageSize > 100 { pageSize = 20 }
	authors, total, err := h.authorRepo.ListAllPaged(page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": authors, "total": total})
}
