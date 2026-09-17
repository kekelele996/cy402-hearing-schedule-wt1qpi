package constants

import "time"

// HearingStatus 庭审场次状态枚举。
// scheduled 为唯一被视为“有效/待办”的状态；改期与取消均只作废旧场次，不做物理删除。
const (
	HearingStatusScheduled = "scheduled"
	HearingStatusResched   = "rescheduled"
	HearingStatusCanceled  = "canceled"
)

// HearingStatusValues 全部庭审状态值。
var HearingStatusValues = []string{HearingStatusScheduled, HearingStatusResched, HearingStatusCanceled}

// MinHearingGap 同一主办律师相邻两场庭审的最小间隔：不足两小时一律拒绝。
const MinHearingGap = 2 * time.Hour

// IsValidHearingStatus 校验庭审状态。
func IsValidHearingStatus(s string) bool {
	for _, v := range HearingStatusValues {
		if v == s {
			return true
		}
	}
	return false
}
