package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	embedded_postgres "github.com/fergusstrange/embedded-postgres"

	"cylawcase/internal/config"
	"cylawcase/internal/constants"
	"cylawcase/internal/handler"
	"cylawcase/internal/model"
	"cylawcase/internal/repository"
	"cylawcase/internal/service"
	"cylawcase/internal/util"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// 端到端冒烟：真实 PostgreSQL + 完整 Gin 路由 + JWT，验证排期模块 HTTP 契约。

var e2eDB *gorm.DB

func TestMain(m *testing.M) {
	cfg := embedded_postgres.DefaultConfig().
		Port(55434).
		Username("postgres").
		Password("postgres").
		Database("cylawcase_e2e").
		StartTimeout(120 * time.Second)
	pg := embedded_postgres.NewDatabase(cfg)
	if err := pg.Start(); err != nil {
		slog.Warn("embedded postgres unavailable, skipping e2e tests", "error", err)
		os.Exit(m.Run())
	}
	dsn := "host=127.0.0.1 user=postgres password=postgres dbname=cylawcase_e2e port=55434 sslmode=disable TimeZone=Asia/Shanghai"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		fmt.Println("open e2e db:", err)
		_ = pg.Stop()
		os.Exit(1)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Client{}, &model.Case{}, &model.Document{},
		&model.Billing{}, &model.Hearing{}, &model.AuditLog{}); err != nil {
		fmt.Println("migrate e2e db:", err)
		_ = pg.Stop()
		os.Exit(1)
	}
	e2eDB = db
	code := m.Run()
	_ = pg.Stop()
	os.Exit(code)
}

func newE2EEngine(t *testing.T) (*gorm.DB, http.Handler, string) {
	t.Helper()
	if e2eDB == nil {
		t.Skip("embedded postgres not available")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{JWTSecret: "test-secret", ServerPort: "8099", UploadDir: t.TempDir(),
		RateLimitPerMinute: 9999, CORSOrigins: []string{"*"}}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	lawyer := &model.User{Username: "e2elawyer_" + suffix, RealName: "E2E律师", Role: constants.RoleLawyer}
	if err := e2eDB.Create(lawyer).Error; err != nil {
		t.Fatalf("create lawyer: %v", err)
	}
	client := &model.Client{Name: "E2E客户_" + suffix}
	if err := e2eDB.Create(client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	cs := &model.Case{CaseNo: "CYE" + suffix, Title: "E2E案件", CaseType: constants.CaseTypeCivil,
		Status: constants.CaseStatusInvestigating, ClientID: client.ID, LeadLawyerID: lawyer.ID,
		CoLawyerIDs: model.CoLawyerJSON("[]")}
	if err := e2eDB.Create(cs).Error; err != nil {
		t.Fatalf("create case: %v", err)
	}

	userRepo := repository.NewUserRepository(e2eDB)
	caseRepo := repository.NewCaseRepository(e2eDB)
	hearingRepo := repository.NewHearingRepository(e2eDB)
	userSvc := service.NewUserService(userRepo, logger)
	caseSvc := service.NewCaseService(caseRepo, repository.NewClientRepository(e2eDB), userRepo, logger)
	hearingSvc := service.NewHearingService(hearingRepo, caseRepo, logger)

	userH := handler.NewUserHandler(userSvc, logger)
	caseH := handler.NewCaseHandler(caseSvc, logger)
	hearingH := handler.NewHearingHandler(hearingSvc, logger)
	// 其余处理器传 nil 不会被注册路由触达（仅挂载排期相关需要的处理器）。
	r := New(cfg, e2eDB, logger, userH, nil, caseH, nil, nil, hearingH, nil, nil)
	token, _ := util.GenerateToken(cfg.JWTSecret, time.Hour, lawyer.ID, lawyer.Username, lawyer.Role)
	return e2eDB, r.Setup(), token
}

func doJSON(t *testing.T, h http.Handler, token, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w.Code, resp
}

func future(day, hour, min int) string {
	now := time.Now()
	base := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, time.Local).AddDate(0, 0, day)
	if !base.After(now.Add(time.Minute)) {
		base = base.AddDate(0, 0, 1)
	}
	return base.Format("2006-01-02 15:04")
}

func TestHearingHTTPFlow(t *testing.T) {
	db, h, token := newE2EEngine(t)
	var caseID uint64
	db.Model(&model.Case{}).Order("id DESC").Limit(1).Pluck("id", &caseID)

	// 1. 排期成功
	status, resp := doJSON(t, h, token, http.MethodPost, "/api/v1/hearings", map[string]any{
		"case_id": caseID, "hearing_time": future(3, 9, 0), "court": "深圳中院", "courtroom": "第1法庭",
	})
	if status != http.StatusOK {
		t.Fatalf("schedule status=%d resp=%v", status, resp)
	}
	firstID := uint64(resp["data"].(map[string]any)["id"].(float64))

	// 2. 90 分钟后再排 → 409 / 40903，且记录未增加
	var before int64
	db.Model(&model.Hearing{}).Where("case_id = ?", caseID).Count(&before)
	status, resp = doJSON(t, h, token, http.MethodPost, "/api/v1/hearings", map[string]any{
		"case_id": caseID, "hearing_time": future(3, 10, 30), "court": "深圳中院", "courtroom": "第1法庭",
	})
	if status != http.StatusConflict || int(resp["code"].(float64)) != constants.CodeHearingConflict {
		t.Fatalf("conflict expect 409/40903, got %d/%v", status, resp["code"])
	}
	var after int64
	db.Model(&model.Hearing{}).Where("case_id = ?", caseID).Count(&after)
	if after != before {
		t.Fatalf("rejected request must not insert rows: before=%d after=%d", before, after)
	}

	// 3. 改期成功 → 旧场次 cancelled + 新场次 scheduled
	status, resp = doJSON(t, h, token, http.MethodPost, fmt.Sprintf("/api/v1/hearings/%d/reschedule", firstID), map[string]any{
		"hearing_time": future(4, 14, 0), "court": "深圳中院", "courtroom": "第2法庭",
	})
	if status != http.StatusOK {
		t.Fatalf("reschedule status=%d resp=%v", status, resp)
	}
	newID := uint64(resp["data"].(map[string]any)["id"].(float64))
	if newID == firstID {
		t.Fatal("reschedule must create a new hearing id")
	}

	// 4. 再次改期同一个旧场次 → 409（幂等防重）
	status, resp = doJSON(t, h, token, http.MethodPost, fmt.Sprintf("/api/v1/hearings/%d/reschedule", firstID), map[string]any{
		"hearing_time": future(5, 9, 0), "court": "深圳中院", "courtroom": "第3法庭",
	})
	if status != http.StatusConflict {
		t.Fatalf("double reschedule of old hearing expect 409, got %d resp=%v", status, resp)
	}

	// 5. 待办列表仅含新场次
	var lawyerID uint64
	db.Model(&model.Case{}).Where("id = ?", caseID).Pluck("lead_lawyer_id", &lawyerID)
	status, resp = doJSON(t, h, token, http.MethodGet,
		fmt.Sprintf("/api/v1/hearings/upcoming?lead_lawyer_id=%d", lawyerID), nil)
	if status != http.StatusOK {
		t.Fatalf("upcoming status=%d", status)
	}
	list := resp["data"].([]any)
	if len(list) != 1 || uint64(list[0].(map[string]any)["id"].(float64)) != newID {
		t.Fatalf("upcoming must contain only the new hearing %d, got %v", newID, list)
	}

	// 6. 缺少必填字段 → 400
	status, _ = doJSON(t, h, token, http.MethodPost, "/api/v1/hearings", map[string]any{
		"case_id": caseID, "hearing_time": future(6, 9, 0), "court": "深圳中院",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("missing courtroom expect 400, got %d", status)
	}

	// 7. 未携带 JWT → 401
	status, _ = doJSON(t, h, "", http.MethodGet, "/api/v1/hearings", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("no token expect 401, got %d", status)
	}
}
