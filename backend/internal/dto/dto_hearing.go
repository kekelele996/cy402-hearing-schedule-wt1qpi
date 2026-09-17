package dto

import "time"

// HearingScheduleRequest 新增庭审排期请求。
// 主办律师由后端按案件的 lead_lawyer_id 确定，不接受前端指定，
// 保证庭审归属与案件主办律师一致。
type HearingScheduleRequest struct {
	CaseID      uint64 `json:"case_id" binding:"required"`
	HearingTime string `json:"hearing_time" binding:"required"`
	Court       string `json:"court" binding:"required,max=200"`
	Courtroom   string `json:"courtroom" binding:"required,max=100"`
}

// HearingRescheduleRequest 改期请求：只承载新场次信息，
// 旧场次作废与新场次生成由服务端原子完成。
type HearingRescheduleRequest struct {
	HearingTime string `json:"hearing_time" binding:"required"`
	Court       string `json:"court" binding:"required,max=200"`
	Courtroom   string `json:"courtroom" binding:"required,max=100"`
}

// ParseHearingTime 解析庭审时间（接受 RFC3339 与 "YYYY-MM-DD HH:MM"）。
func ParseHearingTime(s string) (time.Time, error) {
	layouts := []string{time.RFC3339, "2006-01-02 15:04", "2006-01-02 15:04:05"}
	var lastErr error
	for _, layout := range layouts {
		t, err := time.ParseInLocation(layout, s, time.Local)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}
