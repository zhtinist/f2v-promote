package weixin

import "encoding/json"

// ── 请求结构体 ──

type LoginReq struct {
	Username string `json:"username"`
	Mobile   string `json:"mobile"`
	Password string `json:"password"`
}

type HistoryAuthorReq struct {
	TsUserList []string `json:"ts_user_list"`
}

type AuthorVideosReq struct {
	Type      int    `json:"type"`
	UserName  string `json:"userName"`
	TsUserID  string `json:"ts_user_id"`
	BeginDate string `json:"begin_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
}

type InterestTagsReq struct {
	ExportIds []string `json:"exportIds"`
	TsUserID  string   `json:"ts_user_id"`
}

// ── 通用响应 ──

type Response[T any] struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	BizCode int    `json:"biz_code"`
	Data    T      `json:"data"`
}

// ── 历史合作作者 ──

type HistoryAuthor struct {
	Username       string `json:"username"`
	AuthImgURL     string `json:"authImgUrl"`
	AuthProfession string `json:"authProfession"`
	HeadImgURL     string `json:"headImgUrl"`
	LiveStatus     int    `json:"liveStatus"`
	Permission     int    `json:"permission"`
	Signature      string `json:"signature"`
	NickName       string `json:"nickName"`
}

// ── 作者视频 ──

type AuthorVideo struct {
	ExportID     string              `json:"exportId"`
	Description  string              `json:"description"`
	CoverURL     string              `json:"coverUrl"`
	CreateTime   int64               `json:"createTime"`
	DeleteTime   int64               `json:"deleteTime"`
	Nonce        string              `json:"nonce"`
	ReadCount    int64               `json:"readCount"`
	CommentCount int64               `json:"commentCount"`
	FavCount     int64               `json:"favCount"`
	ForwardCount int64               `json:"forwardCount"`
	LikeCount    int64               `json:"likeCount"`
	AccountInfo  *AuthorVideoAccount `json:"accountInfo"`
	Component    *FinderComponent    `json:"finderComponent"`
	ShoppingCart *ShoppingCartJump   `json:"shoppingcartJumpinfo"`
	Flag         *AuthorVideoFlag    `json:"flag"`
}

type AuthorVideoAccount struct {
	AuthImgURL      string `json:"authImgUrl"`
	AuthProfession  string `json:"authProfession"`
	HeadImgURL      string `json:"headImgUrl"`
	LiveStatus      int    `json:"liveStatus"`
	NickName        string `json:"nickName"`
	Permission      int    `json:"permission"`
	ROI2PromoteFlag int    `json:"roi2PromoteFlag"`
	Signature       string `json:"signature"`
	Username        string `json:"username"`
}

type FinderComponent struct {
	Wording         string       `json:"wording"`
	Type            int          `json:"type"`
	IconURL         string       `json:"icon_url"`
	MiniAppJumpInfo *MiniAppJump `json:"miniAppJumpInfo"`
}

type MiniAppJump struct {
	Path        string `json:"path"`
	FetchInfoID string `json:"fetchInfoId"`
	AppID       string `json:"appId"`
}

type ShoppingCartJump struct {
	Wording   string `json:"wording"`
	ProductID string `json:"productId"`
	Path      string `json:"path"`
}

type AuthorVideoFlag struct {
	HasLiveNotice              bool `json:"hasLiveNotice"`
	EnableTargetComponentClick bool `json:"enableTargetComponentClick"`
	HasShoppingCart             bool `json:"hasShoppingCart"`
}

// ── 兴趣标签 ──

type InterestTag struct {
	ID        string        `json:"id"`
	Text      string        `json:"text"`
	Level     int           `json:"level"`
	ChildTags []InterestTag `json:"childTags"`
}

// ── 投放计划 ──

type BatchData struct {
	List       []BatchOrder `json:"list"`
	PlanPayRes PlanPay      `json:"plan_pay_res"`
	SuccNum    int          `json:"succ_num"`
	ErrNum     int          `json:"err_num"`
}

type BatchOrder struct {
	ID             string `json:"id"`
	CreateErrorMsg string `json:"create_error_msg"`
	Status         int    `json:"status"`
	CreateFlag     int    `json:"create_flag"`
}

type PlanPay struct {
	OrderID   string `json:"order_id"`
	PayURL    string `json:"pay_url"`
	PayStatus int    `json:"pay_status"`
}

type PlanDetail struct {
	ID                  string       `json:"id"`
	OrderID             string       `json:"order_id"`
	PlanName            string       `json:"plan_name"`
	Status              int          `json:"status"`
	SyncStatus          int          `json:"sync_status"`
	StatusLabel         string       `json:"statusLabel"`
	AutoCloseExecStatus int          `json:"auto_close_exec_status"`
	AutoCloseExecLabel  string       `json:"auto_close_exec_status_label"`
	CoverURL            string       `json:"cover_url"`
	Description         string       `json:"description"`
	AuthorNickname      string       `json:"author_nickname"`
	VCreateTime         string       `json:"v_create_time"`
	CommissionRate      float64      `json:"commissionRate"`
	TfMoney             string       `json:"tf_money"`
	Cost                string       `json:"cost"`
	CostBill            string       `json:"cost_bill"`
	ROI                 string       `json:"roi"`
	CommissionROI       string       `json:"commissionRoi"`
	OrderMoney          string       `json:"orderMoney"`
	OrderNum            int          `json:"order_num"`
	AvgOrderMoney       string       `json:"avgOrderMoney"`
	EmptyMoney          float64      `json:"empty_money"`
	NoCostDuration      string       `json:"noCostDuration"`
	ClosingRatio        string       `json:"closingRatio"`
	CostFocus           string       `json:"costFocus"`
	Profit              string       `json:"profit"`
	ClickNum            int          `json:"click_num"`
	ViewNum             int          `json:"view_num"`
	LikeNum             int          `json:"like_num"`
	CommentNum          int          `json:"comment_num"`
	ShareNum            int          `json:"share_num"`
	FocusNum            int          `json:"focus_num"`
	ClickRate           string       `json:"click_rate"`
	OrderRate           string       `json:"order_rate"`
	CostView            string       `json:"cost_view"`
	CostClick           string       `json:"cost_click"`
	CostOrder           string       `json:"cost_order"`
	CostReserve         string       `json:"cost_reserve"`
	FeedReserveLiveUV   int          `json:"feed_reserve_live_uv"`
	DurationLabel       string       `json:"duration_label"`
	SyncTime            string       `json:"sync_time"`
	CreateTime          string       `json:"create_time"`
	BeginTime           string       `json:"begin_time"`
	BatchNo             string       `json:"batch_no"`
	TsUserName          string       `json:"ts_user_name"`
	PromotionTargetName string       `json:"promotion_target_name"`
	PlanSuggestVO       *PlanSuggest `json:"planSuggestVO"`
	PlanPaymentVO       *PlanPayment `json:"planPaymentVO"`
	AuthorVideoList     json.RawMessage `json:"authorVideoList"`
}

type PlanSuggest struct {
	PromotionTypeLabel   string   `json:"promotion_type_label"`
	PromotionTargetLabel string   `json:"promotion_target_label"`
	PricingTypeLabel     string   `json:"pricing_type_label"`
	DurationLabel        string   `json:"duration_label"`
	DeviceType           string   `json:"device_type"`
	Gender               []string `json:"gender"`
	AgeRange             []string `json:"age_range"`
	CityIDs              []string `json:"city_ids"`
	InterestTag          []string `json:"interest_tag"`
	SimilarAcctList      []string `json:"similar_acct_list"`
}

type PlanPayment struct {
	PaidWecoinAmount              int    `json:"paid_wecoin_amount"`
	RefundWecoinAmount            int    `json:"refund_wecoin_amount"`
	PaidVoucherAmountInCents      int    `json:"paid_voucher_amount_in_cents"`
	RefundVoucherAmountInCents    int    `json:"refund_voucher_amount_in_cents"`
	IndemnifyVoucherAmountInCents int    `json:"indemnify_voucher_amount_in_cents"`
	IndemnifyContent              string `json:"indemnify_content"`
	RefundVoucherContent          string `json:"refund_voucher_content"`
}

type PlanRecord struct {
	CreateTime   string `json:"create_time"`
	Content      string `json:"content"`
	Type         int    `json:"type"`
	TypeLabel    string `json:"typeLabel"`
	OperatorName string `json:"operator_name"`
}

type PayStatus struct {
	PayStatus int `json:"pay_status"`
	Status    int `json:"status"`
}

// ── 投放标签 ──

type TagGroup struct {
	Name            string           `json:"name"`
	InterestTagList []InterestTagRef `json:"interest_tag_list"`
	Ages            []int            `json:"ages"`
	Sex             *int             `json:"sex"`
	CityIDs         []string         `json:"cityIds"`
}

type InterestTagRef struct {
	InterestTag string `json:"interestTag"`
	TagLevel    int    `json:"tagLevel"`
}

// ── 创建投放计划请求体 ──

type CreatePlanRequest struct {
	TsUserList            []string          `json:"ts_user_list"`
	PlanCount             int               `json:"plan_count"`
	WeiDou                int               `json:"wei_dou"`
	PromotionTarget       int               `json:"promotion_target"`
	PricingType           int               `json:"pricing_type"`
	PromotionType         int               `json:"promotion_type"`
	MyPrice               int               `json:"my_price"`
	Sex                   *int              `json:"sex"`
	Ages                  []int             `json:"ages"`
	CityIDs               []string          `json:"cityIds"`
	InterestTagList       []InterestTagRef  `json:"interest_tag_list"`
	TimeType              int               `json:"time_type"`
	UseCouponStrategy     int               `json:"use_coupon_strategy"`
	BillingMethod         int               `json:"billing_method"`
	IsPartner             int               `json:"is_partner"`
	CompensationStatus    int               `json:"compensation_status"`
	CompensationParamType int               `json:"compensation_param_type"`
	CompensationParamVal  int               `json:"compensation_param_val"`
	AutoCloseTemplateIDs  []any             `json:"auto_close_template_ids"`
	MorePriceList         []any             `json:"morePriceList"`
	SimilarAuthorList     []any             `json:"similar_author_list"`
	SimilarUsernameList   []any             `json:"similarUsernameList"`
	Portrait              bool              `json:"portrait"`
	DeviceType            *string           `json:"deviceType"`
	GroupID               any               `json:"group_id"`
	InterestTag           []any             `json:"interestTag"`
	ExportID              string            `json:"exportId"`
	AuthorVideoList       []json.RawMessage `json:"authorVideoList"`
	Author                json.RawMessage   `json:"author"`
	SeqNoList             []string          `json:"seq_no_list"`
}
