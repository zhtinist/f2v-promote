package weixin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AiMarketool/f2v-promote/internal/config"
	"github.com/AiMarketool/f2v-promote/internal/model"
	"github.com/AiMarketool/f2v-promote/internal/repository"
)

// ── 浏览器伪装 Headers ──

var defaultHeaders = map[string]string{
	"User-Agent":         "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
	"Accept":             "application/json, text/plain, */*",
	"Accept-Language":    "zh-CN,zh;q=0.9",
	"Content-Type":       "application/json;charset=UTF-8",
	"Origin":             "https://zhuge.v-ma.com",
	"Referer":            "https://zhuge.v-ma.com/",
	"Cache-Control":      "no-cache",
	"Pragma":             "no-cache",
	"Sec-Ch-Ua":          `"Chromium";v="146", "Not-A.Brand";v="24", "Google Chrome";v="146"`,
	"Sec-Ch-Ua-Mobile":   "?0",
	"Sec-Ch-Ua-Platform": `"macOS"`,
	"Sec-Fetch-Dest":     "empty",
	"Sec-Fetch-Mode":     "cors",
	"Sec-Fetch-Site":     "cross-site",
}

// Client 微信视频号（诸葛）平台 API 客户端
type Client struct {
	mu          sync.Mutex
	tagCache    map[string]model.TagMeta
	cfg         *config.Config
	accountRepo *repository.PlatformAccountRepo
	tagRepo     *repository.ZhugeTagRepo
	client      *http.Client
}

func NewClient(cfg *config.Config, accountRepo *repository.PlatformAccountRepo, tagRepo *repository.ZhugeTagRepo) *Client {
	return &Client{
		cfg:         cfg,
		accountRepo: accountRepo,
		tagRepo:     tagRepo,
		tagCache:    make(map[string]model.TagMeta),
		client:      &http.Client{Timeout: 30 * time.Second},
	}
}

// ── 配置解析 ──

func ParseAccountConfig(account *model.PlatformAccount) (*model.WeixinAccountConfig, error) {
	var cfg model.WeixinAccountConfig
	if err := json.Unmarshal(account.AccountConfig, &cfg); err != nil {
		return nil, fmt.Errorf("parse weixin config for account %d: %w", account.ID, err)
	}
	return &cfg, nil
}

func (c *Client) saveAccountConfig(accountID int64, wCfg *model.WeixinAccountConfig) error {
	data, err := json.Marshal(wCfg)
	if err != nil {
		return err
	}
	return c.accountRepo.UpdateAccountConfig(accountID, data)
}

// ── Token 管理 ──

// EnsureToken 确保账号 token 有效，过期则 re-login
func (c *Client) EnsureToken(account *model.PlatformAccount) (string, *model.WeixinAccountConfig, error) {
	wCfg, err := ParseAccountConfig(account)
	if err != nil {
		return "", nil, err
	}

	refreshDuration := time.Duration(c.cfg.ZhugeTokenRefreshHours) * time.Hour

	if wCfg.Token != "" && wCfg.TokenExpiresAt != "" {
		exp, _ := time.Parse(time.RFC3339, wCfg.TokenExpiresAt)
		if time.Until(exp) > 0 {
			return wCfg.Token, wCfg, nil
		}
	}

	token, err := c.login(wCfg)
	if err != nil {
		return "", nil, err
	}

	expiresAt := time.Now().Add(refreshDuration)
	wCfg.Token = token
	wCfg.TokenExpiresAt = expiresAt.Format(time.RFC3339)
	if err := c.saveAccountConfig(account.ID, wCfg); err != nil {
		log.Printf("service=weixin action=persist_token account=%d error=%v", account.ID, err)
	}

	return token, wCfg, nil
}

func (c *Client) login(wCfg *model.WeixinAccountConfig) (string, error) {
	url := strings.TrimRight(c.cfg.ZhugeLoginURL, "/") + "/api/v1/user-info/login"

	var result Response[string]
	if err := c.rawRequest("POST", url, LoginReq{
		Username: wCfg.Account,
		Mobile:   wCfg.Account,
		Password: wCfg.Password,
	}, "", &result); err != nil {
		return "", fmt.Errorf("weixin login %s: %w", wCfg.Account, err)
	}

	if result.Code != 200 {
		return "", fmt.Errorf("weixin login %s: code=%d, msg=%s", wCfg.Account, result.Code, result.Msg)
	}
	if result.Data == "" {
		return "", fmt.Errorf("weixin login %s: empty token", wCfg.Account)
	}

	log.Printf("service=weixin action=login account=%s result=success", wCfg.Account)
	return result.Data, nil
}

// GetTokenForAccount 获取账号 token
func (c *Client) GetTokenForAccount(accountID string) (*model.PlatformAccount, *model.WeixinAccountConfig, string, error) {
	aid, err := strconv.ParseInt(accountID, 10, 64)
	if err != nil {
		return nil, nil, "", fmt.Errorf("invalid account id: %s", accountID)
	}
	account, err := c.accountRepo.GetByID(aid)
	if err != nil || account == nil {
		return nil, nil, "", fmt.Errorf("account not found: %s", accountID)
	}
	token, wCfg, err := c.EnsureToken(account)
	if err != nil {
		return nil, nil, "", err
	}
	return account, wCfg, token, nil
}

// ── 通用请求 ──

func (c *Client) rawRequest(method, url string, body any, token string, out any) error {
	var reqBody io.Reader
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		reqBody = bytes.NewReader(bodyBytes)
	}

	hasToken := token != ""
	log.Printf("service=weixin action=request_out method=%s url=%s has_token=%v request=%s", method, url, hasToken, string(bodyBytes))

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	for k, v := range defaultHeaders {
		req.Header.Set(k, v)
	}
	if token != "" {
		req.Header.Set("token", token)
		req.Header.Set("platform_type", "COMPANY")
	}

	start := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		log.Printf("service=weixin action=request_in method=%s url=%s duration=%v error=%v", method, url, time.Since(start), err)
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	respPreview := string(respBody)
	if len(respPreview) > 500 {
		respPreview = respPreview[:500] + "..."
	}
	log.Printf("service=weixin action=request_in method=%s url=%s status=%d duration=%v response=%s", method, url, resp.StatusCode, time.Since(start), respPreview)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d, body: %s", resp.StatusCode, string(respBody))
	}

	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("unmarshal: %w (body: %s)", err, string(respBody))
	}

	return nil
}

// RequestWithAccount 带 token 的请求，自动重试 401
func (c *Client) RequestWithAccount(account *model.PlatformAccount, method, url string, body any, out any) error {
	token, wCfg, err := c.EnsureToken(account)
	if err != nil {
		return err
	}

	for attempt := range 2 {
		err := c.rawRequest(method, url, body, token, out)
		if err != nil {
			if attempt == 0 && strings.Contains(err.Error(), "status 401") {
				log.Printf("service=weixin action=re_auth account=%d reason=http_401", account.ID)
				wCfg.Token = ""
				_ = c.saveAccountConfig(account.ID, wCfg)
				token, wCfg, err = c.EnsureToken(account)
				if err != nil {
					return fmt.Errorf("re-auth: %w", err)
				}
				continue
			}
			return err
		}

		type codeChecker struct {
			Code int `json:"code"`
		}
		var cc codeChecker
		b, _ := json.Marshal(out)
		_ = json.Unmarshal(b, &cc)
		if cc.Code == 401 && attempt == 0 {
			log.Printf("service=weixin action=re_auth account=%d reason=body_code_401", account.ID)
			wCfg.Token = ""
			_ = c.saveAccountConfig(account.ID, wCfg)
			token, wCfg, err = c.EnsureToken(account)
			if err != nil {
				return fmt.Errorf("re-auth: %w", err)
			}
			continue
		}

		return nil
	}

	return fmt.Errorf("weixin request exhausted retries: %s %s", method, url)
}

// Request 通过 accountID 查账号再调 API
func (c *Client) Request(accountID, method, url string, body any, out any) error {
	account, _, _, err := c.GetTokenForAccount(accountID)
	if err != nil {
		return err
	}
	return c.RequestWithAccount(account, method, url, body, out)
}

// ── API 方法 ──

func (c *Client) GetHistoryAuthors(accountID string) ([]HistoryAuthor, error) {
	account, wCfg, _, err := c.GetTokenForAccount(accountID)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(c.cfg.ZhugeAPIBase, "/") + "/api/v1/wechat/history-author"
	var resp Response[[]HistoryAuthor]
	if err := c.RequestWithAccount(account, "POST", url, HistoryAuthorReq{
		TsUserList: []string{wCfg.TsUserID},
	}, &resp); err != nil {
		return nil, fmt.Errorf("weixin: get history authors: %w", err)
	}
	return resp.Data, nil
}

func (c *Client) GetAuthorVideos(accountID, username, beginDate, endDate string) ([]AuthorVideo, error) {
	account, wCfg, _, err := c.GetTokenForAccount(accountID)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(c.cfg.ZhugeAPIBase, "/") + "/api/v1/wechat/author-videos"
	reqType := 2
	if beginDate != "" {
		reqType = 3
	}
	var resp Response[[]AuthorVideo]
	if err := c.RequestWithAccount(account, "POST", url, AuthorVideosReq{
		Type:      reqType,
		UserName:  username,
		TsUserID:  wCfg.TsUserID,
		BeginDate: beginDate,
		EndDate:   endDate,
	}, &resp); err != nil {
		return nil, fmt.Errorf("weixin: get author videos: %w", err)
	}
	log.Printf("service=weixin action=get_author_videos account=%s username=%s response_count=%d", accountID, username, len(resp.Data))
	return resp.Data, nil
}

func (c *Client) GetInterestTags(accountID string) ([]InterestTag, error) {
	account, wCfg, _, err := c.GetTokenForAccount(accountID)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(c.cfg.ZhugeAPIBase, "/") + "/api/v3/wechat/interest-tags"
	var resp Response[[]InterestTag]
	if err := c.RequestWithAccount(account, "POST", url, InterestTagsReq{
		ExportIds: []string{},
		TsUserID:  wCfg.TsUserID,
	}, &resp); err != nil {
		return nil, fmt.Errorf("weixin: get interest tags: %w", err)
	}
	return resp.Data, nil
}

func (c *Client) GetFlatTags(accountID string) (map[string]model.TagMeta, error) {
	c.mu.Lock()
	if len(c.tagCache) > 0 {
		cache := make(map[string]model.TagMeta, len(c.tagCache))
		for k, v := range c.tagCache {
			cache[k] = v
		}
		c.mu.Unlock()
		return cache, nil
	}
	c.mu.Unlock()

	dbMap, err := c.tagRepo.GetFlat()
	if err == nil && len(dbMap) > 0 {
		c.mu.Lock()
		c.tagCache = dbMap
		c.mu.Unlock()
		return dbMap, nil
	}

	tags, err := c.GetInterestTags(accountID)
	if err != nil {
		return nil, fmt.Errorf("weixin: get interest tags: %w", err)
	}

	flatMap := make(map[string]model.TagMeta)
	var dbTags []model.ZhugeTag
	for _, tag := range tags {
		if len(tag.ChildTags) > 0 {
			for _, child := range tag.ChildTags {
				flatMap[child.Text] = model.TagMeta{ID: child.ID, WxLevel: child.Level}
				pid, ptext := tag.ID, tag.Text
				dbTags = append(dbTags, model.ZhugeTag{
					ID: child.ID, Text: child.Text,
					ParentID: &pid, ParentText: &ptext,
					WxLevel: child.Level,
				})
			}
		} else {
			wxLevel := tag.Level
			if wxLevel == 0 {
				wxLevel = 99
			}
			flatMap[tag.Text] = model.TagMeta{ID: tag.ID, WxLevel: wxLevel}
			dbTags = append(dbTags, model.ZhugeTag{
				ID: tag.ID, Text: tag.Text, WxLevel: wxLevel,
			})
		}
	}

	if len(dbTags) > 0 {
		if err := c.tagRepo.SaveBulk(dbTags); err != nil {
			log.Printf("service=weixin action=persist_tags error=%v", err)
		}
	}

	c.mu.Lock()
	c.tagCache = flatMap
	c.mu.Unlock()

	log.Printf("service=weixin action=load_tags source=api count=%d", len(flatMap))
	return flatMap, nil
}

func (c *Client) CreatePlan(accountID string, planData any) (string, json.RawMessage, error) {
	url := strings.TrimRight(c.cfg.ZhugeAPIBase, "/") + "/api/v5/wechat/create-plan"
	var resp Response[string]
	if err := c.Request(accountID, "POST", url, planData, &resp); err != nil {
		return "", nil, fmt.Errorf("weixin: create plan: %w", err)
	}
	if resp.Code != 200 {
		raw, _ := json.Marshal(resp)
		return "", raw, fmt.Errorf("weixin create plan: code=%d, msg=%s", resp.Code, resp.Msg)
	}
	raw, _ := json.Marshal(resp)
	return resp.Data, raw, nil
}

func (c *Client) GetBatchOrders(accountID, batchID string) (*BatchData, json.RawMessage, error) {
	url := strings.TrimRight(c.cfg.ZhugeAPIBase, "/") + "/api/v5/wechat/batch-data-list/" + batchID
	var resp Response[BatchData]
	if err := c.Request(accountID, "POST", url, struct{}{}, &resp); err != nil {
		return nil, nil, fmt.Errorf("weixin: get batch orders: %w", err)
	}
	raw, _ := json.Marshal(resp)
	return &resp.Data, raw, nil
}

func (c *Client) GetPlanDetail(accountID, promotionID string) (*PlanDetail, error) {
	url := strings.TrimRight(c.cfg.ZhugeLoginURL, "/") + "/api/v1/video-plan/detail/" + promotionID
	var resp Response[PlanDetail]
	if err := c.Request(accountID, "GET", url, nil, &resp); err != nil {
		return nil, fmt.Errorf("weixin: get plan detail: %w", err)
	}
	return &resp.Data, nil
}

func (c *Client) GetPlanRecord(accountID, promotionID string) ([]PlanRecord, error) {
	url := strings.TrimRight(c.cfg.ZhugeLoginURL, "/") + "/api/v1/video-plan/plan_record/" + promotionID
	var resp Response[[]PlanRecord]
	if err := c.Request(accountID, "GET", url, nil, &resp); err != nil {
		return nil, fmt.Errorf("weixin: get plan record: %w", err)
	}
	return resp.Data, nil
}

func (c *Client) PollPaymentStatus(accountID, promotionID string) (*PayStatus, json.RawMessage, error) {
	url := strings.TrimRight(c.cfg.ZhugeAPIBase, "/") + "/api/v5/wechat/poll_plan_pay_status/" + promotionID
	var resp Response[PayStatus]
	if err := c.Request(accountID, "GET", url, nil, &resp); err != nil {
		return nil, nil, fmt.Errorf("weixin: poll payment status: %w", err)
	}
	raw, _ := json.Marshal(resp)
	return &resp.Data, raw, nil
}

// ClosePlan 关停投放计划
func (c *Client) ClosePlan(accountID, promotionID string) error {
	url := strings.TrimRight(c.cfg.ZhugeLoginURL, "/") + "/api/v1/ts-plan-info/close-plan/" + promotionID
	var resp Response[any]
	if err := c.Request(accountID, "POST", url, nil, &resp); err != nil {
		return fmt.Errorf("weixin: close plan %s: %w", promotionID, err)
	}
	if resp.Code != 200 {
		return fmt.Errorf("weixin: close plan %s: code=%d msg=%s", promotionID, resp.Code, resp.Msg)
	}
	log.Printf("service=weixin action=close_plan promotion=%s result=success", promotionID)
	return nil
}
