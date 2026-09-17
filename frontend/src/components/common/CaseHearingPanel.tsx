import { useEffect, useState } from 'react'
import { Button, DatePicker, Empty, Form, Input, Modal, Space, Table, Tag } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import type { Dayjs } from 'dayjs'
import { useHearingStore } from '@/stores/hearingStore'
import { HearingStatusText, HearingStatusColor } from '@/constants/hearing'
import { formatDateTime } from '@/utils/dateFormat'
import type { Hearing } from '@/types'

interface Props {
  caseId: number
  leadLawyerId: number
  caseClosed: boolean
  onChanged?: () => void
}

// CaseHearingPanel 案件详情内的庭审面板：展示该案件完整改期链（含作废场次），
// 已结案/归档案件禁用新增按钮，避免提交未来庭审被后端拒绝。
export default function CaseHearingPanel({ caseId, leadLawyerId, caseClosed, onChanged }: Props) {
  const store = useHearingStore()
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm<{ hearing_time: Dayjs; court: string; courtroom: string }>()

  useEffect(() => {
    store.fetchHistory(caseId)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [caseId])

  async function onSchedule() {
    const v = await form.validateFields()
    await store.schedule({
      case_id: caseId,
      lead_lawyer_id: leadLawyerId || undefined,
      hearing_time: v.hearing_time.toISOString(),
      court: v.court,
      courtroom: v.courtroom,
    })
    setOpen(false)
    form.resetFields()
    store.fetchHistory(caseId)
    onChanged?.()
  }

  const columns = [
    { title: '编号', dataIndex: 'hearing_no' },
    { title: '开庭时间', dataIndex: 'hearing_time', render: (v: string) => formatDateTime(v) },
    { title: '法院', dataIndex: 'court' },
    { title: '法庭', dataIndex: 'courtroom' },
    {
      title: '状态',
      dataIndex: 'status',
      render: (v: string) => <Tag color={HearingStatusColor[v]}>{HearingStatusText[v] || v}</Tag>,
    },
    { title: '版本', render: (_: unknown, r: Hearing) => `第 ${r.seq} 版` },
  ]

  return (
    <div>
      <Space style={{ marginBottom: 12 }}>
        <Button
          type="primary"
          icon={<PlusOutlined />}
          disabled={caseClosed}
          title={caseClosed ? '已结案或归档案件不能新增未来庭审' : ''}
          onClick={() => setOpen(true)}
        >
          排期庭审
        </Button>
        {caseClosed && <span style={{ color: '#999' }}>案件已结案/归档，仅可查看历史场次</span>}
      </Space>
      {store.history.length === 0 ? (
        <Empty description="暂无庭审记录" />
      ) : (
        <Table<Hearing> rowKey="id" dataSource={store.history} pagination={false} size="small" columns={columns} />
      )}
      <Modal title="排期庭审" open={open} onOk={onSchedule} onCancel={() => setOpen(false)} destroyOnClose>
        <Form form={form} layout="vertical">
          <Form.Item name="hearing_time" label="开庭时间" rules={[{ required: true, message: '请选择开庭时间' }]}>
            <DatePicker showTime={{ format: 'HH:mm' }} format="YYYY-MM-DD HH:mm" style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="court" label="法院" rules={[{ required: true, message: '请输入法院' }, { max: 200 }]}>
            <Input />
          </Form.Item>
          <Form.Item name="courtroom" label="法庭" rules={[{ required: true, message: '请输入法庭' }, { max: 100 }]}>
            <Input />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
