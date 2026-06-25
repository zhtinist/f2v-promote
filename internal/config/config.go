package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds all application settings loaded from .env.
type Config struct {
	AppPort string
	AppName string
	Debug   bool

	// MySQL
	MySQLHost     string
	MySQLPort     string
	MySQLUser     string
	MySQLPassword string
	MySQLDatabase string

	// Auth / Session
	SessionSecret string
	AdminToken    string
	MaxUsers      int

	// OpenAI
	OpenAIAPIKey     string
	OpenAIModel      string
	OpenAIMaxRetries int
	OpenAIRetryDelay int // seconds
	OpenAIBaseURL    string

	// Tag
	TagGroupCount    int
	TagCategoryCount int // Step 1 选择的一级分类数量

	// Zhuge
	ZhugeAPIBase             string
	ZhugeLoginURL            string
	ZhugeAccount             string
	ZhugePassword            string
	ZhugeTsUserID            string
	ZhugeWeiDou              int
	ZhugePlanInterval        int // seconds
	ZhugeMaxPlansPer5Min     int
	ZhugeTokenRefreshHours   int
	ZhugePaymentTimeoutHours int
	ZhugeGroupID             string

	// OSS
	OSSAccessKeyID     string
	OSSAccessKeySecret string
	OSSEndpoint        string
	OSSBucket          string
	OSSDomain          string

	// Notification
	WebhookURL string

	// Logging
	LogDir   string
	LogLevel string

	// Basic Auth (optional, empty = disabled)
	BasicAuthUser string
	BasicAuthPass string

	// Feishu
	FeishuAppID             string
	FeishuAppSecret         string
	FeishuVerificationToken string
	FeishuChatID            string

	// Auto Promote
	AutoPromoteEnabled          bool
	AutoPromoteVideoRangeDays   int
	AutoPromoteVideoCooldownSec int

	// MNS
	MNSEndpoint        string
	MNSAccessKeyID     string
	MNSAccessKeySecret string
	MNSQueueName       string
}

// DSN returns the MySQL data source name for GORM.
func (c *Config) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		c.MySQLUser, c.MySQLPassword, c.MySQLHost, c.MySQLPort, c.MySQLDatabase)
}

// Load reads the .env file (if present) and populates a Config from environment variables.
func Load() *Config {
	// Ignore error – .env may not exist in production.
	_ = godotenv.Load()

	return &Config{
		AppPort: envStr("APP_PORT", ":9000"),
		AppName: envStr("APP_NAME", "f2v-promote"),
		Debug:   envBool("DEBUG", false),

		MySQLHost:     envStr("MYSQL_HOST", "127.0.0.1"),
		MySQLPort:     envStr("MYSQL_PORT", "3306"),
		MySQLUser:     envStr("MYSQL_USER", "root"),
		MySQLPassword: envStr("MYSQL_PASSWORD", ""),
		MySQLDatabase: envStr("MYSQL_DATABASE", "f2v_promote"),

		SessionSecret: envStr("SESSION_SECRET", "change-me"),
		AdminToken:    envStr("ADMIN_TOKEN", ""),
		MaxUsers:      envInt("MAX_USERS", 50),

		OpenAIBaseURL:    envStr("OPENAI_BASE_URL", ""),
		OpenAIAPIKey:     envStr("OPENAI_API_KEY", ""),
		OpenAIModel:      envStr("OPENAI_MODEL", "gpt-4o"),
		OpenAIMaxRetries: envInt("OPENAI_MAX_RETRIES", 3),
		OpenAIRetryDelay: envInt("OPENAI_RETRY_DELAY", 2),
		

		TagGroupCount:    envInt("TAG_GROUP_COUNT", 1),
		TagCategoryCount: envInt("TAG_CATEGORY_COUNT", 5),

		ZhugeAPIBase:             envStr("ZHUGE_API_BASE", "https://zhuge-login.v-ma.net"),
		ZhugeLoginURL:            envStr("ZHUGE_LOGIN_URL", "https://zhuge-api.v-ma.net"),
		ZhugeAccount:             envStr("ZHUGE_ACCOUNT", ""),
		ZhugePassword:            envStr("ZHUGE_PASSWORD", ""),
		ZhugeTsUserID:            envStr("ZHUGE_TS_USER_ID", ""),
		ZhugeWeiDou:              envInt("ZHUGE_WEI_DOU", 500),
		ZhugePlanInterval:        envInt("ZHUGE_PLAN_INTERVAL", 10),
		ZhugeMaxPlansPer5Min:     envInt("ZHUGE_MAX_PLANS_PER_5MIN", 5),
		ZhugeTokenRefreshHours:   envInt("ZHUGE_TOKEN_REFRESH_HOURS", 20),
		ZhugePaymentTimeoutHours: envInt("ZHUGE_PAYMENT_TIMEOUT_HOURS", 2),
		ZhugeGroupID:             envStr("ZHUGE_GROUP_ID", ""),

		OSSAccessKeyID:     envStr("OSS_ACCESS_KEY_ID", ""),
		OSSAccessKeySecret: envStr("OSS_ACCESS_KEY_SECRET", ""),
		OSSEndpoint:        envStr("OSS_ENDPOINT", ""),
		OSSBucket:          envStr("OSS_BUCKET", ""),
		OSSDomain:          envStr("OSS_DOMAIN", ""),

		WebhookURL: envStr("WEBHOOK_URL", ""),

		LogDir:   envStr("LOG_DIR", "logs"),
		LogLevel: envStr("LOG_LEVEL", "INFO"),

		BasicAuthUser: envStr("BASIC_AUTH_USER", ""),
		BasicAuthPass: envStr("BASIC_AUTH_PASS", ""),

		FeishuAppID:             envStr("FEISHU_APP_ID", ""),
		FeishuAppSecret:         envStr("FEISHU_APP_SECRET", ""),
		FeishuVerificationToken: envStr("FEISHU_VERIFICATION_TOKEN", ""),
		FeishuChatID:            envStr("FEISHU_CHAT_ID", ""),

		AutoPromoteEnabled:          envBool("AUTO_PROMOTE_ENABLED", false),
		AutoPromoteVideoRangeDays:   envInt("AUTO_PROMOTE_VIDEO_RANGE_DAYS", 30),
		AutoPromoteVideoCooldownSec: envInt("AUTO_PROMOTE_VIDEO_COOLDOWN_SEC", 60),

		MNSEndpoint:        envStr("MNS_ENDPOINT", ""),
		MNSAccessKeyID:     envStr("MNS_ACCESS_KEY_ID", ""),
		MNSAccessKeySecret: envStr("MNS_ACCESS_KEY_SECRET", ""),
		MNSQueueName:       envStr("MNS_QUEUE_NAME", "auto-promote-prod"),
	}
}

// --------------- helpers ---------------

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
