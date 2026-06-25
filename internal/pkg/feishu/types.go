package feishu

import "encoding/json"

// SheetInfo 飞书电子表格 sheet 信息
type SheetInfo struct {
	SheetID string `json:"sheet_id"`
	Title   string `json:"title"`
	Index   int    `json:"index"`
}

// FeishuResponse 飞书 API 通用响应
type FeishuResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// TokenResponse tenant_access_token 响应
type TokenResponse struct {
	Code              int    `json:"code"`
	Msg               string `json:"msg"`
	TenantAccessToken string `json:"tenant_access_token"`
	Expire            int    `json:"expire"` // seconds
}

// CreateSpreadsheetResp 创建电子表格响应
type CreateSpreadsheetResp struct {
	Spreadsheet struct {
		SpreadsheetToken string `json:"spreadsheet_token"`
		Title            string `json:"title"`
	} `json:"spreadsheet"`
}

// SheetsQueryResp 获取 sheet 列表响应
type SheetsQueryResp struct {
	Sheets []SheetInfo `json:"sheets"`
}

// BatchUpdateResp sheets_batch_update 响应
type BatchUpdateResp struct {
	Replies []struct {
		AddSheet struct {
			Properties struct {
				SheetID string `json:"sheetId"`
				Title   string `json:"title"`
				Index   int    `json:"index"`
			} `json:"properties"`
		} `json:"addSheet"`
	} `json:"replies"`
}

// CellStyleItem 单元格样式项（用于批量设置样式）
type CellStyleItem struct {
	Ranges []string               `json:"ranges"`
	Style  map[string]interface{} `json:"style"`
}

// AppendResponse values_append 响应
type AppendResponse struct {
	SpreadsheetToken string `json:"spreadsheetToken"`
	Updates          struct {
		UpdatedRange string `json:"updatedRange"` // 如 "sheetID!A5:V8"
		UpdatedRows  int    `json:"updatedRows"`
	} `json:"updates"`
}
