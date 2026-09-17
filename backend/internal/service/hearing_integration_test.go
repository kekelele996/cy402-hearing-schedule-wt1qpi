package service

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	embedded_postgres "github.com/fergusstrange/embedded-postgres"

	"cylawcase/internal/constants"
	"cylawcase/internal/model"
	"cylawcase/internal/repository"
	"cylawcase/internal/util"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// 集成测试通过 embedded-postgres（首次运行从 Maven 中央仓库下载官方 PG 二进制）
// 启动真实 PostgreSQL，验证事务、咨询锁、唯一索引与并发改期收敛。

var (
	testDB     *gorm.DB
	testPG     *embedded_postgres.EmbeddedPostgres
	testLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
)

func TestMain(m *testing.M) {
	cfg := embedded_postgres.DefaultConfig().
		Port(55433).
		Username("postgres").
		Password("postgres").
		Database("cylawcase_test").
		StartTimeout(120 * time.Second)
	pg := embedded_postgres.NewDatabase(cfg)
	if err := pg.Start(); err != nil {
		// 无网络/无法下载二进制时跳过集成测试，不阻断纯逻辑测试。
		slog.Warn("embedded postgres unavailable, skipping integration tests", "error", err)
		os.Exit(m.Run())
	}
	testPG = pg
	dsn := "host=127.0.0.1 user=postgres password=postgres dbname=cylawcase_test port=55433 sslmode=disable TimeZone=Asia/Shanghai"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		slog.Error("connect test db failed", "error", err)
		_ = pg.Stop()
		os.Exit(1)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Client{}, &model.Case{},
		&model.Hearing{}, &model.AuditLog{}); err != nil {
		slog.Error("migrate test db failed", "error", err)
		_ = pg.Stop()
		os.Exit(1)
	}
	testDB = db
	code := m.Run()
	_ = pg.Stop()
	os.Exit(code)
}

// hearingFixture 每个用例独立的律师/客户/案件与服务。
type hearingFixture struct {
	svc      *HearingService
	caseID   uint64
	lawyerID uint64
}

// newHearingTestEnv 每个用例使用独立律师与案件，避免相互干扰。
func newHearingTestEnv(t *testing.T, caseStatus string) hearingFixture {
	t.Helper()
	if testDB == nil {
		t.Skip("embedded postgres not available")
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	lawyer := &model.User{Username: "lawyer_" + suffix, RealName: "测试律师", Role: constants.RoleLawyer}
	if err := testDB.Create(lawyer).Error; err != nil {
		t.Fatalf("create lawyer: %v", err)
	}
	client := &model.Client{Name: "测试客户_" + suffix}
	if err := testDB.Create(client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	cs := &model.Case{
		CaseNo:       "CYT" + suffix,
		Title:        "测试案件",
		CaseType:     constants.CaseTypeCivil,
		Status:       caseStatus,
		ClientID:     client.ID,
		LeadLawyerID: lawyer.ID,
		CoLawyerIDs:  model.CoLawyerJSON("[]"),
	}
	if err := testDB.Create(cs).Error; err != nil {
		t.Fatalf("create case: %v", err)
	}
	hearingRepo := repository.NewHearingRepository(testDB)
	caseRepo := repository.NewCaseRepository(testDB)
	return hearingFixture{
		svc:      NewHearingService(hearingRepo, caseRepo, testLogger),
		caseID:   cs.ID,
		lawyerID: lawyer.ID,
	}
}

// at 以今天为基准构造第 day 天（0=今天）hour:min 的本地时间，保证落在未来。
func at(day, hour, min int) time.Time {
	now := time.Now()
	base := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, time.Local)
	t := base.AddDate(0, 0, day)
	if !t.After(now.Add(time.Minute)) {
		t = base.AddDate(0, 0, day+1)
	}
	return t
}

func scheduleInput(day, hour, min int) ScheduleInput {
	return ScheduleInput{HearingTime: at(day, hour, min), Court: "深圳市中级人民法院", Courtroom: "第17法庭"}
}

// isHearingConflict 判断错误链中是否携带撞庭业务错误码。
func isHearingConflict(err error) bool {
	var ae *util.AppError
	return asAppError(err, &ae) && ae.Code == constants.CodeHearingConflict
}

func asAppError(err error, target **util.AppError) bool {
	for err != nil {
		if ae, ok := err.(*util.AppError); ok {
			*target = ae
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// TestHearingScheduleSuccess 基本字段齐备时排期成功。
func TestHearingScheduleSuccess(t *testing.T) {
	f := newHearingTestEnv(t, constants.CaseStatusInvestigating)
	h, err := f.svc.Schedule(f.caseID, scheduleInput(2, 9, 0))
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if h.Status != constants.HearingStatusScheduled || h.HearingNo == "" {
		t.Fatalf("unexpected hearing: %+v", h)
	}
	if h.Court == "" || h.Courtroom == "" {
		t.Fatal("court/courtroom must be persisted")
	}
	if h.LeadLawyerID != f.lawyerID {
		t.Fatalf("lead lawyer mismatch: got %d want %d", h.LeadLawyerID, f.lawyerID)
	}
}

// TestHearingScheduleConflictRejects 间隔不足两小时整次拒绝，原排期不变。
func TestHearingScheduleConflictRejects(t *testing.T) {
	f := newHearingTestEnv(t, constants.CaseStatusFiled)
	if _, err := f.svc.Schedule(f.caseID, scheduleInput(3, 9, 0)); err != nil {
		t.Fatalf("first schedule: %v", err)
	}
	before := countAll(t, f.lawyerID)
	// 90 分钟后 → 撞庭
	_, err := f.svc.Schedule(f.caseID, scheduleInput(3, 10, 30))
	if !isHearingConflict(err) {
		t.Fatalf("expect conflict error, got %v", err)
	}
	if got := countAll(t, f.lawyerID); got != before {
		t.Fatalf("record count changed after rejection: before=%d after=%d", before, got)
	}
	// 前 119 分钟同样撞庭
	if _, err := f.svc.Schedule(f.caseID, scheduleInput(3, 7, 1)); !isHearingConflict(err) {
		t.Fatalf("expect conflict for -119m, got %v", err)
	}
}

// TestHearingScheduleExactlyTwoHoursAllowed 恰好两小时间隔允许（边界）。
func TestHearingScheduleExactlyTwoHoursAllowed(t *testing.T) {
	f := newHearingTestEnv(t, constants.CaseStatusHearing)
	if _, err := f.svc.Schedule(f.caseID, scheduleInput(4, 9, 0)); err != nil {
		t.Fatalf("first schedule: %v", err)
	}
	if _, err := f.svc.Schedule(f.caseID, scheduleInput(4, 11, 0)); err != nil {
		t.Fatalf("exactly 2h apart should be allowed, got %v", err)
	}
}

// TestHearingScheduleClosedCaseRejected 已结案/归档案件不能新增未来庭审。
func TestHearingScheduleClosedCaseRejected(t *testing.T) {
	for _, st := range []string{constants.CaseStatusClosed, constants.CaseStatusArchived} {
		f := newHearingTestEnv(t, st)
		_, err := f.svc.Schedule(f.caseID, scheduleInput(5, 9, 0))
		var ae *util.AppError
		if !asAppError(err, &ae) || ae.Code != constants.CodeHearingCaseClosed {
			t.Fatalf("status=%s expect case-closed rejection, got %v", st, err)
		}
		if countAll(t, f.lawyerID) != 0 {
			t.Fatalf("status=%s must not create any hearing", st)
		}
	}
}

// TestHearingRescheduleKeepsOldAndCreatesNew 改期作废旧场次并生成新场次。
func TestHearingRescheduleKeepsOldAndCreatesNew(t *testing.T) {
	f := newHearingTestEnv(t, constants.CaseStatusInvestigating)
	old, err := f.svc.Schedule(f.caseID, scheduleInput(6, 9, 0))
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	nw, err := f.svc.Reschedule(old.ID, scheduleInput(6, 14, 0))
	if err != nil {
		t.Fatalf("reschedule: %v", err)
	}
	if nw.RescheduledFromID == nil || *nw.RescheduledFromID != old.ID {
		t.Fatalf("new hearing must point to old id %d, got %+v", old.ID, nw.RescheduledFromID)
	}
	// 旧场次仍在库中但已 cancelled
	old2, _ := f.svc.Get(old.ID)
	if old2.Status != constants.HearingStatusCancelled || old2.CancelledAt == nil {
		t.Fatalf("old hearing must be cancelled with cancelled_at, got %+v", old2)
	}
	if old2.HearingTime.Equal(nw.HearingTime) {
		t.Fatal("old hearing time must remain unchanged (reschedule must not mutate old record)")
	}
	// 待办视图只有新场次
	up, err := f.svc.ListUpcoming(f.lawyerID)
	if err != nil {
		t.Fatalf("upcoming: %v", err)
	}
	if len(up) != 1 || up[0].ID != nw.ID {
		t.Fatalf("upcoming must contain only the new hearing, got %+v", up)
	}
}

// TestHearingRescheduleConflictReverts 改期撞庭时整次回滚，原排期不变。
func TestHearingRescheduleConflictReverts(t *testing.T) {
	f := newHearingTestEnv(t, constants.CaseStatusInvestigating)
	a, err := f.svc.Schedule(f.caseID, scheduleInput(7, 9, 0))
	if err != nil {
		t.Fatalf("schedule a: %v", err)
	}
	// 另一场 15:00（与 9:00 间隔 6 小时，合法）
	b, err := f.svc.Schedule(f.caseID, scheduleInput(7, 15, 0))
	if err != nil {
		t.Fatalf("schedule b: %v", err)
	}
	// 把 a 改到 15:30，与 b 仅隔 30 分钟 → 拒绝；a 必须仍 scheduled
	_, err = f.svc.Reschedule(a.ID, scheduleInput(7, 15, 30))
	if !isHearingConflict(err) {
		t.Fatalf("expect conflict, got %v", err)
	}
	a2, _ := f.svc.Get(a.ID)
	if a2.Status != constants.HearingStatusScheduled {
		t.Fatalf("original schedule must remain intact, got status=%s", a2.Status)
	}
	b2, _ := f.svc.Get(b.ID)
	if b2.Status != constants.HearingStatusScheduled {
		t.Fatalf("unrelated hearing must stay scheduled")
	}
}

// TestHearingConcurrentRescheduleConverges 同时到达的两个改期收敛为一条有效记录。
func TestHearingConcurrentRescheduleConverges(t *testing.T) {
	f := newHearingTestEnv(t, constants.CaseStatusInvestigating)
	old, err := f.svc.Schedule(f.caseID, scheduleInput(8, 9, 0))
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = f.svc.Reschedule(old.ID, scheduleInput(8, 10, 30))
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = f.svc.Reschedule(old.ID, scheduleInput(8, 11, 30))
	}()
	wg.Wait()

	okCount, failCount := 0, 0
	for _, e := range errs {
		if e == nil {
			okCount++
		} else if isHearingConflict(e) {
			failCount++
		} else {
			t.Fatalf("unexpected error: %v", e)
		}
	}
	if okCount != 1 || failCount != 1 {
		t.Fatalf("expect exactly 1 success & 1 conflict, got ok=%d fail=%d (%v %v)", okCount, failCount, errs[0], errs[1])
	}

	// 库内最终态：旧场次 cancelled，后继 scheduled 恰有一条
	var scheduled, cancelled int64
	testDB.Model(&model.Hearing{}).Where("lead_lawyer_id = ?", f.lawyerID).
		Where("status = ?", constants.HearingStatusScheduled).Count(&scheduled)
	testDB.Model(&model.Hearing{}).Where("lead_lawyer_id = ?", f.lawyerID).
		Where("status = ?", constants.HearingStatusCancelled).Count(&cancelled)
	if scheduled != 1 || cancelled != 1 {
		t.Fatalf("converge expected 1 scheduled + 1 cancelled, got scheduled=%d cancelled=%d", scheduled, cancelled)
	}
	var succ int64
	testDB.Model(&model.Hearing{}).Where("rescheduled_from_id = ?", old.ID).Count(&succ)
	if succ != 1 {
		t.Fatalf("old hearing must have exactly one successor, got %d", succ)
	}
}

// TestHearingConcurrentSchedulesNoOverlap 并发排期不得产生两场间隔不足的有效庭审。
func TestHearingConcurrentSchedulesNoOverlap(t *testing.T) {
	f := newHearingTestEnv(t, constants.CaseStatusInvestigating)
	// 两个请求相差 30 分钟并发到达，咨询锁串行化后只允许一场成功
	t1 := at(10, 9, 0)
	t2 := t1.Add(30 * time.Minute)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = f.svc.Schedule(f.caseID, ScheduleInput{HearingTime: t1, Court: "c", Courtroom: "r"})
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = f.svc.Schedule(f.caseID, ScheduleInput{HearingTime: t2, Court: "c", Courtroom: "r"})
	}()
	wg.Wait()

	ok := 0
	for _, e := range errs {
		if e == nil {
			ok++
		} else if !isHearingConflict(e) {
			t.Fatalf("unexpected error: %v", e)
		}
	}
	if ok != 1 {
		t.Fatalf("expect exactly one concurrent schedule to succeed, got %d", ok)
	}
}

func countAll(t *testing.T, lawyerID uint64) int64 {
	t.Helper()
	var n int64
	if err := testDB.Model(&model.Hearing{}).Where("lead_lawyer_id = ?", lawyerID).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// TestHearingRescheduleBlockedAfterCaseClosed 排期后案件结案，改期同样被拒绝。
func TestHearingRescheduleBlockedAfterCaseClosed(t *testing.T) {
	f := newHearingTestEnv(t, constants.CaseStatusInvestigating)
	old, err := f.svc.Schedule(f.caseID, scheduleInput(11, 9, 0))
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if err := testDB.Model(&model.Case{}).Where("id = ?", f.caseID).
		Update("status", constants.CaseStatusClosed).Error; err != nil {
		t.Fatalf("close case: %v", err)
	}
	_, err = f.svc.Reschedule(old.ID, scheduleInput(12, 9, 0))
	var ae *util.AppError
	if !asAppError(err, &ae) || ae.Code != constants.CodeHearingCaseClosed {
		t.Fatalf("expect case-closed rejection on reschedule, got %v", err)
	}
	still, _ := f.svc.Get(old.ID)
	if still.Status != constants.HearingStatusScheduled {
		t.Fatalf("original hearing must stay scheduled after rejected reschedule")
	}
}
