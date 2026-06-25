package model

// 平台常量
const (
	PlatformWeixin = "weixin" // 微信视频号（默认）
	PlatformDouyin = "douyin" // 抖音（后续扩展）
)

// PlatformDisplayName 平台中文映射
var PlatformDisplayName = map[string]string{
	PlatformWeixin: "微信",
	PlatformDouyin: "抖音",
}

// GetPlatformDisplayName 获取平台中文名，无映射则原值返回
func GetPlatformDisplayName(platform string) string {
	if name, ok := PlatformDisplayName[platform]; ok {
		return name
	}
	return platform
}
