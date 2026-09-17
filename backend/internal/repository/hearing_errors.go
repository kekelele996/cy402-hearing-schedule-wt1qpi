package repository

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// 庭审排期仓储层哨兵错误，service 层负责翻译成统一错误码。
var (
	ErrHearingConflict   = errors.New("hearing time conflicts with another hearing within two hours")
	ErrHearingInvalid    = errors.New("hearing is no longer scheduled")
	ErrHearingConcurrent = errors.New("hearing was modified by a concurrent operation")
)

// hearingAdvisoryNS 庭审排期咨询锁的固定命名空间。
const hearingAdvisoryNS int64 = 684342 // "H" 相关私有命名空间，避免与其他模块咨询键碰撞

// isDuplicateKeyErr 跨方言识别唯一约束冲突（PostgreSQL 23505 / SQLite UNIQUE）。
func isDuplicateKeyErr(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "duplicate")
}
