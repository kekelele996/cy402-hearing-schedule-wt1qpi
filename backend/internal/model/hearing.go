package model

import "time"

// Hearing 庭审场次实体。
//
// 排期规则（均不可放宽，否则会出现撞庭或从旧记录误读待办）：
//   - 每场庭审归属一个案件和一位主办律师，必须载明时间、法院、法庭。
//   - 同一律师两场有效庭审间隔不足两小时时整次拒绝，原排期不变。
//   - 改期只作废旧场次（status=rescheduled）并生成新场次，不做原地更新或物理删除。
//   - 同一场次的同时改期通过 root_id 部分唯一索引收敛为一条有效记录。
//   - 已结案或归档案件不能新增未来庭审。
type Hearing struct {
	ID           uint64    `gorm:"primaryKey" json:"id"`
	HearingNo    string    `gorm:"size:50;uniqueIndex;not null" json:"hearing_no"`
	CaseID       uint64    `gorm:"not null;index:idx_hearings_case" json:"case_id"`
	LeadLawyerID uint64    `gorm:"not null;index:idx_hearings_lawyer" json:"lead_lawyer_id"`
	HearingTime  time.Time `gorm:"not null;index" json:"hearing_time"`
	Court        string    `gorm:"size:200;not null" json:"court"`
	Courtroom    string    `gorm:"size:100;not null" json:"courtroom"`
	// Status 仅 scheduled 为有效场次；rescheduled/canceled 为作废场次。
	Status          string    `gorm:"size:30;not null;default:scheduled;index" json:"status"`
	RootID          uint64    `gorm:"not null;index;uniqueIndex:uniq_hearing_active,where:status='scheduled'" json:"root_id"`
	Seq             int       `gorm:"not null;default:1" json:"seq"`
	RescheduledFrom uint64    `gorm:"not null;default:0" json:"rescheduled_from"`
	CancelReason    string    `gorm:"size:255;not null;default:''" json:"cancel_reason"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// TableName 指定表名。
func (Hearing) TableName() string { return "hearings" }
