package repository

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"cylawcase/internal/constants"
	"cylawcase/internal/model"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// HearingRepository 庭审排期仓储。
//
// 并发正确性的两个关键手段均落在数据库层：
//  1. TransactionWithLawyerLock：同一律师的全部写入在事务内先取
//     pg_advisory_xact_lock（键空间 9102 + lawyer_id），把同律师的
//     撞庭检查与写入串行化，从根上消除 check-then-insert 竞态；
//  2. FindForUpdate：改期时对旧场次加行锁，同时到达的两个改期只有一个
//     能看到 scheduled 状态，另一个直接失败，配合部分唯一索引收敛为一条后继。
type HearingRepository struct {
	db *gorm.DB
}

// NewHearingRepository 构造庭审仓储。
func NewHearingRepository(db *gorm.DB) *HearingRepository {
	return &HearingRepository{db: db}
}

// lawyerAdvisoryKey 与律师 ID 组合成全局唯一的 advisory lock 键值。
// 9102 为「庭审排期」模块固定键空间，避免与其他业务锁碰撞。
const lawyerAdvisoryKey int64 = 9102

// TransactionWithLawyerLock 在事务内对指定律师加事务级咨询锁后执行 fn。
// 锁在事务提交/回滚时自动释放，无需手动 unlock，同一律师的写操作互斥。
func (r *HearingRepository) TransactionWithLawyerLock(leadLawyerID uint64, fn func(tx *gorm.DB) error) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?, ?)", lawyerAdvisoryKey, int64(leadLawyerID)).Error; err != nil {
			return fmt.Errorf("acquire lawyer advisory lock: %w", err)
		}
		return fn(tx)
	})
}

// LockCaseForUpdate 在事务内锁定案件行并返回案件，防止排期与案件结案/归档并发。
func (r *HearingRepository) LockCaseForUpdate(tx *gorm.DB, caseID uint64) (*model.Case, error) {
	var c model.Case
	if err := tx.Set("gorm:query_option", "FOR UPDATE").First(&c, caseID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("lock case for update: %w", err)
	}
	return &c, nil
}

// FindForUpdate 在事务内锁定庭审场次行并返回。
func (r *HearingRepository) FindForUpdate(tx *gorm.DB, id uint64) (*model.Hearing, error) {
	var h model.Hearing
	if err := tx.Set("gorm:query_option", "FOR UPDATE").First(&h, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("lock hearing for update: %w", err)
	}
	return &h, nil
}

// FindByID 按 ID 查询场次（无锁，供详情/校验使用）。
func (r *HearingRepository) FindByID(id uint64) (*model.Hearing, error) {
	var h model.Hearing
	if err := r.db.First(&h, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find hearing by id: %w", err)
	}
	return &h, nil
}

// Create 在事务内创建场次。
func (r *HearingRepository) Create(tx *gorm.DB, h *model.Hearing) error {
	if err := tx.Create(h).Error; err != nil {
		var pgErr *pgconn.PgError
		// 23505 = unique_violation：并发双改期时第二个事务在
		// uni_hearings_rescheduled_from_id 上撞唯一约束，收敛为并发冲突。
		if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
			strings.Contains(pgErr.ConstraintName, "rescheduled_from") {
			return ErrHearingConcurrentReschedule
		}
		return fmt.Errorf("create hearing: %w", err)
	}
	return nil
}

// ErrHearingConcurrentReschedule 同一旧场次已存在后继场次（并发改期的败者）。
var ErrHearingConcurrentReschedule = errors.New("hearing concurrent reschedule")

// Save 在事务内保存场次（乐观锁版本号由调用方通过 UpdatesWithVersion 处理）。
func (r *HearingRepository) Save(tx *gorm.DB, h *model.Hearing) error {
	if err := tx.Save(h).Error; err != nil {
		return fmt.Errorf("save hearing: %w", err)
	}
	return nil
}

// CancelScheduledWithCAS 在事务内用条件更新作废旧场次（CAS）。
// 仅当 status=scheduled 且 version 匹配时生效；返回受影响行数：
// 0 表示场次已被并发改期作废，调用方必须回滚整次请求（原排期不变）。
func (r *HearingRepository) CancelScheduledWithCAS(tx *gorm.DB, id uint64, version int, cancelledAt time.Time) (int64, error) {
	res := tx.Model(&model.Hearing{}).
		Where("id = ? AND status = ? AND version = ?", id, constants.HearingStatusScheduled, version).
		Updates(map[string]any{
			"status":       constants.HearingStatusCancelled,
			"cancelled_at": cancelledAt,
			"version":      gorm.Expr("version + 1"),
		})
	if res.Error != nil {
		return 0, fmt.Errorf("cancel hearing with cas: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// List 分页查询场次，支持案件/律师/状态/时间范围筛选。
func (r *HearingRepository) List(page, pageSize int, caseID, lawyerID uint64, status string,
	startTime, endTime *time.Time) ([]model.Hearing, int64, error) {
	var list []model.Hearing
	var total int64
	q := r.db.Model(&model.Hearing{})
	if caseID > 0 {
		q = q.Where("case_id = ?", caseID)
	}
	if lawyerID > 0 {
		q = q.Where("lead_lawyer_id = ?", lawyerID)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if startTime != nil {
		q = q.Where("hearing_time >= ?", *startTime)
	}
	if endTime != nil {
		q = q.Where("hearing_time <= ?", *endTime)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count hearings: %w", err)
	}
	if err := q.Order("hearing_time DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, fmt.Errorf("list hearings: %w", err)
	}
	return list, total, nil
}

// CountScheduledBetweenLawyer 统计某律师在开区间 (start, end) 内的有效场次数量。
// 两端严格不等，对应撞庭条件 abs(Δ) < 2h：恰好 2 小时（落在 start 或 end）不算冲突。
// excludeID 用于改期时排除正在被作废的旧场次本身。
func (r *HearingRepository) CountScheduledBetweenLawyer(tx *gorm.DB, lawyerID uint64,
	start, end time.Time, excludeID uint64) (int64, error) {
	var n int64
	q := tx.Model(&model.Hearing{}).
		Where("lead_lawyer_id = ?", lawyerID).
		Where("status = ?", constants.HearingStatusScheduled).
		Where("hearing_time > ? AND hearing_time < ?", start, end)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	if err := q.Count(&n).Error; err != nil {
		return 0, fmt.Errorf("count scheduled hearings between: %w", err)
	}
	return n, nil
}

// FindScheduledBetweenLawyer 查明某律师在开区间 (start, end) 内的全部有效场次，
// 用于撞庭冲突定位具体场次。excludeID 用于改期时排除被作废的旧场次。
func (r *HearingRepository) FindScheduledBetweenLawyer(tx *gorm.DB, lawyerID uint64,
	start, end time.Time, excludeID uint64) ([]model.Hearing, error) {
	var list []model.Hearing
	q := tx.Where("lead_lawyer_id = ?", lawyerID).
		Where("status = ?", constants.HearingStatusScheduled).
		Where("hearing_time > ? AND hearing_time < ?", start, end)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	if err := q.Order("hearing_time ASC").Find(&list).Error; err != nil {
		return nil, fmt.Errorf("find scheduled hearings between: %w", err)
	}
	return list, nil
}
