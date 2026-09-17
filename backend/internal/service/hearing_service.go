package service

import (
	"errors"
	"log/slog"
	"time"

	"cylawcase/internal/constants"
	"cylawcase/internal/model"
	"cylawcase/internal/repository"
	"cylawcase/internal/util"
)

// HearingService 庭审排期业务逻辑。
type HearingService struct {
	repo     *repository.HearingRepository
	caseRepo *repository.CaseRepository
	userRepo *repository.UserRepository
	logger   *slog.Logger
}

// NewHearingService 构造庭审排期服务。
func NewHearingService(repo *repository.HearingRepository, caseRepo *repository.CaseRepository,
	userRepo *repository.UserRepository, logger *slog.Logger) *HearingService {
	return &HearingService{repo: repo, caseRepo: caseRepo, userRepo: userRepo, logger: logger}
}

// Schedule 新建庭审排期。leadLawyerID 为 0 时取案件主办律师。
// 任一条约束不满足都整体失败，不会留下半截数据。
func (s *HearingService) Schedule(caseID, leadLawyerID uint64, hearingTime time.Time, court, courtroom string) (*model.Hearing, error) {
	c, err := s.caseRepo.FindByID(caseID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, util.NewAppError(constants.CodeNotFound, constants.MsgNotFound)
		}
		return nil, util.Wrap(err, "Hearing[case_id=%d] schedule: case not found", caseID)
	}
	if leadLawyerID == 0 {
		leadLawyerID = c.LeadLawyerID
	} else if _, err := s.userRepo.FindByID(leadLawyerID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, util.NewAppError(constants.CodeNotFound, "主办律师不存在")
		}
		return nil, util.Wrap(err, "Hearing[lead_lawyer_id=%d] schedule: lawyer not found", leadLawyerID)
	}
	if err := assertFutureAllowed(c.Status, hearingTime); err != nil {
		return nil, err
	}
	created, err := s.repo.ScheduleBatch([]repository.ScheduleInput{{
		CaseID: caseID, LeadLawyerID: leadLawyerID,
		HearingTime: hearingTime, Court: court, Courtroom: courtroom,
	}})
	if err != nil {
		s.logger.Warn(constants.LogHearingScheduleFailed, "case_id", caseID, "error", err.Error())
		return nil, mapHearingRepoError(err, "Hearing[case_id=%d] schedule failed", caseID)
	}
	h := created[0]
	s.logger.Info(constants.LogHearingScheduleSuccess, "hearing_id", h.ID, "case_id", caseID, "lead_lawyer_id", leadLawyerID)
	return &h, nil
}

// Reschedule 改期：只作废旧场次并生成新场次，不原地修改。
// court/courtroom 为空时沿用旧场次值。
func (s *HearingService) Reschedule(id uint64, newTime time.Time, court, courtroom string) (*model.Hearing, error) {
	old, err := s.repo.FindByID(id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, util.NewAppError(constants.CodeNotFound, constants.MsgNotFound)
		}
		return nil, util.Wrap(err, "Hearing[id=%d] reschedule find failed", id)
	}
	c, err := s.caseRepo.FindByID(old.CaseID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, util.NewAppError(constants.CodeNotFound, constants.MsgNotFound)
		}
		return nil, util.Wrap(err, "Hearing[id=%d] reschedule: case not found", id)
	}
	if err := assertFutureAllowed(c.Status, newTime); err != nil {
		return nil, err
	}
	if court == "" {
		court = old.Court
	}
	if courtroom == "" {
		courtroom = old.Courtroom
	}
	s.logger.Info(constants.LogHearingRescheduleStart, "hearing_id", id, "seq", old.Seq, "new_time", newTime.Format(time.RFC3339))
	h, err := s.repo.Reschedule(id, old.Seq, newTime, court, courtroom)
	if err != nil {
		s.logger.Warn(constants.LogHearingRescheduleFailed, "hearing_id", id, "error", err.Error())
		return nil, mapHearingRepoError(err, "Hearing[id=%d] reschedule failed", id)
	}
	s.logger.Info(constants.LogHearingRescheduleSuccess, "old_hearing_id", id, "new_hearing_id", h.ID, "root_id", h.RootID, "seq", h.Seq)
	return h, nil
}

// Cancel 取消场次（作废，不物理删除）。
func (s *HearingService) Cancel(id uint64, reason string) (*model.Hearing, error) {
	h, err := s.repo.Cancel(id, reason)
	if err != nil {
		s.logger.Warn(constants.LogHearingCancelFailed, "hearing_id", id, "error", err.Error())
		return nil, mapHearingRepoError(err, "Hearing[id=%d] cancel failed", id)
	}
	s.logger.Info(constants.LogHearingCancelSuccess, "hearing_id", id, "root_id", h.RootID)
	return h, nil
}

// Get 场次详情。
func (s *HearingService) Get(id uint64) (*model.Hearing, error) {
	h, err := s.repo.FindByID(id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, util.NewAppError(constants.CodeNotFound, constants.MsgNotFound)
	}
	return h, err
}

// List 分页查询。
func (s *HearingService) List(page, pageSize int, caseID, lawyerID uint64, status string, from, to *time.Time, includeVoid bool) ([]model.Hearing, int64, error) {
	return s.repo.List(page, pageSize, caseID, lawyerID, status, from, to, includeVoid)
}

// ListUpcoming 未来有效庭审（待办唯一数据源，作废场次不会出现）。
func (s *HearingService) ListUpcoming(lawyerID uint64, limit int) ([]model.Hearing, error) {
	return s.repo.ListUpcoming(time.Now(), lawyerID, limit)
}

// ListHistoryByCase 某案件全部场次（含作废），用于追溯改期链。
func (s *HearingService) ListHistoryByCase(caseID uint64) ([]model.Hearing, error) {
	return s.repo.ListHistoryByCase(caseID)
}

// assertFutureAllowed 已结案或归档案件不能新增未来庭审；补录历史场次（时间在过去）不受限。
func assertFutureAllowed(caseStatus string, hearingTime time.Time) error {
	if !hearingTime.After(time.Now()) {
		return nil
	}
	if caseStatus == constants.CaseStatusClosed || caseStatus == constants.CaseStatusArchived {
		return util.NewAppError(constants.CodeHearingCaseClosed,
			"Hearing schedule rejected: case status="+caseStatus+" does not allow future hearings")
	}
	return nil
}

// mapHearingRepoError 将仓储哨兵错误翻译为统一错误码，其余错误原样包装。
func mapHearingRepoError(err error, format string, args ...any) error {
	switch {
	case errors.Is(err, repository.ErrHearingConflict):
		return util.NewAppError(constants.CodeHearingConflict, constants.MsgHearingConflict)
	case errors.Is(err, repository.ErrHearingInvalid):
		return util.NewAppError(constants.CodeHearingInvalid, constants.MsgHearingInvalid)
	case errors.Is(err, repository.ErrHearingConcurrent):
		return util.NewAppError(constants.CodeHearingConcurrent, constants.MsgHearingConcurrent)
	case errors.Is(err, repository.ErrNotFound):
		return util.NewAppError(constants.CodeNotFound, constants.MsgNotFound)
	default:
		return util.Wrap(err, format, args...)
	}
}
