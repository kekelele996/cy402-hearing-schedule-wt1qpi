import request from '@/utils/request'

export interface HearingSchedulePayload {
  case_id: number
  lead_lawyer_id?: number
  hearing_time: string // RFC3339
  court: string
  courtroom: string
}

export function listHearings(params: {
  page?: number
  page_size?: number
  case_id?: number
  lead_lawyer_id?: number
  status?: string
  from?: string
  to?: string
  include_void?: boolean
}) {
  return request.get('/hearings', { params })
}

export function getHearing(id: number) {
  return request.get(`/hearings/${id}`)
}

export function scheduleHearing(data: HearingSchedulePayload) {
  return request.post('/hearings', data)
}

export function rescheduleHearing(id: number, data: { hearing_time: string; court?: string; courtroom?: string }) {
  return request.post(`/hearings/${id}/reschedule`, data)
}

export function cancelHearing(id: number, reason?: string) {
  return request.post(`/hearings/${id}/cancel`, { reason })
}

export function listUpcomingHearings(params?: { lead_lawyer_id?: number; limit?: number }) {
  return request.get('/hearings/upcoming', { params })
}

export function listHearingHistoryByCase(caseId: number) {
  return request.get(`/hearings/by-case/${caseId}/history`)
}
