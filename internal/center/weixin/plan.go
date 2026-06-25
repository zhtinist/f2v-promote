package weixin

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"

	"github.com/AiMarketool/f2v-promote/internal/config"
)

// BuildPlanData 构建诸葛 create-plan 请求体
// promoteType: "like_rate"→推荐(target=3), "followers"→关注(target=2)
func BuildPlanData(tagGroup TagGroup, video, author json.RawMessage, cfg *config.Config, tsUserID, groupID, promoteType string) *CreatePlanRequest {
	ages := tagGroup.Ages
	if len(ages) == 0 {
		ages = []int{1, 2, 3, 4, 5}
	}

	cityIDs := tagGroup.CityIDs
	if cityIDs == nil {
		cityIDs = []string{}
	}

	interestTagList := tagGroup.InterestTagList
	if interestTagList == nil {
		interestTagList = []InterestTagRef{}
	}

	exportID := ""
	var videoMap map[string]any
	if err := json.Unmarshal(video, &videoMap); err == nil {
		if v, ok := videoMap["exportId"].(string); ok {
			exportID = v
		}
	}

	seqNo := fmt.Sprintf("%d", rand.Int63n(9e18)+1e16)

	var gid any = groupID
	if n, err := strconv.Atoi(groupID); err == nil {
		gid = n
	}

	// 根据投放类型映射 promotion_target
	promotionTarget := 2 // 默认：关注(followers)
	if promoteType == "like_rate" {
		promotionTarget = 3 // 推荐(点赞率)
	}

	return &CreatePlanRequest{
		TsUserList:            []string{tsUserID},
		PlanCount:             1,
		WeiDou:                cfg.ZhugeWeiDou,
		PromotionTarget:       promotionTarget,
		PricingType:           2,
		PromotionType:         2,
		MyPrice:               40,
		Sex:                   tagGroup.Sex,
		Ages:                  ages,
		CityIDs:               cityIDs,
		InterestTagList:       interestTagList,
		TimeType:              1,
		UseCouponStrategy:     3,
		BillingMethod:         0,
		IsPartner:             0,
		CompensationStatus:    0,
		CompensationParamType: 1,
		CompensationParamVal:  2,
		AutoCloseTemplateIDs:  []any{},
		MorePriceList:         []any{},
		SimilarAuthorList:     []any{},
		SimilarUsernameList:   []any{},
		Portrait:              false,
		DeviceType:            nil,
		GroupID:               gid,
		InterestTag:           []any{},
		ExportID:              exportID,
		AuthorVideoList:       []json.RawMessage{video},
		Author:                author,
		SeqNoList:             []string{seqNo},
	}
}

