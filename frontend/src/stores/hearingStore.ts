import { create } from 'zustand'
import { listHearings, listUpcomingHearings } from '@/api/hearing'
import type { Hearing } from '@/types'

interface HearingState {
  list: Hearing[]
  total: number
  upcoming: Hearing[]
  fetchList: (params?: Record<string, unknown>) => Promise<void>
  fetchUpcoming: (lawyerId: number) => Promise<void>
}

// 待办（upcoming）与历史/作废场次严格分离：
// upcoming 只来自 /hearings/upcoming（后端仅返回 scheduled 且未来），
// 避免从 cancelled 旧记录误读出待办。
export const useHearingStore = create<HearingState>((set) => ({
  list: [],
  total: 0,
  upcoming: [],
  async fetchList(params = {}) {
    const res: any = await listHearings(params)
    set({ list: res.data.list, total: res.data.total })
  },
  async fetchUpcoming(lawyerId) {
    const res: any = await listUpcomingHearings(lawyerId)
    set({ upcoming: res.data })
  },
}))
