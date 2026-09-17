package service

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"cylawcase/internal/constants"
	"cylawcase/internal/model"
	"cylawcase/internal/repository"
	"cylawcase/internal/util"

	"gorm.io/gorm"
)

// HearingService 庭审排期业务逻辑。
//
// 不可放宽的约束（放宽即撞庭或从旧记录误读待办）：
//   - 撞庭判定只认 status=scheduled 的有效场次，cancelled 旧场次永不参与，
//     也永远不会出现在「待办」视图中；
//   - 同一律师两场有效庭审时间差 < 2 小时 → 整次拒绝、事务回滚，原排期不变；
//   - 改期 = 旧场次置 cancelled + 新建 scheduled 场次，绝不 UPDATE 原场次时间；
//   - 同时到达的两个改期：行锁 + CAS + 后继唯一索引三重保证只成活一条新场次；
//   - 已结案(closed)/已归档(archived)案件不能新增未来庭审。
type HearingService struct {
	repo     *repository.HearingRepository
	caseRepo *repository.CaseRepository
	logger   *slog.Logger
}

// NewHearingService 构造庭审服务。
func NewHearingService(repo *repository.HearingRepository, caseRepo *repository.CaseRepository, logger *slog.Logger) *HearingService {
	return &HearingService{repo: repo, caseRepo: caseRepo, logger: logger}
}

// ScheduleInput 排期/改期输入。
type ScheduleInput struct {
	HearingTime time.Time
	Court       string
	Courtroom   string
}

// Schedule 为案件新增一场未来庭审。主办律师取案件的主办律师，不由前端指定，
// 保证「每场庭审归属案件和主办律师」一致。
func (s *HearingService) Schedule(caseID uint64, in ScheduleInput) (*model.Hearing, error) {
	if err := validateScheduleInput(in); err != nil {
		return nil, err
	}
	// 事务外读律师 ID 仅用于决定先锁哪把律师咨询锁。若锁内发现案件主办律师
	// 已被改派（errLawyerChanged），则释放旧锁、按新律师重试，最多两轮。
	leadLawyerID, err := s.currentLeadLawyer(caseID)
	if err != nil {
		return nil, util.Wrap(err, "Hearing[case_id=%d] schedule: case not found", caseID)
	}

	var created *model.Hearing
	for attempt := 0; attempt < 2; attempt++ {
		var changed bool
		err = s.repo.TransactionWithLawyerLock(leadLawyerID, func(tx *gorm.DB) error {
			lockedCase, err := s.repo.LockCaseForUpdate(tx, caseID)
			if err != nil {
				return util.Wrap(err, "Hearing[case_id=%d] schedule: lock case failed", caseID)
			}
			if isClosedOrArchived(lockedCase.Status) {
				s.logger.Warn(constants.LogHearingCaseClosedReject, "case_id", caseID, "status", lockedCase.Status)
				return util.NewAppError(constants.CodeHearingCaseClosed,
					"Hearing[case_id="+u64(caseID)+"] schedule rejected: case status="+lockedCase.Status)
			}
			if lockedCase.LeadLawyerID != leadLawyerID {
				// 案件在取锁间隙被改派：本轮作废，外层按新律师重入。
				leadLawyerID = lockedCase.LeadLawyerID
				changed = true
				return errLawyerChanged
			}
			if conflict, err := s.findIntervalConflict(tx, leadLawyerID, in.HearingTime, 0); err != nil {
				return err
			} else if conflict != nil {
				s.logger.Warn(constants.LogHearingConflictReject,
					"lawyer_id", leadLawyerID, "conflict_hearing_id", conflict.ID)
				return conflictAppError(leadLawyerID, conflict.ID)
			}
			h := &model.Hearing{
				HearingNo:    genHearingNo(),
				CaseID:       caseID,
				LeadLawyerID: leadLawyerID,
				HearingTime:  in.HearingTime,
				Court:        in.Court,
				Courtroom:    in.Courtroom,
				Status:       constants.HearingStatusScheduled,
			}
			if err := s.repo.Create(tx, h); err != nil {
				s.logger.Error(constants.LogHearingCreateFailed, "error", err.Error())
				return util.Wrap(err, "Hearing[case_id=%d] schedule save failed", caseID)
			}
			created = h
			return nil
		})
		if errors.Is(err, errLawyerChanged) && changed {
			continue
		}
		break
	}
	if err != nil {
		return nil, util.Wrap(err, "Hearing[case_id=%d] schedule failed", caseID)
	}
	s.logger.Info(constants.LogHearingCreateSuccess, "hearing_id", created.ID,
		"case_id", caseID, "lawyer_id", leadLawyerID)
	return created, nil
}

// currentLeadLawyer 事务外读取案件当前主办律师。
func (s *HearingService) currentLeadLawyer(caseID uint64) (uint64, error) {
	c, err := s.caseRepo.FindByID(caseID)
	if err != nil {
		return 0, err
	}
	return c.LeadLawyerID, nil
}

// Reschedule 改期：仅作废旧场次并生成新场次，原场次时间不做任何更新。
// 任一约束不满足（撞庭、旧场次已作废、并发改期落败）都整体回滚，原排期不变。
func (s *HearingService) Reschedule(hearingID uint64, in ScheduleInput) (*model.Hearing, error) {
	if err := validateScheduleInput(in); err != nil {
		return nil, err
	}
	// 事务外读取旧场次，只为拿到加锁所需的律师 ID。
	old, err := s.repo.FindByID(hearingID)
	if err != nil {
		return nil, util.Wrap(err, "Hearing[id=%d] reschedule find failed", hearingID)
	}
	leadLawyerID := old.LeadLawyerID

	var created *model.Hearing
	err = s.repo.TransactionWithLawyerLock(leadLawyerID, func(tx *gorm.DB) error {
		s.logger.Info(constants.LogHearingRescheduleStart, "hearing_id", hearingID)

		// 锁定案件：已结案/归档案件不得借改期新增未来庭审。
		lockedCase, err := s.repo.LockCaseForUpdate(tx, old.CaseID)
		if err != nil {
			return util.Wrap(err, "Hearing[id=%d] reschedule: lock case failed", hearingID)
		}
		if isClosedOrArchived(lockedCase.Status) {
			s.logger.Warn(constants.LogHearingCaseClosedReject, "case_id", lockedCase.ID, "status", lockedCase.Status)
			return util.NewAppError(constants.CodeHearingCaseClosed,
				"Hearing[id="+u64(hearingID)+"] reschedule rejected: case status="+lockedCase.Status)
		}

		// 锁定旧场次行：同时到达的两个改期在此排队，二者看到的状态/版本不同。
		locked, err := s.repo.FindForUpdate(tx, hearingID)
		if err != nil {
			return util.Wrap(err, "Hearing[id=%d] reschedule lock failed", hearingID)
		}
		if locked.Status != constants.HearingStatusScheduled {
			return util.NewAppError(constants.CodeHearingConflict,
				"Hearing[id="+u64(hearingID)+"] reschedule rejected: already "+locked.Status)
		}
		if locked.LeadLawyerID != leadLawyerID {
			// 理论不可达：咨询锁键取自同一条场次记录的律师，行锁后该字段不会被本模块改动。
			s.logger.Warn("hearing lawyer changed under lock", "hearing_id", hearingID,
				"expected", leadLawyerID, "actual", locked.LeadLawyerID)
			return util.NewAppError(constants.CodeConflict,
				"Hearing[id="+u64(hearingID)+"] reschedule rejected: lawyer changed concurrently")
		}

		// 撞庭检查排除正在作废的旧场次本身——旧时间随之释放，允许改回原时段附近。
		if conflict, err := s.findIntervalConflict(tx, leadLawyerID, in.HearingTime, hearingID); err != nil {
			return err
		} else if conflict != nil {
			s.logger.Warn(constants.LogHearingConflictReject,
				"lawyer_id", leadLawyerID, "conflict_hearing_id", conflict.ID)
			return conflictAppError(leadLawyerID, conflict.ID)
		}

		// CAS 作废旧场次：并发双改期的败者影响 0 行 → 回滚，绝不再生成第二条新场次。
		now := time.Now()
		affected, err := s.repo.CancelScheduledWithCAS(tx, hearingID, locked.Version, now)
		if err != nil {
			return util.Wrap(err, "Hearing[id=%d] reschedule cancel failed", hearingID)
		}
		if affected == 0 {
			return util.NewAppError(constants.CodeHearingConflict,
				"Hearing[id="+u64(hearingID)+"] reschedule rejected: concurrent modification")
		}

		oldID := hearingID
		h := &model.Hearing{
			HearingNo:         genHearingNo(),
			CaseID:            locked.CaseID,
			LeadLawyerID:      locked.LeadLawyerID,
			HearingTime:       in.HearingTime,
			Court:             in.Court,
			Courtroom:         in.Courtroom,
			Status:            constants.HearingStatusScheduled,
			RescheduledFromID: &oldID,
		}
		if err := s.repo.Create(tx, h); err != nil {
			if errors.Is(err, repository.ErrHearingConcurrentReschedule) {
				return util.NewAppError(constants.CodeHearingConflict,
					"Hearing[id="+u64(hearingID)+"] reschedule rejected: successor already exists")
			}
			s.logger.Error(constants.LogHearingRescheduleFailed, "error", err.Error())
			return util.Wrap(err, "Hearing[id=%d] reschedule create failed", hearingID)
		}
		created = h
		return nil
	})
	if err != nil {
		return nil, util.Wrap(err, "Hearing[id=%d] reschedule failed", hearingID)
	}
	s.logger.Info(constants.LogHearingRescheduleSuccess,
		"old_hearing_id", hearingID, "new_hearing_id", created.ID, "lawyer_id", leadLawyerID)
	return created, nil
}

// List 分页查询。status 为空时不过滤（管理视图可见全部历史，含 cancelled）。
func (s *HearingService) List(page, pageSize int, caseID, lawyerID uint64, status string,
	startTime, endTime *time.Time) ([]model.Hearing, int64, error) {
	if status != "" && !constants.IsValidHearingStatus(status) {
		return nil, 0, util.NewAppError(constants.CodeValidationFailed, "Hearing[status="+status+"] list: invalid status")
	}
	return s.repo.List(page, pageSize, caseID, lawyerID, status, startTime, endTime)
}

// ListUpcoming 律师待办：仅返回当前时间之后的 scheduled 场次。
// cancelled 旧场次在此被彻底排除，杜绝「从旧记录误读出待办」。
func (s *HearingService) ListUpcoming(lawyerID uint64) ([]model.Hearing, error) {
	now := time.Now()
	list, _, err := s.repo.List(1, 200, 0, lawyerID, constants.HearingStatusScheduled, &now, nil)
	if err != nil {
		return nil, util.Wrap(err, "Hearing upcoming list failed: lawyer_id=%d", lawyerID)
	}
	return list, nil
}

// Get 场次详情。
func (s *HearingService) Get(id uint64) (*model.Hearing, error) {
	return s.repo.FindByID(id)
}

// findIntervalConflict 在已持有律师咨询锁的事务内检查 2 小时间隔冲突。
// 判定窗口为开区间 (t-2h, t+2h)，对应严格条件 abs(Δ) < 2h；
// 恰好 2 小时间隔允许（两端边界都不拒绝）。仅统计 scheduled 有效场次。
// 返回第一场冲突场次；无冲突时返回 nil。
func (s *HearingService) findIntervalConflict(tx *gorm.DB, lawyerID uint64,
	t time.Time, excludeID uint64) (*model.Hearing, error) {
	start := t.Add(-constants.MinHearingInterval)
	end := t.Add(constants.MinHearingInterval)
	list, err := s.repo.FindScheduledBetweenLawyer(tx, lawyerID, start, end, excludeID)
	if err != nil {
		return nil, util.Wrap(err, "Hearing interval check failed: lawyer_id=%d", lawyerID)
	}
	if len(list) == 0 {
		return nil, nil
	}
	return &list[0], nil
}

// errLawyerChanged 持锁期间发现案件主办律师已被改派，调用方按新律师重试。
var errLawyerChanged = errors.New("case lead lawyer changed")

func validateScheduleInput(in ScheduleInput) error {
	if in.Court == "" {
		return util.NewAppError(constants.CodeValidationFailed, "Hearing[court] schedule: court is required")
	}
	if in.Courtroom == "" {
		return util.NewAppError(constants.CodeValidationFailed, "Hearing[courtroom] schedule: courtroom is required")
	}
	if in.HearingTime.IsZero() {
		return util.NewAppError(constants.CodeValidationFailed, "Hearing[hearing_time] schedule: hearing_time is required")
	}
	if !in.HearingTime.After(time.Now()) {
		return util.NewAppError(constants.CodeValidationFailed, "Hearing[hearing_time] schedule: must be a future time")
	}
	return nil
}

func isClosedOrArchived(status string) bool {
	return status == constants.CaseStatusClosed || status == constants.CaseStatusArchived
}

func conflictAppError(lawyerID, conflictID uint64) error {
	return util.NewAppError(constants.CodeHearingConflict,
		fmt.Sprintf("Hearing[lead_lawyer_id=%d] schedule rejected: interval < 2h with hearing[id=%d]",
			lawyerID, conflictID))
}

func genHearingNo() string {
	return fmt.Sprintf("HEAR%d%06d", time.Now().Year(), time.Now().UnixNano()%1000000)
}
