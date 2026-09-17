package dto

// HearingScheduleRequest 排期请求。
// hearing_time 为 RFC3339（如 2026-09-20T09:00:00+08:00）；lead_lawyer_id 可省略，取案件主办律师。
type HearingScheduleRequest struct {
	CaseID       uint64 `json:"case_id" binding:"required"`
	LeadLawyerID uint64 `json:"lead_lawyer_id"`
	HearingTime  string `json:"hearing_time" binding:"required"`
	Court        string `json:"court" binding:"required,max=200"`
	Courtroom    string `json:"courtroom" binding:"required,max=100"`
}

// HearingRescheduleRequest 改期请求：旧场次作废、生成新场次。
// court/courtroom 可省略，省略时沿用旧场次。
type HearingRescheduleRequest struct {
	HearingTime string `json:"hearing_time" binding:"required"`
	Court       string `json:"court" binding:"omitempty,max=200"`
	Courtroom   string `json:"courtroom" binding:"omitempty,max=100"`
}

// HearingCancelRequest 取消请求。
type HearingCancelRequest struct {
	Reason string `json:"reason" binding:"omitempty,max=255"`
}
