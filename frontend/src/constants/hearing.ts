// 庭审场次状态枚举（与后端 backend/internal/constants/hearing.go 保持一致）
export const HearingStatus = {
  SCHEDULED: 'scheduled',
  CANCELLED: 'cancelled',
} as const

export const HearingStatusText: Record<string, string> = {
  [HearingStatus.SCHEDULED]: '待开庭',
  [HearingStatus.CANCELLED]: '已作废（改期）',
}

export const HearingStatusOptions = Object.entries(HearingStatusText).map(([value, label]) => ({ label, value }))

// 同一律师两场庭审最小间隔（小时），与后端 MinHearingInterval 一致
export const MIN_HEARING_INTERVAL_HOURS = 2

// 不允许新增未来庭审的案件状态（与后端 isClosedOrArchived 一致）
export const CASE_BLOCKED_FOR_HEARING: string[] = ['closed', 'archived']
