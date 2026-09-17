import { create } from 'zustand'
import {
  listHearings,
  listUpcomingHearings,
  listHearingHistoryByCase,
  scheduleHearing,
  rescheduleHearing,
  cancelHearing,
  type HearingSchedulePayload,
} from '@/api/hearing'
import type { Hearing } from '@/types'

interface HearingState {
  list: Hearing[]
  total: number
  upcoming: Hearing[]
  history: Hearing[]
  fetchList: (params?: Record<string, unknown>) => Promise<void>
  fetchUpcoming: (lawyerId?: number) => Promise<void>
  fetchHistory: (caseId: number) => Promise<void>
  schedule: (payload: HearingSchedulePayload) => Promise<void>
  reschedule: (id: number, payload: { hearing_time: string; court?: string; courtroom?: string }) => Promise<void>
  cancel: (id: number, reason?: string) => Promise<void>
}

export const useHearingStore = create<HearingState>((set) => ({
  list: [],
  total: 0,
  upcoming: [],
  history: [],
  async fetchList(params = {}) {
    const res: any = await listHearings(params)
    set({ list: res.data.list, total: res.data.total })
  },
  async fetchUpcoming(lawyerId) {
    const res: any = await listUpcomingHearings(lawyerId ? { lead_lawyer_id: lawyerId } : {})
    set({ upcoming: res.data })
  },
  async fetchHistory(caseId) {
    const res: any = await listHearingHistoryByCase(caseId)
    set({ history: res.data })
  },
  async schedule(payload) {
    await scheduleHearing(payload)
  },
  async reschedule(id, payload) {
    await rescheduleHearing(id, payload)
  },
  async cancel(id, reason) {
    await cancelHearing(id, reason)
  },
}))
