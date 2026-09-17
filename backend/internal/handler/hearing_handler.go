package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"cylawcase/internal/constants"
	"cylawcase/internal/dto"
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

// List 庭审场次分页列表（默认仅有效场次）。
func (h *HearingHandler) List(c *gin.Context) {
	var q dto.PageQuery
	_ = c.ShouldBindQuery(&q)
	q.Normalize()
	caseID, _ := strconv.ParseUint(c.Query("case_id"), 10, 64)
	lawyerID, _ := strconv.ParseUint(c.Query("lead_lawyer_id"), 10, 64)
	includeVoid := c.Query("include_void") == "true" || c.Query("include_void") == "1"
	var from, to *time.Time
	if s := c.Query("from"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			from = &t
		}
	}
	if s := c.Query("to"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			to = &t
		}
	}
	list, total, err := h.svc.List(q.Page, q.PageSize, caseID, lawyerID, c.Query("status"), from, to, includeVoid)
	if err != nil {
		h.wrapError(c, err, "Hearing list failed")
		return
	}
	OK(c, pageResponse(list, total, q.Page, q.PageSize))
}

// Upcoming 未来有效庭审（律师待办）。
func (h *HearingHandler) Upcoming(c *gin.Context) {
	lawyerID, _ := strconv.ParseUint(c.Query("lead_lawyer_id"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	list, err := h.svc.ListUpcoming(lawyerID, limit)
	if err != nil {
		h.wrapError(c, err, "Hearing upcoming failed")
		return
	}
	OK(c, list)
}

// HistoryByCase 某案件全部场次（含作废，改期链追溯）。
func (h *HearingHandler) HistoryByCase(c *gin.Context) {
	caseID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "Hearing history by case: invalid case id")
		return
	}
	list, err := h.svc.ListHistoryByCase(caseID)
	if err != nil {
		h.wrapError(c, err, "Hearing history by case failed")
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
	hearing, err := h.svc.Get(id)
	if err != nil {
		h.wrapError(c, err, "Hearing get failed")
		return
	}
	OK(c, hearing)
}

// Schedule 新建排期。
func (h *HearingHandler) Schedule(c *gin.Context) {
	var req dto.HearingScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "Hearing schedule: "+err.Error())
		return
	}
	t, err := time.Parse(time.RFC3339, req.HearingTime)
	if err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "Hearing schedule: hearing_time must be RFC3339")
		return
	}
	hearing, err := h.svc.Schedule(req.CaseID, req.LeadLawyerID, t, req.Court, req.Courtroom)
	if err != nil {
		h.wrapError(c, err, "Hearing schedule failed")
		return
	}
	OKWithMessage(c, constants.MsgHearingScheduled, hearing)
}

// Reschedule 改期。
func (h *HearingHandler) Reschedule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "Hearing[id] reschedule: invalid id")
		return
	}
	var req dto.HearingRescheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "Hearing reschedule: "+err.Error())
		return
	}
	t, err := time.Parse(time.RFC3339, req.HearingTime)
	if err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "Hearing reschedule: hearing_time must be RFC3339")
		return
	}
	hearing, err := h.svc.Reschedule(id, t, req.Court, req.Courtroom)
	if err != nil {
		h.wrapError(c, err, "Hearing reschedule failed")
		return
	}
	OKWithMessage(c, constants.MsgHearingRescheduled, hearing)
}

// Cancel 取消场次。
func (h *HearingHandler) Cancel(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "Hearing[id] cancel: invalid id")
		return
	}
	var req dto.HearingCancelRequest
	_ = c.ShouldBindJSON(&req)
	hearing, err := h.svc.Cancel(id, req.Reason)
	if err != nil {
		h.wrapError(c, err, "Hearing cancel failed")
		return
	}
	OKWithMessage(c, constants.MsgHearingCanceled, hearing)
}

func (h *HearingHandler) wrapError(c *gin.Context, err error, ctx string) {
	var appErr *util.AppError
	if errors.As(err, &appErr) {
		c.Set("audit_detail", appErr.Message)
		h.logger.Warn("hearing handler error", "context", ctx, "error", appErr.Error())
		Fail(c, appErrorStatus(appErr.Code), appErr.Code, appErr.Message)
		return
	}
	h.logger.Error("hearing handler error", "context", ctx, "error", err.Error())
	Fail(c, http.StatusInternalServerError, constants.CodeInternalError, constants.MsgInternalError)
}
