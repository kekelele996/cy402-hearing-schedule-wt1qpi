// 庭审场次状态枚举（与后端 backend/internal/constants/hearing.go 保持一致）
export const HearingStatus = {
  SCHEDULED: 'scheduled',
  RESCHEDULED: 'rescheduled',
  CANCELED: 'canceled',
} as const

export const HearingStatusText: Record<string, string> = {
  [HearingStatus.SCHEDULED]: '待开庭',
  [HearingStatus.RESCHEDULED]: '已改期',
  [HearingStatus.CANCELED]: '已取消',
}

// 颜色与 StatusBadge/Tag 约定保持一致：有效场次高亮，作废场次弱化。
export const HearingStatusColor: Record<string, string> = {
  [HearingStatus.SCHEDULED]: 'processing',
  [HearingStatus.RESCHEDULED]: 'default',
  [HearingStatus.CANCELED]: 'error',
}

export const HearingStatusOptions = Object.entries(HearingStatusText).map(([value, label]) => ({ label, value }))

// 同一律师两场庭审的最小间隔（毫秒），仅用于前端提示，最终以后端两小时约束为准。
export const MIN_HEARING_GAP_MS = 2 * 60 * 60 * 1000
