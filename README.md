# LexCase（律师案件管理系统）

面向律师事务所的一体化管理平台，支持客户管理、案件全流程跟踪、**庭审排期**、文档归档和费用结算。

## 快速启动（Docker Compose 一键部署）

```bash
cp .env.example .env
docker compose up -d --build
```

启动完成后访问：

- 前端：http://localhost:28031
- 后端 API：http://localhost:29069
- 后端健康检查：http://localhost:29069/healthz
- PostgreSQL：localhost:38102

预置账号（密码见 database/init.sql 与 backend/internal/service/seed.go）：

| 用户名 | 密码 | 角色 |
| --- | --- | --- |
| admin | Admin@123 | 管理员 |
| lawyer | User@123 | 律师 |
| assistant | User@123 | 助理 |

## 本地开发

后端：

```bash
cd backend && go mod tidy && go run ./cmd/server
```

构建：`cd backend && go build ./...`

前端：

```bash
cd frontend && npm install && npm run dev
```

前端开发服务器通过 Vite 代理将 `/api` 转发到 `http://localhost:29069`。

## 技术栈

| 层 | 技术 |
| --- | --- |
| 前端 | React 18 + TypeScript + Ant Design + Vite + Zustand |
| 后端 | Go 1.22 + Gin + GORM |
| 数据库 | PostgreSQL 16 |
| 认证 | JWT（github.com/golang-jwt/jwt/v5）+ RBAC |
| 其他依赖 | gin-contrib/cors、golang.org/x/crypto/bcrypt、go-playground/validator/v10 |

## 项目目录结构

```
cy-402/
├── docker-compose.yml
├── .env.example
├── database/init.sql              # PostgreSQL 初始化脚本（建表 + 种子数据）
├── backend/
│   ├── cmd/server/main.go
│   └── internal/
│       ├── config/
│       ├── model/                 # user/client/case/document/billing/hearing/audit_log
│       ├── repository/            # 按实体分文件
│       ├── service/               # 业务逻辑 + 种子数据 + 单元/集成测试
│       ├── handler/               # HTTP 处理器（含 upload_handler、audit_log_handler）
│       ├── router/                # router.go + 按实体分文件（含 hearings.go 与 e2e 测试）
│       ├── middleware/            # auth/rbac/rate_limiter/error_handler/audit_log/cors/request_logger
│       ├── dto/
│       ├── constants/             # 枚举、错误码、日志模板、文案
│       └── util/                  # jwt/logger/formatters/amount_formatter/app_error/file_upload
└── frontend/
    └── src/
        ├── api/                   # auth/user/client/case/document/billing/hearing/auditLog/upload
        ├── stores/                # authStore/userStore/clientStore/caseStore/documentStore/billingStore/hearingStore
        ├── types/
        ├── components/common/     # CaseCard/DocumentList/StatusBadge/TimelineItem/AmountSummary/ClientCard/CaseTable/BillingCard/HearingCard/HearingFormModal/DocumentCard/FileUploader/FilterBar/AvatarUploader/PermissionGuard
        ├── hooks/                 # useAuth/usePagination/useFileUpload/usePermission
        ├── pages/                 # Cases/CaseDetail/Clients/Billing/Documents/Hearings/Profile/AuditLogs/Login
        ├── router/                # index.tsx + guards.tsx
        ├── utils/                 # dateFormat/amountFormatter/request
        └── constants/             # case/billing/hearing/document/errorCodes
```

## 环境变量

| 变量 | 说明 | 默认值 |
| --- | --- | --- |
| COMPOSE_PROJECT_NAME | Docker Compose 项目名/容器前缀 | cylawcase |
| DB_NAME | 数据库名 | cylawcase_db |
| DB_USER | 数据库用户 | cylawcase_user |
| DB_PASSWORD | 数据库密码 | cylawcase_pwd |
| DB_ROOT_PASSWORD | 预留 root 密码项 | cylawcase_root |
| JWT_SECRET | JWT 签名密钥 | change_me_to_a_long_random_string |
| JWT_EXPIRE_HOURS | JWT 过期小时数 | 72 |
| APP_CORS_ORIGINS | 允许的跨域来源（逗号分隔） | http://localhost:28031 |
| FRONTEND_PORT | 前端端口 | 28031 |
| BACKEND_PORT | 后端端口 | 29069 |
| DB_PORT | 数据库端口 | 38102 |

## Docker 部署说明

- 端口映射：前端 28031:80、后端 29069:8080、PostgreSQL 38102:5432。
- 数据卷：`db-data` 持久化 PostgreSQL 数据；`upload-data` 持久化上传文件。
- 服务依赖：backend `depends_on` db（service_healthy），frontend `depends_on` backend（service_healthy）。
- 前端 Nginx 将 `/api/` 反代到 `http://backend:8080/`，支持 SPA 路由 `try_files`。
- 常见问题：
  - 端口冲突：修改 `.env` 中 `FRONTEND_PORT/BACKEND_PORT/DB_PORT` 后重新 `docker compose up -d`。
  - 数据库重置：`docker compose down -v` 后重新启动。

## 枚举出现位置清单

### CaseStatus（filed/investigating/hearing/closed/archived）
- 后端：`backend/internal/constants/case.go`、`backend/internal/model/case.go`、`backend/internal/service/case_service.go`、`backend/internal/util/formatters.go`、`backend/internal/constants/log_templates.go`、`backend/internal/constants/error_codes.go`、`backend/internal/dto/dto_case.go`、`database/init.sql`
- 前端：`frontend/src/constants/case.ts`、`frontend/src/components/common/StatusBadge.tsx`、`frontend/src/pages/Cases.tsx`、`frontend/src/pages/CaseDetail.tsx`、`frontend/src/components/common/CaseCard.tsx`、`frontend/src/components/common/CaseTable.tsx`

### BillingType（attorney_fee/court_fee/travel_fee/other）
- 后端：`backend/internal/constants/billing.go`、`backend/internal/model/billing.go`、`backend/internal/service/billing_service.go`、`backend/internal/util/formatters.go`、`backend/internal/constants/log_templates.go`、`backend/internal/constants/error_codes.go`、`backend/internal/dto/dto_billing.go`、`database/init.sql`
- 前端：`frontend/src/constants/billing.ts`、`frontend/src/components/common/BillingCard.tsx`、`frontend/src/pages/Billing.tsx`

### BillingStatus（pending/paid/invoiced/void）
- 后端：`backend/internal/constants/billing.go`、`backend/internal/model/billing.go`、`backend/internal/service/billing_service.go`、`backend/internal/util/formatters.go`、`backend/internal/constants/log_templates.go`、`backend/internal/constants/error_codes.go`、`database/init.sql`
- 前端：`frontend/src/constants/billing.ts`、`frontend/src/components/common/StatusBadge.tsx`、`frontend/src/components/common/AmountSummary.tsx`、`frontend/src/components/common/BillingCard.tsx`、`frontend/src/pages/Billing.tsx`

### HearingStatus（scheduled/cancelled）
- 后端：`backend/internal/constants/hearing.go`、`backend/internal/model/hearing.go`、`backend/internal/repository/hearing_repository.go`、`backend/internal/service/hearing_service.go`、`backend/internal/handler/hearing_handler.go`、`backend/internal/dto/dto_hearing.go`、`backend/internal/util/formatters.go`、`backend/internal/constants/log_templates.go`、`backend/internal/constants/error_codes.go`、`backend/internal/constants/messages.go`、`database/init.sql`
- 前端：`frontend/src/constants/hearing.ts`、`frontend/src/types/index.ts`、`frontend/src/api/hearing.ts`、`frontend/src/stores/hearingStore.ts`、`frontend/src/components/common/StatusBadge.tsx`、`frontend/src/components/common/HearingCard.tsx`、`frontend/src/components/common/HearingFormModal.tsx`、`frontend/src/pages/Hearings.tsx`、`frontend/src/pages/CaseDetail.tsx`

## 庭审排期模块：不可放宽的约束与并发保证

- **必备字段**：每场庭审归属案件与主办律师（主办律师由后端按案件 `lead_lawyer_id` 确定），
  并必须载明开庭时间、法院、法庭；开庭时间必须是未来时间（`HearingScheduleRequest` validator + service 双重校验）。
- **两小时间隔**：同一律师任意两场 `scheduled` 有效庭审时间差 **严格小于 2 小时** 时整次拒绝
  （HTTP 409 / 错误码 40903），事务回滚，原排期一条记录都不变；恰好 2 小时允许（开区间 `(t-2h, t+2h)`）。
- **改期即作废+新建**：改期绝不 UPDATE 旧场次时间，而是把旧场次置为 `cancelled`（带 `cancelled_at`）
  并插入一条 `scheduled` 新场次，新场次以 `rescheduled_from_id` 指向旧场次，旧记录留档审计。
- **并发双改期收敛**：同一旧场次同时收到两个改期请求时，旧场次行锁（`SELECT ... FOR UPDATE`）+
  状态/版本 CAS 条件更新 + `rescheduled_from_id` 唯一索引（PostgreSQL 唯一索引允许多个 NULL，
  根场次不受限）三重保证：恰好一个请求成功，库内最终只有 1 条后继 `scheduled` 场次、1 条 `cancelled` 旧场次。
- **并发排期防撞庭**：同一律师的排期/改期写入在事务内先取
  `pg_advisory_xact_lock(9102, lawyer_id)` 咨询锁，消除「先查后插」竞态，
  并发提交两场相隔 30 分钟的庭审也只会成功一场。
- **结案/归档冻结**：事务内对案件行加 `FOR UPDATE` 锁后再判状态，
  `closed`/`archived` 案件不能新增未来庭审（HTTP 409 / 错误码 40904），防止与结案操作并发穿透。
- **待办只读有效场次**：`GET /hearings/upcoming` 只返回当前时间之后的 `scheduled` 场次；
  `cancelled` 旧场次仅出现在全量列表/案件详情中并以置灰样式留档，不会被误读成待办。
- **测试**：`backend/internal/service/hearing_integration_test.go` 使用
  embedded-postgres（真实 PostgreSQL）验证全部约束及两类并发收敛场景；
  `backend/internal/router/hearing_e2e_test.go` 覆盖完整 HTTP/JWT 契约。运行：`cd backend && go test ./...`

## API 接口清单

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | /healthz | 服务健康检查 |
| GET | /api/healthz | Nginx 反代健康检查 |
| GET | /api/v1/healthz | API 版本健康检查 |
| POST | /api/v1/auth/register | 用户注册 |
| POST | /api/v1/auth/login | 用户登录，返回 JWT |
| GET | /api/v1/users/me | 当前登录用户信息 |
| PUT | /api/v1/users/me | 修改个人资料 |
| GET | /api/v1/users | 用户列表（仅管理员） |
| GET | /api/v1/clients | 客户分页列表 |
| POST | /api/v1/clients | 新建客户 |
| GET | /api/v1/clients/:id | 客户详情与历史案件 |
| PUT | /api/v1/clients/:id | 编辑客户 |
| DELETE | /api/v1/clients/:id | 删除客户 |
| GET | /api/v1/cases | 案件分页列表 |
| POST | /api/v1/cases | 创建案件 |
| GET | /api/v1/cases/:id | 案件详情 |
| PUT | /api/v1/cases/:id | 更新案件 |
| POST | /api/v1/cases/:id/status | 案件状态流转 |
| POST | /api/v1/cases/:id/assign | 分配主办律师 |
| GET | /api/v1/documents | 文档中心分页列表 |
| POST | /api/v1/documents | 上传文档记录 |
| GET | /api/v1/documents/by-case/:id | 按案件查询文档 |
| DELETE | /api/v1/documents/:id | 删除文档 |
| GET | /api/v1/billings | 账单分页列表 |
| POST | /api/v1/billings | 创建账单 |
| GET | /api/v1/billings/summary | 本月应收/已收/待收汇总 |
| GET | /api/v1/billings/by-case/:id | 按案件查询账单 |
| POST | /api/v1/billings/:id/paid | 标记支付 |
| POST | /api/v1/billings/:id/invoiced | 标记开票 |
| POST | /api/v1/billings/:id/void | 作废账单 |
| GET | /api/v1/hearings | 庭审场次分页列表（case_id/lead_lawyer_id/status/start_time/end_time） |
| POST | /api/v1/hearings | 新增庭审排期（撞庭 409/40903，结案 409/40904） |
| GET | /api/v1/hearings/upcoming?lead_lawyer_id= | 律师待办（仅未来的 scheduled 场次） |
| GET | /api/v1/hearings/:id | 场次详情 |
| POST | /api/v1/hearings/:id/reschedule | 改期：作废旧场次并生成新场次（并发落败/撞庭 409） |
| GET | /api/v1/audit-logs | 审计日志（仅管理员） |
| POST | /api/v1/upload/file | 文件上传 |

## 主要功能

- 客户管理：新建/编辑/检索客户，查看历史案件。
- 案件管理：创建案件、状态流转（立案→调查→庭审→结案→归档）、律师分配、筛选查询。
- 庭审排期：为案件安排未来庭审（时间/法院/法庭必填，主办律师取案件主办律师），
  同一律师两场有效庭审间隔不足两小时整次拒绝、原排期不变；改期只作废旧场次并生成新场次，
  并发双改期在数据库层收敛为一条有效记录；已结案/归档案件禁止新增未来庭审；
  律师待办视图只含 scheduled 有效场次，cancelled 旧场次不会被误读为待办。
- 文档归档：按案件上传/查看/删除文档（起诉状/答辩状/证据/判决书/合同等）。
- 费用结算：创建账单、标记支付、开票、作废，本月应收/已收/待收汇总。
- 审计日志：写操作自动记录（管理员查看）。
- 角色权限：JWT + RBAC（admin/lawyer/assistant）。

## License

MIT License
