package repository

import (
	"errors"
	"fmt"
	"time"

	"cylawcase/internal/model"

	"gorm.io/gorm"
)

// HearingRepository 庭审场次仓储。
type HearingRepository struct {
	db *gorm.DB
}

// NewHearingRepository 构造庭审仓储。
func NewHearingRepository(db *gorm.DB) *HearingRepository {
	return &HearingRepository{db: db}
}

// DB 暴露底层连接，供需要自定义事务的场景使用。
func (r *HearingRepository) DB() *gorm.DB { return r.db }

// ScheduleInput 单场排期输入。
type ScheduleInput struct {
	CaseID       uint64
	LeadLawyerID uint64
	HearingTime  time.Time
	Court        string
	Courtroom    string
}

// ScheduleBatch 在单个事务内原子排期：任一场次触发约束则整批回滚，原排期不变。
func (r *HearingRepository) ScheduleBatch(inputs []ScheduleInput) ([]model.Hearing, error) {
	var created []model.Hearing
	err := r.db.Transaction(func(tx *gorm.DB) error {
		for i := range inputs {
			in := inputs[i]
			lockLawyer(tx, in.LeadLawyerID)
			if conflict, err := findConflict(tx, in.LeadLawyerID, in.HearingTime, 0, 0); err != nil {
				return err
			} else if conflict != nil {
				return ErrHearingConflict
			}
			h := model.Hearing{
				HearingNo:    genHearingNo(),
				CaseID:       in.CaseID,
				LeadLawyerID: in.LeadLawyerID,
				HearingTime:  in.HearingTime,
				Court:        in.Court,
				Courtroom:    in.Courtroom,
				Status:       "scheduled",
				RootID:       0,
				Seq:          1,
			}
			if err := tx.Create(&h).Error; err != nil {
				return fmt.Errorf("create hearing: %w", err)
			}
			h.RootID = h.ID
			if err := tx.Model(&model.Hearing{}).Where("id = ?", h.ID).Update("root_id", h.ID).Error; err != nil {
				return fmt.Errorf("link hearing root: %w", err)
			}
			created = append(created, h)
		}
		return nil
	})
	return created, err
}

// Reschedule 改期：作废旧场次并生成新场次，全过程在单个事务内完成。
// expectSeq 实现乐观并发控制：旧场次序号必须与调用方读取时一致，否则返回冲突错误。
func (r *HearingRepository) Reschedule(oldID uint64, expectSeq int, newTime time.Time, court, courtroom string) (*model.Hearing, error) {
	var result *model.Hearing
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var old model.Hearing
		if err := tx.First(&old, oldID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("find hearing for reschedule: %w", err)
		}
		if old.Status != "scheduled" {
			return ErrHearingInvalid
		}
		if old.Seq != expectSeq {
			return ErrHearingConcurrent
		}
		// 以当前案件归属的主办律师加锁并做冲突检测。
		var c model.Case
		if err := tx.First(&c, old.CaseID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("find case for reschedule: %w", err)
		}
		lockLawyer(tx, c.LeadLawyerID)
		if conflict, err := findConflict(tx, c.LeadLawyerID, newTime, old.RootID, old.ID); err != nil {
			return err
		} else if conflict != nil {
			return ErrHearingConflict
		}
		// CAS 作废：WHERE status='scheduled' AND seq=expectSeq，
		// 与 root_id 部分唯一索引构成双重收敛，同时到达的两个改期最多只有一个成功。
		res := tx.Model(&model.Hearing{}).
			Where("id = ? AND status = ? AND seq = ?", oldID, "scheduled", expectSeq).
			Updates(map[string]any{"status": "rescheduled", "updated_at": time.Now()})
		if res.Error != nil {
			return fmt.Errorf("void old hearing: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrHearingConcurrent
		}
		h := model.Hearing{
			HearingNo:       genHearingNo(),
			CaseID:          old.CaseID,
			LeadLawyerID:    c.LeadLawyerID,
			HearingTime:     newTime,
			Court:           court,
			Courtroom:       courtroom,
			Status:          "scheduled",
			RootID:          old.RootID,
			Seq:             old.Seq + 1,
			RescheduledFrom: old.ID,
		}
		if err := tx.Create(&h).Error; err != nil {
			return mapHearingCreateError(err)
		}
		result = &h
		return nil
	})
	return result, err
}

// Cancel 取消场次：CAS 条件作废，已作废场次不可重复取消。
func (r *HearingRepository) Cancel(id uint64, reason string) (*model.Hearing, error) {
	var result *model.Hearing
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var h model.Hearing
		if err := tx.First(&h, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("find hearing for cancel: %w", err)
		}
		if h.Status != "scheduled" {
			return ErrHearingInvalid
		}
		res := tx.Model(&model.Hearing{}).
			Where("id = ? AND status = ?", id, "scheduled").
			Updates(map[string]any{"status": "canceled", "cancel_reason": reason, "updated_at": time.Now()})
		if res.Error != nil {
			return fmt.Errorf("cancel hearing: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrHearingConcurrent
		}
		h.Status = "canceled"
		h.CancelReason = reason
		result = &h
		return nil
	})
	return result, err
}

// FindByID 按 ID 查询场次（含已作废场次，调用方按 Status 判定有效性）。
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

// List 分页查询，支持案件/律师/状态筛选；默认只返回有效场次，传 include_void=true 才含作废记录。
func (r *HearingRepository) List(page, pageSize int, caseID, lawyerID uint64, status string, from, to *time.Time, includeVoid bool) ([]model.Hearing, int64, error) {
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
	} else if !includeVoid {
		q = q.Where("status = ?", "scheduled")
	}
	if from != nil {
		q = q.Where("hearing_time >= ?", from)
	}
	if to != nil {
		q = q.Where("hearing_time <= ?", to)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count hearings: %w", err)
	}
	if err := q.Order("hearing_time ASC, id ASC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, fmt.Errorf("list hearings: %w", err)
	}
	return list, total, nil
}

// ListUpcoming 查询未来的有效庭审，按时间升序，limit 为 0 时不限制条数。
func (r *HearingRepository) ListUpcoming(now time.Time, lawyerID uint64, limit int) ([]model.Hearing, error) {
	var list []model.Hearing
	q := r.db.Where("status = ? AND hearing_time >= ?", "scheduled", now)
	if lawyerID > 0 {
		q = q.Where("lead_lawyer_id = ?", lawyerID)
	}
	q = q.Order("hearing_time ASC, id ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&list).Error; err != nil {
		return nil, fmt.Errorf("list upcoming hearings: %w", err)
	}
	return list, nil
}

// ListHistoryByCase 查询某案件全部场次（含作废），按时间升序，用于追溯改期链。
func (r *HearingRepository) ListHistoryByCase(caseID uint64) ([]model.Hearing, error) {
	var list []model.Hearing
	if err := r.db.Where("case_id = ?", caseID).
		Order("hearing_time ASC, id ASC").Find(&list).Error; err != nil {
		return nil, fmt.Errorf("list hearing history by case: %w", err)
	}
	return list, nil
}

// lockLawyer 在 PostgreSQL 上获取以律师为键的事务级咨询锁，串行化同律师并发写入；
// 其他方言（如测试用 SQLite）退化为无锁，由 CAS 与唯一索引保证正确性。
func lockLawyer(tx *gorm.DB, lawyerID uint64) {
	if tx.Dialector.Name() != "postgres" {
		return
	}
	_ = tx.Exec("SELECT pg_advisory_xact_lock(?, ?)", int32(hearingAdvisoryNS), int32(lawyerID)).Error
}

// findConflict 查询同律师、与目标时间间隔不足两小时的有效场次。
// excludeRootID 用于改期时排除同一条改期链；excludeID 额外排除被作废的旧场次自身。
func findConflict(tx *gorm.DB, lawyerID uint64, t time.Time, excludeRootID, excludeID uint64) (*model.Hearing, error) {
	minGap := 2 * time.Hour
	q := tx.Where("lead_lawyer_id = ? AND status = ?", lawyerID, "scheduled").
		Where("hearing_time > ? AND hearing_time < ?", t.Add(-minGap), t.Add(minGap))
	if excludeRootID > 0 {
		q = q.Where("root_id <> ?", excludeRootID)
	}
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	var h model.Hearing
	err := q.Order("hearing_time ASC").First(&h).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find hearing conflict: %w", err)
	}
	return &h, nil
}

// mapHearingCreateError 将唯一索引冲突映射为并发改期冲突（同时改期收敛场景）。
func mapHearingCreateError(err error) error {
	if err != nil && isDuplicateKeyErr(err) {
		return ErrHearingConcurrent
	}
	if err != nil {
		return fmt.Errorf("create rescheduled hearing: %w", err)
	}
	return nil
}

// genHearingNo 生成庭审编号：HEAR + 年份 + 纳秒随机段。
func genHearingNo() string {
	return fmt.Sprintf("HEAR%d%07d", time.Now().Year(), time.Now().UnixNano()%10000000)
}
