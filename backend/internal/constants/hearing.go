package constants

import "time"

// HearingStatus 庭审场次状态枚举。
const (
	HearingStatusScheduled = "scheduled" // 待开庭（有效场次，律师待办的唯一来源）
	HearingStatusCancelled = "cancelled" // 已作废（改期后旧场次落为此状态，仅留档审计）
)

// HearingStatusValues 全部庭审状态值。
var HearingStatusValues = []string{HearingStatusScheduled, HearingStatusCancelled}

// MinHearingInterval 同一律师两场有效庭审之间的最小间隔：不足 2 小时整次拒绝。
const MinHearingInterval = 2 * time.Hour

// IsValidHearingStatus 校验庭审状态。
func IsValidHearingStatus(s string) bool {
	for _, v := range HearingStatusValues {
		if v == s {
			return true
		}
	}
	return false
}
