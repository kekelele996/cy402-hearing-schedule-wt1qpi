package service

import (
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"cylawcase/internal/constants"
	"cylawcase/internal/model"
	"cylawcase/internal/repository"
	"cylawcase/internal/util"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// gormLoggerDiscard 静默 GORM SQL 日志（包括预期内的 ErrRecordNotFound）。
func gormLoggerDiscard() logger.Interface {
	return logger.New(slog.NewLogLogger(slog.NewTextHandler(io.Discard, nil), slog.LevelError),
		logger.Config{IgnoreRecordNotFoundError: true})
}

// newHearingTestDB 构造 SQLite 测试库（文件模式，WAL + 忙等待，可跨连接串行写入）。
func newHearingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "hearing.db") + "?_busy_timeout=5000&_journal_mode=WAL"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: gormLoggerDiscard()})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Client{}, &model.Case{}, &model.Hearing{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func newHearingFixture(t *testing.T) (*HearingService, *gorm.DB, uint64, uint64, uint64) {
	db := newHearingTestDB(t)
	lawyer := model.User{Username: "lawyer1", RealName: "张律师", Role: constants.RoleLawyer}
	if err := db.Create(&lawyer).Error; err != nil {
		t.Fatalf("create lawyer: %v", err)
	}
	client := model.Client{Name: "测试客户"}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	mkCase := func(status string) uint64 {
		c := model.Case{CaseNo: "CASE" + status + "-" + time.Now().Format("150405.000000"), Title: "测试案件-" + status,
			CaseType: constants.CaseTypeCivil, Status: status, ClientID: client.ID, LeadLawyerID: lawyer.ID}
		if err := db.Create(&c).Error; err != nil {
			t.Fatalf("create case: %v", err)
		}
		return c.ID
	}
	openCaseID := mkCase(constants.CaseStatusInvestigating)
	closedCaseID := mkCase(constants.CaseStatusClosed)
	svc := NewHearingService(repository.NewHearingRepository(db),
		repository.NewCaseRepository(db), repository.NewUserRepository(db), testLogger())
	return svc, db, openCaseID, closedCaseID, lawyer.ID
}

func appCode(t *testing.T, err error) int {
	t.Helper()
	var appErr *util.AppError
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	t.Fatalf("expected AppError, got %v", err)
	return 0
}

func countScheduled(t *testing.T, db *gorm.DB, rootID uint64) int {
	var n int64
	db.Model(&model.Hearing{}).Where("root_id = ? AND status = ?", rootID, constants.HearingStatusScheduled).Count(&n)
	return int(n)
}

// TestScheduleBasic 正常排期并校验必备字段。
func TestScheduleBasic(t *testing.T) {
	svc, db, caseID, _, lawyerID := newHearingFixture(t)
	at := time.Now().Add(48 * time.Hour).Truncate(time.Hour)
	h, err := svc.Schedule(caseID, lawyerID, at, "深圳市中级人民法院", "第 1 法庭")
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if h.Status != constants.HearingStatusScheduled || h.RootID != h.ID || h.Seq != 1 {
		t.Fatalf("unexpected hearing: %+v", h)
	}
	if h.Court == "" || h.Courtroom == "" {
		t.Fatal("court and courtroom required")
	}
	var stored model.Hearing
	db.First(&stored, h.ID)
	if !stored.HearingTime.Equal(at) || stored.LeadLawyerID != lawyerID {
		t.Fatalf("stored hearing mismatch: %+v", stored)
	}
}

// TestScheduleConflictRejected 间隔不足两小时整次拒绝，原排期不变。
func TestScheduleConflictRejected(t *testing.T) {
	svc, db, caseID, _, lawyerID := newHearingFixture(t)
	base := time.Now().Add(72 * time.Hour).Truncate(time.Hour)
	if _, err := svc.Schedule(caseID, lawyerID, base, "法院A", "1 庭"); err != nil {
		t.Fatalf("first schedule: %v", err)
	}
	_, err := svc.Schedule(caseID, lawyerID, base.Add(119*time.Minute), "法院B", "2 庭")
	if code := appCode(t, err); code != constants.CodeHearingConflict {
		t.Fatalf("expected conflict code %d, got %d", constants.CodeHearingConflict, code)
	}
	var n int64
	db.Model(&model.Hearing{}).Where("lead_lawyer_id = ?", lawyerID).Count(&n)
	if n != 1 {
		t.Fatalf("original schedule must stay unchanged, got %d hearings", n)
	}
}

// TestScheduleTwoHourBoundary 间隔恰为两小时允许（仅“不足”两小时才拒绝）。
func TestScheduleTwoHourBoundary(t *testing.T) {
	svc, _, caseID, _, lawyerID := newHearingFixture(t)
	base := time.Now().Add(96 * time.Hour).Truncate(time.Hour)
	if _, err := svc.Schedule(caseID, lawyerID, base, "法院A", "1 庭"); err != nil {
		t.Fatalf("first schedule: %v", err)
	}
	if _, err := svc.Schedule(caseID, lawyerID, base.Add(2*time.Hour), "法院B", "2 庭"); err != nil {
		t.Fatalf("exactly 2h gap should be allowed: %v", err)
	}
}

// TestScheduleClosedCaseFutureRejected 已结案/归档案件不能新增未来庭审。
func TestScheduleClosedCaseFutureRejected(t *testing.T) {
	svc, _, _, closedCaseID, lawyerID := newHearingFixture(t)
	_, err := svc.Schedule(closedCaseID, lawyerID, time.Now().Add(24*time.Hour), "法院", "1 庭")
	if code := appCode(t, err); code != constants.CodeHearingCaseClosed {
		t.Fatalf("expected closed code %d, got %d", constants.CodeHearingCaseClosed, code)
	}
}

// TestScheduleClosedCasePastAllowed 已结案件补录过去的历史场次允许。
func TestScheduleClosedCasePastAllowed(t *testing.T) {
	svc, _, _, closedCaseID, lawyerID := newHearingFixture(t)
	if _, err := svc.Schedule(closedCaseID, lawyerID, time.Now().Add(-24*time.Hour), "法院", "1 庭"); err != nil {
		t.Fatalf("past hearing on closed case should be allowed (backfill): %v", err)
	}
}

// TestRescheduleChain 改期只作废旧场次并生成新场次，待办只剩新场次。
func TestRescheduleChain(t *testing.T) {
	svc, db, caseID, _, lawyerID := newHearingFixture(t)
	old, err := svc.Schedule(caseID, lawyerID, time.Now().Add(24*time.Hour), "法院A", "1 庭")
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	newTime := time.Now().Add(72 * time.Hour)
	nw, err := svc.Reschedule(old.ID, newTime, "法院B", "3 庭")
	if err != nil {
		t.Fatalf("reschedule: %v", err)
	}
	if nw.RootID != old.RootID || nw.Seq != 2 || nw.RescheduledFrom != old.ID {
		t.Fatalf("chain broken: %+v", nw)
	}
	if nw.Status != constants.HearingStatusScheduled {
		t.Fatalf("new hearing must be scheduled: %+v", nw)
	}
	var voided model.Hearing
	db.First(&voided, old.ID)
	if voided.Status != constants.HearingStatusResched {
		t.Fatalf("old hearing must be rescheduled, got %s", voided.Status)
	}
	if countScheduled(t, db, old.RootID) != 1 {
		t.Fatal("chain must converge to exactly one scheduled hearing")
	}
	upcoming, err := svc.ListUpcoming(lawyerID, 0)
	if err != nil || len(upcoming) != 1 || upcoming[0].ID != nw.ID {
		t.Fatalf("upcoming must show only the new hearing, got %+v err=%v", upcoming, err)
	}
	history, err := svc.ListHistoryByCase(caseID)
	if err != nil || len(history) != 2 {
		t.Fatalf("history must keep both sessions, got %d err=%v", len(history), err)
	}
}

// TestConcurrentRescheduleConverges 同时到达的两个改期（都基于 seq=1 的旧快照）
// 必须收敛为一条有效记录：一个成功，另一个并发冲突失败。
func TestConcurrentRescheduleConverges(t *testing.T) {
	svc, db, caseID, _, lawyerID := newHearingFixture(t)
	old, err := svc.Schedule(caseID, lawyerID, time.Now().Add(24*time.Hour), "法院A", "1 庭")
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	// 两个改期请求都读到 seq=1 的同一快照。
	snapshotSeq := old.Seq
	type rc struct {
		id  uint64
		err error
	}
	resCh := make(chan rc, 2)
	repo := repository.NewHearingRepository(db)
	run := func(offset time.Duration) {
		// 直接调用仓储并传入过期快照序号，模拟同时到达且互不等待的两个改期。
		h, e := repo.Reschedule(old.ID, snapshotSeq, time.Now().Add(offset), "法院X", "9 庭")
		if h != nil {
			resCh <- rc{h.ID, e}
		} else {
			resCh <- rc{0, e}
		}
	}
	go run(72 * time.Hour)
	go run(120 * time.Hour)
	var ok, fail int
	for i := 0; i < 2; i++ {
		r := <-resCh
		if r.err == nil {
			ok++
		} else if errors.Is(r.err, repository.ErrHearingConcurrent) || errors.Is(r.err, repository.ErrHearingInvalid) {
			// 失败方可能在 CAS 处被拒（Concurrent），也可能读到胜者已提交的状态（Invalid），
			// 两者都是 409 拒绝，关键不变量是最终只剩一条有效记录。
			fail++
		} else {
			t.Fatalf("unexpected reschedule error: %v", r.err)
		}
	}
	if ok != 1 || fail != 1 {
		t.Fatalf("expected exactly 1 success and 1 concurrent failure, got ok=%d fail=%d", ok, fail)
	}
	if countScheduled(t, db, old.RootID) != 1 {
		t.Fatal("concurrent reschedules must converge to one valid record")
	}
}

// TestRescheduleConflictKeepsOld 改期时间撞庭时拒绝，旧场次保持有效。
func TestRescheduleConflictKeepsOld(t *testing.T) {
	svc, db, caseID, _, lawyerID := newHearingFixture(t)
	h1, err := svc.Schedule(caseID, lawyerID, time.Now().Add(24*time.Hour), "法院A", "1 庭")
	if err != nil {
		t.Fatalf("schedule h1: %v", err)
	}
	h2, err := svc.Schedule(caseID, lawyerID, time.Now().Add(200*time.Hour), "法院B", "2 庭")
	if err != nil {
		t.Fatalf("schedule h2: %v", err)
	}
	// 把 h2 改到距 h1 仅 30 分钟，应拒绝。
	_, err = svc.Reschedule(h2.ID, h1.HearingTime.Add(30*time.Minute), "法院C", "3 庭")
	if code := appCode(t, err); code != constants.CodeHearingConflict {
		t.Fatalf("expected conflict, got code %d err %v", constants.CodeHearingConflict, err)
	}
	var fresh model.Hearing
	db.First(&fresh, h2.ID)
	if fresh.Status != constants.HearingStatusScheduled {
		t.Fatalf("old schedule must remain scheduled, got %s", fresh.Status)
	}
}

// TestRescheduleVoidedRejected 已作废场次不能再次改期。
func TestRescheduleVoidedRejected(t *testing.T) {
	svc, _, caseID, _, lawyerID := newHearingFixture(t)
	h, err := svc.Schedule(caseID, lawyerID, time.Now().Add(24*time.Hour), "法院A", "1 庭")
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if _, err := svc.Reschedule(h.ID, time.Now().Add(48*time.Hour), "法院B", "2 庭"); err != nil {
		t.Fatalf("first reschedule: %v", err)
	}
	_, err = svc.Reschedule(h.ID, time.Now().Add(72*time.Hour), "法院C", "3 庭")
	if code := appCode(t, err); code != constants.CodeHearingInvalid {
		t.Fatalf("expected invalid code %d, got %d", constants.CodeHearingInvalid, code)
	}
}

// TestCancel 取消后作废，不再出现在待办，且不能重复取消。
func TestCancel(t *testing.T) {
	svc, db, caseID, _, lawyerID := newHearingFixture(t)
	h, err := svc.Schedule(caseID, lawyerID, time.Now().Add(24*time.Hour), "法院A", "1 庭")
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if _, err := svc.Cancel(h.ID, "当事人申请"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	upcoming, _ := svc.ListUpcoming(lawyerID, 0)
	if len(upcoming) != 0 {
		t.Fatalf("canceled hearing must not appear in upcoming, got %d", len(upcoming))
	}
	var stored model.Hearing
	db.First(&stored, h.ID)
	if stored.Status != constants.HearingStatusCanceled {
		t.Fatalf("status must be canceled, got %s", stored.Status)
	}
	if _, err := svc.Cancel(h.ID, "再次取消"); appCode(t, err) != constants.CodeHearingInvalid {
		t.Fatalf("double cancel must be rejected as invalid")
	}
}

// TestScheduleBatchAtomicRollback 批量排期中任一场撞庭，整批回滚，原排期不变。
func TestScheduleBatchAtomicRollback(t *testing.T) {
	_, db, caseID, _, lawyerID := newHearingFixture(t)
	repo := repository.NewHearingRepository(db)
	base := time.Now().Add(300 * time.Hour).Truncate(time.Hour)
	// 已有一场：lawyer @ base
	if _, err := repo.ScheduleBatch([]repository.ScheduleInput{
		{CaseID: caseID, LeadLawyerID: lawyerID, HearingTime: base, Court: "法院", Courtroom: "1 庭"},
	}); err != nil {
		t.Fatalf("seed batch: %v", err)
	}
	// 新批量：第二场与已有场次相距 30 分钟。
	_, err := repo.ScheduleBatch([]repository.ScheduleInput{
		{CaseID: caseID, LeadLawyerID: lawyerID + 1, HearingTime: base.Add(400 * time.Hour), Court: "法院", Courtroom: "2 庭"},
		{CaseID: caseID, LeadLawyerID: lawyerID, HearingTime: base.Add(30 * time.Minute), Court: "法院", Courtroom: "3 庭"},
	})
	if !errors.Is(err, repository.ErrHearingConflict) {
		t.Fatalf("expected ErrHearingConflict, got %v", err)
	}
	var n int64
	db.Model(&model.Hearing{}).Count(&n)
	if n != 1 {
		t.Fatalf("batch must rollback entirely, expected 1 row, got %d", n)
	}
}

// TestScheduleCaseNotFound 不存在的案件排期返回 NotFound。
func TestScheduleCaseNotFound(t *testing.T) {
	svc, _, _, _, lawyerID := newHearingFixture(t)
	_, err := svc.Schedule(999999, lawyerID, time.Now().Add(24*time.Hour), "法院", "1 庭")
	if code := appCode(t, err); code != constants.CodeNotFound {
		t.Fatalf("expected not found code %d, got %d", constants.CodeNotFound, code)
	}
}
