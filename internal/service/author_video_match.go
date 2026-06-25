package service

import (
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/AiMarketool/f2v-promote/internal/center/weixin"
	"github.com/AiMarketool/f2v-promote/internal/model"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"gorm.io/datatypes"
)

// AuthorVideoMatchService 在 video_stats 写入时自动关联 author_id
type AuthorVideoMatchService struct {
	authorVideoRepo *repository.AuthorVideoRepo
	videoStatRepo   *repository.VideoStatRepo
	authorRepo      *repository.AuthorRepo
	weixinClient    *weixin.Client
}

func NewAuthorVideoMatchService(
	authorVideoRepo *repository.AuthorVideoRepo,
	videoStatRepo *repository.VideoStatRepo,
	authorRepo *repository.AuthorRepo,
	weixinClient *weixin.Client,
) *AuthorVideoMatchService {
	return &AuthorVideoMatchService{
		authorVideoRepo: authorVideoRepo,
		videoStatRepo:   videoStatRepo,
		authorRepo:      authorRepo,
		weixinClient:    weixinClient,
	}
}

// FillAuthorIDs 批量为 video_stats 回填 author_id（批量预加载 + 内存匹配）
func (s *AuthorVideoMatchService) FillAuthorIDs(stats []model.VideoStat) {
	if len(stats) == 0 {
		return
	}

	// ── 收集待匹配的 nonce / description ──
	nonces := make([]string, 0, len(stats))
	descs := make([]string, 0, len(stats))
	for _, stat := range stats {
		if stat.AuthorID != nil && *stat.AuthorID != 0 && stat.AuthorVideoID != 0 {
			continue
		}
		if stat.Nonce != "" {
			nonces = append(nonces, stat.Nonce)
		}
		if stat.Description != "" {
			descs = append(descs, strings.TrimSpace(stat.Description))
		}
	}

	if len(nonces) == 0 && len(descs) == 0 {
		return
	}

	// ── P1: 批量查 author_videos 表，构建内存 Map ──
	nonceMap, descMap := s.buildAuthorVideoMaps(nonces, descs)

	// ── 内存匹配 + 收集未命中列表 ──
	updates := make(map[int64]avMatch) // statID → {authorID, authorVideoID}
	var unmatchedStats []model.VideoStat

	for i, stat := range stats {
		if stat.AuthorID != nil && *stat.AuthorID != 0 && stat.AuthorVideoID != 0 {
			continue
		}
		if stat.Nonce == "" && stat.Description == "" {
			continue
		}

		if stat.Nonce != "" {
			if m, ok := nonceMap[stat.Nonce]; ok {
				stats[i].AuthorID = &m.AuthorID
				stats[i].AuthorVideoID = m.AuthorVideoID
				updates[stat.ID] = m
				log.Printf("service=match action=fill stat=%d author=%d av=%d source=db_nonce", stat.ID, m.AuthorID, m.AuthorVideoID)
				continue
			}
		}
		if stat.Description != "" {
			if m, ok := descMap[strings.TrimSpace(stat.Description)]; ok {
				stats[i].AuthorID = &m.AuthorID
				stats[i].AuthorVideoID = m.AuthorVideoID
				updates[stat.ID] = m
				log.Printf("service=match action=fill stat=%d author=%d av=%d source=db_desc", stat.ID, m.AuthorID, m.AuthorVideoID)
				continue
			}
		}

		unmatchedStats = append(unmatchedStats, stats[i])
	}

	// ── P2: 未命中的走 API 聚合匹配 ──
	if len(unmatchedStats) > 0 {
		apiMatches := s.batchMatchByAPI(unmatchedStats)

		idxMap := make(map[int64]int, len(stats))
		for i, st := range stats {
			idxMap[st.ID] = i
		}
		for statID, m := range apiMatches {
			updates[statID] = m
			if idx, ok := idxMap[statID]; ok {
				aid := m.AuthorID
				stats[idx].AuthorID = &aid
				stats[idx].AuthorVideoID = m.AuthorVideoID
			}
		}
	}

	// ── 批量回填 DB（author_id + author_video_id）──
	if len(updates) > 0 {
		authorIDs := make(map[int64]int64, len(updates))
		avIDs := make(map[int64]int64, len(updates))
		for statID, m := range updates {
			authorIDs[statID] = m.AuthorID
			avIDs[statID] = m.AuthorVideoID
		}
		if err := s.videoStatRepo.BatchUpdateAuthorAndVideoID(authorIDs, avIDs); err != nil {
			log.Printf("service=match action=batch_update error=%v", err)
		} else {
			log.Printf("service=match action=batch_update count=%d", len(updates))
		}
	}
}

type avMatch struct {
	AuthorID      int64
	AuthorVideoID int64
}

// buildAuthorVideoMaps 批量查询 author_videos，返回 nonceMap 和 descMap（含 author_video_id）
func (s *AuthorVideoMatchService) buildAuthorVideoMaps(nonces, descs []string) (map[string]avMatch, map[string]avMatch) {
	nonceMap := make(map[string]avMatch)
	descMap := make(map[string]avMatch)

	authorVideos, err := s.authorVideoRepo.GetByNoncesOrDescriptions(nonces, descs)
	if err != nil {
		log.Printf("service=match action=batch_query_author_videos error=%v", err)
		return nonceMap, descMap
	}

	for _, av := range authorVideos {
		m := avMatch{AuthorID: av.AuthorID, AuthorVideoID: av.ID}
		if av.Nonce != "" {
			nonceMap[av.Nonce] = m
		}
		if av.Description != "" {
			descMap[strings.TrimSpace(av.Description)] = m
		}
	}

	log.Printf("service=match action=build_maps nonce_hits=%d desc_hits=%d", len(nonceMap), len(descMap))
	return nonceMap, descMap
}

// batchMatchByAPI 聚合 API 调用：计算最宽日期范围，每个作者只调一次 API
func (s *AuthorVideoMatchService) batchMatchByAPI(stats []model.VideoStat) map[int64]avMatch {
	result := make(map[int64]avMatch) // statID → avMatch

	// ── 计算最宽日期范围 ──
	var minDate, maxDate time.Time
	for _, stat := range stats {
		if len(stat.PublishDate) < 10 {
			continue
		}
		t, err := time.Parse("2006-01-02", stat.PublishDate[:10])
		if err != nil {
			t, err = time.Parse("2006/01/02", stat.PublishDate[:10])
		}
		if err != nil {
			log.Printf("service=match action=parse_publish_date error=%v", err)
			continue
		}
		if minDate.IsZero() || t.Before(minDate) {
			minDate = t
		}
		if maxDate.IsZero() || t.After(maxDate) {
			maxDate = t
		}
	}

	if minDate.IsZero() {
		return result
	}

	// ── 生成 7 天分片（API 每次最多拉 7 天）──
	actualStart := minDate.AddDate(0, 0, -1)
	actualEnd := maxDate.AddDate(0, 0, 1)

	type dateChunk struct{ Start, End string }
	var chunks []dateChunk
	for cs := actualStart; !cs.After(actualEnd); cs = cs.AddDate(0, 0, 7) {
		ce := cs.AddDate(0, 0, 6)
		if ce.After(actualEnd) {
			ce = actualEnd
		}
		chunks = append(chunks, dateChunk{
			Start: cs.Format("2006-01-02") + " 00:00:00",
			End:   ce.Format("2006-01-02") + " 23:59:59",
		})
	}

	// ── 查全部作者（仅一次）──
	authors, err := s.authorRepo.ListAll()
	if err != nil {
		log.Printf("service=match action=list_authors error=%v", err)
		return result
	}

	// ── 每个作者按分片调 API，将视频 upsert 到 author_videos ──
	var allNonces []string
	var allDescs []string

	for _, author := range authors {
		accountIDStr := strconv.FormatInt(author.AccountID, 10)

		for _, chunk := range chunks {
			videos, err := s.weixinClient.GetAuthorVideos(accountIDStr, author.Username, chunk.Start, chunk.End)
			if err != nil {
				log.Printf("service=match action=get_author_videos author=%s(%d) chunk=%s~%s error=%v",
					author.Username, author.ID, chunk.Start, chunk.End, err)
				continue
			}

			var avList []model.AuthorVideo
			for _, video := range videos {
				if video.Nonce != "" {
					allNonces = append(allNonces, video.Nonce)
				}
				trimDesc := strings.TrimSpace(video.Description)
				if trimDesc != "" {
					allDescs = append(allDescs, trimDesc)
				}

				publishTimeStr := time.Unix(video.CreateTime, 0).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
				rawJSON, _ := json.Marshal(video)
				avList = append(avList, model.AuthorVideo{
					AuthorID:    author.ID,
					AccountID:   author.AccountID,
					ExportID:    video.ExportID,
					Description: video.Description,
					CoverURL:    video.CoverURL,
					PublishTime: publishTimeStr,
					Nonce:       video.Nonce,
					RawData:     datatypes.JSON(rawJSON),
				})
			}
			if len(avList) > 0 {
				if _, err := s.authorVideoRepo.BulkUpsert(avList); err != nil {
					log.Printf("service=match action=upsert_author_videos author=%d error=%v", author.ID, err)
				}
			}
		}
	}

	// ── upsert 后重新查 DB 获取实际的 author_video_id ──
	apiNonceMap := make(map[string]avMatch)
	apiDescMap := make(map[string]avMatch)

	if len(allNonces) > 0 || len(allDescs) > 0 {
		avRecords, err := s.authorVideoRepo.GetByNoncesOrDescriptions(allNonces, allDescs)
		if err != nil {
			log.Printf("service=match action=re_query_author_videos error=%v", err)
		} else {
			for _, av := range avRecords {
				m := avMatch{AuthorID: av.AuthorID, AuthorVideoID: av.ID}
				if av.Nonce != "" {
					apiNonceMap[av.Nonce] = m
				}
				if av.Description != "" {
					apiDescMap[strings.TrimSpace(av.Description)] = m
				}
			}
		}
	}

	// ── 对未匹配 stats 做内存匹配 ──
	for _, stat := range stats {
		if stat.Nonce != "" {
			if m, ok := apiNonceMap[stat.Nonce]; ok {
				result[stat.ID] = m
				log.Printf("service=match action=fill stat=%d author=%d av=%d source=api_nonce", stat.ID, m.AuthorID, m.AuthorVideoID)
				continue
			}
		}
		if stat.Description != "" {
			trimDesc := strings.TrimSpace(stat.Description)
			if m, ok := apiDescMap[trimDesc]; ok {
				result[stat.ID] = m
				log.Printf("service=match action=fill stat=%d author=%d av=%d source=api_desc", stat.ID, m.AuthorID, m.AuthorVideoID)
				continue
			}
		}
	}

	return result
}
