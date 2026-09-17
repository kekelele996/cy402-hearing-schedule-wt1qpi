import { Tag } from 'antd'
import { CaseStatusText } from '@/constants/case'
import { BillingStatusText } from '@/constants/billing'
import { HearingStatusText } from '@/constants/hearing'

export type StatusKind = 'case' | 'billing' | 'hearing'

const caseColor: Record<string, string> = {
  filed: 'blue',
  investigating: 'orange',
  hearing: 'purple',
  closed: 'green',
  archived: 'default',
}

const billingColor: Record<string, string> = {
  pending: 'orange',
  paid: 'green',
  invoiced: 'blue',
  void: 'default',
}

const hearingColor: Record<string, string> = {
  scheduled: 'purple',
  cancelled: 'default',
}

const textMap: Record<StatusKind, Record<string, string>> = {
  case: CaseStatusText,
  billing: BillingStatusText,
  hearing: HearingStatusText,
}

const colorMap: Record<StatusKind, Record<string, string>> = {
  case: caseColor,
  billing: billingColor,
  hearing: hearingColor,
}

export default function StatusBadge({ status, kind = 'case' }: { status: string; kind?: StatusKind }) {
  const text = textMap[kind][status] || status
  const color = colorMap[kind][status]
  return <Tag color={color}>{text}</Tag>
}
