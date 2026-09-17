import request from '@/utils/request'
import type { Hearing } from '@/types'

export interface HearingPayload {
  case_id?: number
  hearing_time: string
  court: string
  courtroom: string
}

export function listHearings(params: Record<string, unknown>) {
  return request.get('/hearings', { params })
}

export function listUpcomingHearings(leadLawyerId: number) {
  return request.get('/hearings/upcoming', { params: { lead_lawyer_id: leadLawyerId } })
}

export function getHearing(id: number) {
  return request.get(`/hearings/${id}`)
}

// scheduleHearing 新增庭审排期。
export function scheduleHearing(data: HearingPayload) {
  return request.post('/hearings', data)
}

// rescheduleHearing 改期：后端作废旧场次并生成新场次，前端不直接改旧记录。
export function rescheduleHearing(id: number, data: Omit<HearingPayload, 'case_id'>) {
  return request.post(`/hearings/${id}/reschedule`, data)
}
