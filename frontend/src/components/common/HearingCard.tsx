import { Card, Descriptions, Button, Space, Popconfirm, Tooltip } from 'antd'
import { ClockCircleOutlined } from '@ant-design/icons'
import StatusBadge from './StatusBadge'
import { HearingStatus } from '@/constants/hearing'
import { formatDateTime } from '@/utils/dateFormat'
import type { Hearing } from '@/types'

interface HearingCardProps {
  item: Hearing
  caseTitle?: string
  lawyerName?: string
  onReschedule?: (h: Hearing) => void
}

// HearingCard 庭审场次卡片，被 /hearings 与 /cases/:id 共用。
// 已作废场次以禁用样式展示，且不提供「改期」入口——旧记录只读留档。
export default function HearingCard({ item, caseTitle, lawyerName, onReschedule }: HearingCardProps) {
  const cancelled = item.status === HearingStatus.CANCELLED
  return (
    <Card
      size="small"
      style={{ opacity: cancelled ? 0.55 : 1 }}
      title={
        <Space>
          <ClockCircleOutlined />
          <span>{item.hearing_no}</span>
          {caseTitle ? <span style={{ fontWeight: 400 }}>{caseTitle}</span> : null}
        </Space>
      }
      extra={<StatusBadge status={item.status} kind="hearing" />}
    >
      <Descriptions column={1} size="small">
        <Descriptions.Item label="开庭时间">{formatDateTime(item.hearing_time)}</Descriptions.Item>
        <Descriptions.Item label="法院">{item.court}</Descriptions.Item>
        <Descriptions.Item label="法庭">{item.courtroom}</Descriptions.Item>
        <Descriptions.Item label="主办律师">{lawyerName || `#${item.lead_lawyer_id}`}</Descriptions.Item>
        {cancelled && item.cancelled_at && (
          <Descriptions.Item label="作废时间">{formatDateTime(item.cancelled_at)}</Descriptions.Item>
        )}
      </Descriptions>
      {!cancelled && onReschedule && (
        <Space style={{ marginTop: 8 }}>
          <Popconfirm
            title="改期将作废当前场次并生成新场次"
            description="原场次记录会保留为「已作废」，仅新场次进入待办。"
            onConfirm={() => onReschedule(item)}
            okText="继续改期"
            cancelText="取消"
          >
            <Tooltip title="作废旧场次并生成新场次">
              <Button size="small">改期</Button>
            </Tooltip>
          </Popconfirm>
        </Space>
      )}
    </Card>
  )
}
