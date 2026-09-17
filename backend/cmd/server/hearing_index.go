package main

import (
	"cylawcase/internal/model"

	"gorm.io/gorm"
)

// initHearingIndexes 建立 GORM 标签无法表达的部分唯一索引：
// 每条改期链（root_id）在 status='scheduled' 时最多存在一条有效场次，
// 从数据库层收敛“同时到达的两个改期”，避免误读出两条待办。
// 仅 PostgreSQL 支持部分索引；其他方言跳过（如本地测试）。
func initHearingIndexes(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	return db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS uniq_hearing_active
		ON ` + model.Hearing{}.TableName() + ` (root_id)
		WHERE status = 'scheduled'`).Error
}
