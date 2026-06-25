package feishu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

const (
	baseURL  = "https://open.feishu.cn/open-apis"
	tokenURL = baseURL + "/auth/v3/tenant_access_token/internal/"
)

// Client 飞书 API 客户端
type Client struct {
	appID     string
	appSecret string
	token     string
	tokenExp  time.Time
	mu        sync.Mutex // 保护 token 字段
	client    *http.Client
}

// NewClient 创建飞书客户端
func NewClient(appID, appSecret string) *Client {
	return &Client{
		appID:     appID,
		appSecret: appSecret,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// EnsureToken 确保 tenant_access_token 有效
func (c *Client) EnsureToken() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Now().Before(c.tokenExp) {
		return nil
	}

	body, _ := json.Marshal(map[string]string{
		"app_id":     c.appID,
		"app_secret": c.appSecret,
	})

	resp, err := c.client.Post(tokenURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("feishu token request: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("feishu token decode: %w", err)
	}

	if tokenResp.Code != 0 {
		return fmt.Errorf("feishu token error: code=%d msg=%s", tokenResp.Code, tokenResp.Msg)
	}

	c.token = tokenResp.TenantAccessToken
	c.tokenExp = time.Now().Add(time.Duration(tokenResp.Expire-60) * time.Second) // 提前 60s 刷新
	log.Printf("service=feishu action=refresh_token expires_in=%ds", tokenResp.Expire)
	return nil
}

// doRequest 通用 HTTP 请求
func (c *Client) doRequest(method, url string, body interface{}) ([]byte, error) {
	if err := c.EnsureToken(); err != nil {
		return nil, err
	}

	var reqBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("feishu new request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("feishu request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("feishu read response: %w", err)
	}

	// 检查通用错误
	var feishuResp FeishuResponse
	if err := json.Unmarshal(respBody, &feishuResp); err == nil {
		if feishuResp.Code != 0 {
			return nil, fmt.Errorf("feishu API error: code=%d msg=%s url=%s", feishuResp.Code, feishuResp.Msg, url)
		}
	}

	return respBody, nil
}

// CreateFolder 在指定父文件夹下创建子文件夹
func (c *Client) CreateFolder(parentToken, name string) (string, error) {
	url := baseURL + "/drive/v1/files/create_folder"

	body := map[string]interface{}{
		"name":         name,
		"folder_token": parentToken,
	}

	respBody, err := c.doRequest("POST", url, body)
	if err != nil {
		return "", err
	}

	var result struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("feishu create folder decode: %w", err)
	}

	log.Printf("service=feishu action=create_folder folder=%s name=%s parent=%s", result.Data.Token, name, parentToken)
	return result.Data.Token, nil
}

// CreateSpreadsheet 在指定文件夹下创建电子表格
func (c *Client) CreateSpreadsheet(folderToken, title string) (string, error) {
	url := baseURL + "/sheets/v3/spreadsheets"

	body := map[string]interface{}{
		"title":        title,
		"folder_token": folderToken,
	}

	respBody, err := c.doRequest("POST", url, body)
	if err != nil {
		return "", err
	}

	var result struct {
		Data CreateSpreadsheetResp `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("feishu create spreadsheet decode: %w", err)
	}

	token := result.Data.Spreadsheet.SpreadsheetToken
	log.Printf("service=feishu action=create_spreadsheet token=%s title=%s", token, title)
	return token, nil
}

// GetSheets 获取电子表格所有 sheet 列表
func (c *Client) GetSheets(spreadsheetToken string) ([]SheetInfo, error) {
	url := fmt.Sprintf("%s/sheets/v3/spreadsheets/%s/sheets/query", baseURL, spreadsheetToken)

	respBody, err := c.doRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data SheetsQueryResp `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("feishu get sheets decode: %w", err)
	}

	return result.Data.Sheets, nil
}

// CreateSheet 新增 sheet 页（追加到末尾）
func (c *Client) CreateSheet(spreadsheetToken, title string, index int) (string, error) {
	url := fmt.Sprintf("%s/sheets/v2/spreadsheets/%s/sheets_batch_update", baseURL, spreadsheetToken)

	body := map[string]interface{}{
		"requests": []map[string]interface{}{
			{
				"addSheet": map[string]interface{}{
					"properties": map[string]interface{}{
						"title":       title,
						"index":       index,
						"columnCount": 26,
					},
				},
			},
		},
	}

	respBody, err := c.doRequest("POST", url, body)
	if err != nil {
		return "", err
	}

	var result struct {
		Data BatchUpdateResp `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("feishu create sheet decode: %w", err)
	}

	if len(result.Data.Replies) == 0 {
		return "", fmt.Errorf("feishu create sheet: empty replies")
	}

	sheetID := result.Data.Replies[0].AddSheet.Properties.SheetID
	log.Printf("service=feishu action=create_sheet sheet=%s title=%s spreadsheet=%s", sheetID, title, spreadsheetToken)
	return sheetID, nil
}

// DeleteSheet 删除指定 sheet 页
func (c *Client) DeleteSheet(spreadsheetToken, sheetID string) error {
	url := fmt.Sprintf("%s/sheets/v2/spreadsheets/%s/sheets_batch_update", baseURL, spreadsheetToken)

	body := map[string]interface{}{
		"requests": []map[string]interface{}{
			{
				"deleteSheet": map[string]interface{}{
					"sheetId": sheetID,
				},
			},
		},
	}

	_, err := c.doRequest("POST", url, body)
	return err
}

// WriteHeader 写入表头行（第一行）
func (c *Client) WriteHeader(spreadsheetToken, sheetID string, headers []string) error {
	url := fmt.Sprintf("%s/sheets/v2/spreadsheets/%s/values", baseURL, spreadsheetToken)

	// 构建 header 行为 interface 切片
	headerRow := make([]interface{}, len(headers))
	for i, h := range headers {
		headerRow[i] = h
	}

	body := map[string]interface{}{
		"valueRange": map[string]interface{}{
			"range":  fmt.Sprintf("%s!A2:%s2", sheetID, colLetter(len(headers))),
			"values": [][]interface{}{headerRow},
		},
	}

	_, err := c.doRequest("PUT", url, body)
	return err
}

// WriteRow 在指定行写入一行数据
func (c *Client) WriteRow(spreadsheetToken, sheetID string, row int, data []interface{}) error {
	url := fmt.Sprintf("%s/sheets/v2/spreadsheets/%s/values", baseURL, spreadsheetToken)
	body := map[string]interface{}{
		"valueRange": map[string]interface{}{
			"range":  fmt.Sprintf("%s!A%d:%s%d", sheetID, row, colLetter(len(data)), row),
			"values": [][]interface{}{data},
		},
	}
	_, err := c.doRequest("PUT", url, body)
	return err
}

// WriteTitleRow 在第1行写入视频描述并合并单元格
func (c *Client) WriteTitleRow(spreadsheetToken, sheetID, title string, colCount int) error {
	// 写入标题到 A1
	url := fmt.Sprintf("%s/sheets/v2/spreadsheets/%s/values", baseURL, spreadsheetToken)
	body := map[string]interface{}{
		"valueRange": map[string]interface{}{
			"range":  fmt.Sprintf("%s!A1:%s1", sheetID, colLetter(colCount)),
			"values": [][]interface{}{{title}},
		},
	}
	if _, err := c.doRequest("PUT", url, body); err != nil {
		return err
	}

	// 合并 A1 到最后一列
	mergeRange := fmt.Sprintf("%s!A1:%s1", sheetID, colLetter(colCount))
	return c.MergeCells(spreadsheetToken, mergeRange)
}

// MergeCells 合并指定范围的单元格
func (c *Client) MergeCells(spreadsheetToken, mergeRange string) error {
	url := fmt.Sprintf("%s/sheets/v2/spreadsheets/%s/merge_cells", baseURL, spreadsheetToken)
	body := map[string]interface{}{
		"range":     mergeRange,
		"mergeType": "MERGE_ALL",
	}
	_, err := c.doRequest("POST", url, body)
	return err
}

// AppendRows 追加数据行，返回写入范围（如 "sheetID!A5:V8"）
func (c *Client) AppendRows(spreadsheetToken, sheetID string, rows [][]interface{}) (string, error) {
	if len(rows) == 0 {
		return "", nil
	}

	url := fmt.Sprintf("%s/sheets/v2/spreadsheets/%s/values_append", baseURL, spreadsheetToken)

	colCount := len(rows[0])
	body := map[string]interface{}{
		"valueRange": map[string]interface{}{
			"range":  fmt.Sprintf("%s!A:%s", sheetID, colLetter(colCount)),
			"values": rows,
		},
	}

	respBody, err := c.doRequest("POST", url, body)
	if err != nil {
		return "", err
	}

	var result struct {
		Data AppendResponse `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		log.Printf("service=feishu action=append_rows row_count=%d sheet=%s spreadsheet=%s (parse_range_failed)", len(rows), sheetID, spreadsheetToken)
		return "", nil // 写入成功但解析范围失败，不影响主流程
	}

	updatedRange := result.Data.Updates.UpdatedRange
	log.Printf("service=feishu action=append_rows row_count=%d sheet=%s spreadsheet=%s range=%s", len(rows), sheetID, spreadsheetToken, updatedRange)
	return updatedRange, nil
}

// BatchSetCellStyle 批量设置单元格样式（背景色等）
func (c *Client) BatchSetCellStyle(spreadsheetToken string, items []CellStyleItem) error {
	if len(items) == 0 {
		return nil
	}
	url := fmt.Sprintf("%s/sheets/v2/spreadsheets/%s/styles_batch_update", baseURL, spreadsheetToken)
	body := map[string]interface{}{
		"data": items,
	}
	_, err := c.doRequest("PUT", url, body)
	if err != nil {
		log.Printf("service=feishu action=batch_set_style error=%v items=%d", err, len(items))
	}
	return err
}

// colLetter 将列数转为字母 (1→A, 16→P, 27→AA)
func colLetter(n int) string {
	result := ""
	for n > 0 {
		n-- // 转为 0-based
		result = string(rune('A'+n%26)) + result
		n /= 26
	}
	return result
}

// SendCardToChat 向指定群组发送交互卡片
// chatID: 群组 ID（open_chat_id）
// card: 卡片内容（由 card.go 构建）
func (c *Client) SendCardToChat(chatID string, card map[string]interface{}) (string, error) {
	url := baseURL + "/im/v1/messages?receive_id_type=chat_id"

	cardJSON, err := json.Marshal(card)
	if err != nil {
		return "", fmt.Errorf("feishu marshal card: %w", err)
	}

	body := map[string]interface{}{
		"receive_id": chatID,
		"msg_type":   "interactive",
		"content":    string(cardJSON),
	}

	respBody, err := c.doRequest("POST", url, body)
	if err != nil {
		return "", err
	}

	var result struct {
		Data struct {
			MessageID string `json:"message_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("feishu send card decode: %w", err)
	}

	log.Printf("service=feishu action=send_card chat_id=%s message_id=%s", chatID, result.Data.MessageID)
	return result.Data.MessageID, nil
}

// GetUserNameFromChat 通过群成员列表获取用户姓名（需 im:chat:readonly 权限）
func (c *Client) GetUserNameFromChat(chatID, openID string) string {
	url := fmt.Sprintf("%s/im/v1/chats/%s/members?member_id_type=open_id&page_size=100", baseURL, chatID)

	respBody, err := c.doRequest("GET", url, nil)
	if err != nil {
		log.Printf("service=feishu action=get_chat_members error=%v chat_id=%s", err, chatID)
		return openID
	}

	var result struct {
		Data struct {
			Items []struct {
				MemberID string `json:"member_id"`
				Name     string `json:"name"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return openID
	}

	for _, m := range result.Data.Items {
		if m.MemberID == openID {
			return m.Name
		}
	}
	return openID
}
