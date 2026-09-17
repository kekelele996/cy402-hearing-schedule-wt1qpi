package model

import "time"

// Hearing 庭审排期实体。
//
// 改期不更新原记录：改期 = 将旧场次置为 cancelled 并写入一条新的 scheduled 场次，
// 新旧场次通过 rescheduled_from_id 串成改期链，旧记录完整保留用于审计追溯。
type Hearing struct {
	ID           uint64    `gorm:"primaryKey" json:"id"`
	HearingNo    string    `gorm:"size:50;uniqueIndex;not null" json:"hearing_no"`
	CaseID       uint64    `gorm:"not null;index:idx_hearings_case" json:"case_id"`
	LeadLawyerID uint64    `gorm:"not null;index" json:"lead_lawyer_id"`
	HearingTime  time.Time `gorm:"not null;index" json:"hearing_time"`
	Court        string    `gorm:"size:200;not null" json:"court"`
	Courtroom    string    `gorm:"size:100;not null" json:"courtroom"`
	Status       string    `gorm:"size:30;not null;default:scheduled;index" json:"status"`
	// RescheduledFromID 指向被本场替换的旧场次。PostgreSQL 唯一索引允许多个 NULL，
	// 因此根场次（首次排期）可有多条，而每个旧场次至多只能有一条后继场次，
	// 作为「并发双改期收敛为一条有效记录」的数据库层兜底。
	RescheduledFromID *uint64    `gorm:"uniqueIndex:uni_hearings_rescheduled_from_id" json:"rescheduled_from_id"`
	Version           int        `gorm:"not null;default:0" json:"version"`
	CreatedAt         time.Time  `json:"created_at"`
	CancelledAt       *time.Time `json:"cancelled_at"`
}

// TableName 指定表名。
func (Hearing) TableName() string { return "hearings" }
