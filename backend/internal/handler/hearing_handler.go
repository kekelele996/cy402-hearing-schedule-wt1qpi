package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"cylawcase/internal/constants"
	"cylawcase/internal/dto"
	"cylawcase/internal/repository"
	"cylawcase/internal/service"
	"cylawcase/internal/util"

	"github.com/gin-gonic/gin"
)

// HearingHandler 庭审排期 HTTP 处理器。
type HearingHandler struct {
	svc    *service.HearingService
	logger *slog.Logger
}

// NewHearingHandler 构造庭审处理器。
func NewHearingHandler(svc *service.HearingService, logger *slog.Logger) *HearingHandler {
	return &HearingHandler{svc: svc, logger: logger}
}

// List 场次列表（默认返回全部状态；律师待办请用 /hearings/upcoming）。
func (h *HearingHandler) List(c *gin.Context) {
	var q dto.PageQuery
	_ = c.ShouldBindQuery(&q)
	q.Normalize()
	caseID, _ := strconv.ParseUint(c.Query("case_id"), 10, 64)
	lawyerID, _ := strconv.ParseUint(c.Query("lead_lawyer_id"), 10, 64)
	startTime, endTime := parseHearingRange(c)
	list, total, err := h.svc.List(q.Page, q.PageSize, caseID, lawyerID, c.Query("status"), startTime, endTime)
	if err != nil {
		h.wrapError(c, err, "Hearing list failed")
		return
	}
	OK(c, pageResponse(list, total, q.Page, q.PageSize))
}

// Upcoming 律师待办：当前时间之后的有效场次，cancelled 旧场次不会出现。
func (h *HearingHandler) Upcoming(c *gin.Context) {
	lawyerID, err := strconv.ParseUint(c.Query("lead_lawyer_id"), 10, 64)
	if err != nil || lawyerID == 0 {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "Hearing upcoming: lead_lawyer_id is required")
		return
	}
	list, err := h.svc.ListUpcoming(lawyerID)
	if err != nil {
		h.wrapError(c, err, "Hearing upcoming failed")
		return
	}
	OK(c, list)
}

// Get 场次详情。
func (h *HearingHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "Hearing[id] get: invalid id")
		return
	}
	h2, err := h.svc.Get(id)
	if err != nil {
		h.wrapError(c, err, "Hearing get failed")
		return
	}
	OK(c, h2)
}

// Schedule 新增庭审排期。撞庭/案件已结案时整次拒绝，响应 409。
func (h *HearingHandler) Schedule(c *gin.Context) {
	var req dto.HearingScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "Hearing schedule: "+err.Error())
		return
	}
	hearingTime, err := dto.ParseHearingTime(req.HearingTime)
	if err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "Hearing schedule: invalid hearing_time")
		return
	}
	h2, err := h.svc.Schedule(req.CaseID, service.ScheduleInput{
		HearingTime: hearingTime, Court: req.Court, Courtroom: req.Courtroom,
	})
	if err != nil {
		h.wrapError(c, err, "Hearing[case_id="+strconv.FormatUint(req.CaseID, 10)+"] schedule failed")
		return
	}
	OKWithMessage(c, constants.MsgHearingScheduled, h2)
}

// Reschedule 改期：作废旧场次并生成新场次。
func (h *HearingHandler) Reschedule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "Hearing[id] reschedule: invalid id")
		return
	}
	var req dto.HearingRescheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest,
			"Hearing[id="+strconv.FormatUint(id, 10)+"] reschedule: "+err.Error())
		return
	}
	hearingTime, err := dto.ParseHearingTime(req.HearingTime)
	if err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "Hearing reschedule: invalid hearing_time")
		return
	}
	h2, err := h.svc.Reschedule(id, service.ScheduleInput{
		HearingTime: hearingTime, Court: req.Court, Courtroom: req.Courtroom,
	})
	if err != nil {
		h.wrapError(c, err, "Hearing[id="+strconv.FormatUint(id, 10)+"] reschedule failed")
		return
	}
	OKWithMessage(c, constants.MsgHearingRescheduled, h2)
}

// parseHearingRange 解析列表时间范围（日期或 RFC3339）。
func parseHearingRange(c *gin.Context) (*time.Time, *time.Time) {
	var start, end *time.Time
	if s := c.Query("start_time"); s != "" {
		if t, err := dto.ParseHearingTime(s); err == nil {
			start = &t
		} else if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
			start = &t
		}
	}
	if s := c.Query("end_time"); s != "" {
		if t, err := dto.ParseHearingTime(s); err == nil {
			end = &t
		} else if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
			end = &t
		}
	}
	return start, end
}

func (h *HearingHandler) wrapError(c *gin.Context, err error, ctx string) {
	var appErr *util.AppError
	if errors.As(err, &appErr) {
		c.Set("audit_detail", appErr.Message)
		h.logger.Warn("hearing handler error", "context", ctx, "error", appErr.Error())
		Fail(c, appErrorStatus(appErr.Code), appErr.Code, appErr.Message)
		return
	}
	if errors.Is(err, repository.ErrNotFound) {
		Fail(c, http.StatusNotFound, constants.CodeNotFound, "Hearing not found")
		return
	}
	h.logger.Error("hearing handler error", "context", ctx, "error", err.Error())
	Fail(c, http.StatusInternalServerError, constants.CodeInternalError, constants.MsgInternalError)
}
