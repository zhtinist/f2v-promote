package model

// VideoStat 视频号动态数据统计
type VideoStat struct {
	Base
	AuthorVideoID   int64  `gorm:"type:bigint;not null" json:"author_video_id"`                      // 作者视频ID
	Platform        string `gorm:"type:varchar(32);not null;default:'weixin';index" json:"platform"` // 平台标识
	ExportID        string `gorm:"type:varchar(256);not null" json:"export_id"`                      // 视频ID
	CollectDate     string `gorm:"type:varchar(20);not null" json:"collect_date"`                    // 采集日期
	AuthorID        *int64 `gorm:"index" json:"author_id"`                                           // 作者ID，关联 authors.id
	Description     string `gorm:"type:text" json:"description"`                                     // 视频描述
	PublishDate     string `gorm:"type:varchar(20)" json:"publish_date"`                             // 发布时间
	CompletionRate  string `gorm:"type:varchar(20)" json:"completion_rate"`                          // 完播率
	AvgPlayDuration string `gorm:"type:varchar(20)" json:"avg_play_duration"`                        // 平均播放时长
	PlayCount       int64  `gorm:"default:0" json:"play_count"`                                      // 播放量
	RecommendCount  int64  `gorm:"default:0" json:"recommend_count"`                                 // 推荐
	LikeCount       int64  `gorm:"default:0" json:"like_count"`                                      // 喜欢
	CommentCount    int64  `gorm:"default:0" json:"comment_count"`                                   // 评论量
	ShareCount      int64  `gorm:"default:0" json:"share_count"`                                     // 分享量
	FollowCount     int64  `gorm:"default:0" json:"follow_count"`                                    // 关注量
	ForwardCount    int64  `gorm:"default:0" json:"forward_count"`                                   // 转发聊天和朋友圈
	RingtoneCount   int64  `gorm:"default:0" json:"ringtone_count"`                                  // 设为铃声
	StatusCount     int64  `gorm:"default:0" json:"status_count"`                                    // 设为状态
	CoverCount      int64  `gorm:"default:0" json:"cover_count"`                                     // 设为朋友圈封面
	FeishuSynced    bool   `gorm:"default:false;index" json:"feishu_synced"`                         // 飞书同步状态
	Nonce           string `gorm:"type:varchar(256);not null" json:"nonce"`                          // 随机字符串 唯一的
}

func (VideoStat) TableName() string { return "video_stats" }
