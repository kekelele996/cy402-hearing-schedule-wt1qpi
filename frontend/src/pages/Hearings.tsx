import { useEffect, useState } from 'react'
import { Button, Card, DatePicker, Form, Input, InputNumber, message, Modal, Select, Space, Switch, Table, Tag } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import dayjs, { Dayjs } from 'dayjs'
import { useHearingStore } from '@/stores/hearingStore'
import { HearingStatusText, HearingStatusColor, HearingStatusOptions } from '@/constants/hearing'
import { formatDateTime } from '@/utils/dateFormat'
import type { Hearing } from '@/types'

interface FormValues {
  case_id: number
  lead_lawyer_id?: number
  hearing_time: Dayjs
  court: string
  courtroom: string
}

export default function Hearings() {
  const store = useHearingStore()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [status, setStatus] = useState<string | undefined>(undefined)
  const [includeVoid, setIncludeVoid] = useState(false)
  const [open, setOpen] = useState(false)
  const [rescheduleTarget, setRescheduleTarget] = useState<Hearing | null>(null)
  const [form] = Form.useForm<FormValues>()
  const [rForm] = Form.useForm<{ hearing_time: Dayjs; court?: string; courtroom?: string }>()

  function reload(p = page) {
    const params: Record<string, unknown> = { page: p, page_size: pageSize, include_void: includeVoid || undefined }
    if (status) params.status = status
    store.fetchList(params)
  }

  useEffect(() => {
    reload(1)
    setPage(1)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status, includeVoid])

  useEffect(() => {
    reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, pageSize])

  async function onSchedule() {
    const v = await form.validateFields()
    await store.schedule({
      case_id: v.case_id,
      lead_lawyer_id: v.lead_lawyer_id,
      hearing_time: v.hearing_time.toISOString(),
      court: v.court,
      courtroom: v.courtroom,
    })
    message.success('庭审已排期')
    setOpen(false)
    form.resetFields()
    setPage(1)
    reload(1)
  }

  async function onReschedule() {
    if (!rescheduleTarget) return
    const v = await rForm.validateFields()
    await store.reschedule(rescheduleTarget.id, {
      hearing_time: v.hearing_time.toISOString(),
      court: v.court,
      courtroom: v.courtroom,
    })
    message.success('庭审已改期，旧场次已作废')
    setRescheduleTarget(null)
    rForm.resetFields()
    reload()
  }

  async function onCancel(row: Hearing) {
    Modal.confirm({
      title: `取消庭审 ${row.hearing_no}？`,
      content: '取消后旧场次作废，不会再出现在待办列表中。',
      okText: '确认取消',
      okButtonProps: { danger: true },
      cancelText: '返回',
      onOk: async () => {
        await store.cancel(row.id)
        message.success('庭审已取消')
        reload()
      },
    })
  }

  const columns = [
    { title: '庭审编号', dataIndex: 'hearing_no' },
    { title: '开庭时间', dataIndex: 'hearing_time', render: (v: string) => formatDateTime(v) },
    { title: '法院', dataIndex: 'court' },
    { title: '法庭', dataIndex: 'courtroom' },
    { title: '案件ID', dataIndex: 'case_id' },
    { title: '主办律师ID', dataIndex: 'lead_lawyer_id' },
    {
      title: '状态',
      dataIndex: 'status',
      render: (v: string) => <Tag color={HearingStatusColor[v]}>{HearingStatusText[v] || v}</Tag>,
    },
    {
      title: '改期链',
      render: (_: unknown, row: Hearing) => (
        <span style={{ color: '#999' }}>
          #{row.root_id} / 第 {row.seq} 版
        </span>
      ),
    },
    {
      title: '操作',
      render: (_: unknown, row: Hearing) =>
        row.status === 'scheduled' ? (
          <Space>
            <Button
              size="small"
              type="primary"
              onClick={() => {
                setRescheduleTarget(row)
                rForm.setFieldsValue({
                  hearing_time: dayjs(row.hearing_time),
                  court: row.court,
                  courtroom: row.courtroom,
                })
              }}
            >
              改期
            </Button>
            <Button size="small" danger onClick={() => onCancel(row)}>
              取消
            </Button>
          </Space>
        ) : (
          <span style={{ color: '#bbb' }}>已作废</span>
        ),
    },
  ]

  return (
    <Card
      title="庭审排期"
      extra={
        <Space>
          <Select
            placeholder="场次状态"
            allowClear
            style={{ width: 140 }}
            options={HearingStatusOptions}
            value={status}
            onChange={setStatus}
          />
          <Space size={4}>
            <span style={{ color: '#666' }}>含作废场次</span>
            <Switch checked={includeVoid} onChange={setIncludeVoid} />
          </Space>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
            新建排期
          </Button>
        </Space>
      }
    >
      <Table<Hearing>
        rowKey="id"
        dataSource={store.list}
        pagination={{
          current: page,
          pageSize,
          total: store.total,
          onChange: (p, ps) => {
            setPage(p)
            setPageSize(ps)
          },
        }}
        columns={columns}
      />

      <Modal title="新建庭审排期" open={open} onOk={onSchedule} onCancel={() => setOpen(false)} destroyOnClose>
        <Form form={form} layout="vertical">
          <Form.Item name="case_id" label="案件ID" rules={[{ required: true, message: '请输入案件ID' }]}>
            <InputNumber style={{ width: '100%' }} min={1} />
          </Form.Item>
          <Form.Item name="lead_lawyer_id" label="主办律师ID（留空取案件主办律师）">
            <InputNumber style={{ width: '100%' }} min={1} />
          </Form.Item>
          <Form.Item name="hearing_time" label="开庭时间" rules={[{ required: true, message: '请选择开庭时间' }]}>
            <DatePicker showTime={{ format: 'HH:mm' }} format="YYYY-MM-DD HH:mm" style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="court" label="法院" rules={[{ required: true, message: '请输入法院' }, { max: 200 }]}>
            <Input placeholder="如：深圳市南山区人民法院" />
          </Form.Item>
          <Form.Item name="courtroom" label="法庭" rules={[{ required: true, message: '请输入法庭' }, { max: 100 }]}>
            <Input placeholder="如：第 8 法庭" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={`改期：${rescheduleTarget?.hearing_no || ''}`}
        open={!!rescheduleTarget}
        onOk={onReschedule}
        onCancel={() => setRescheduleTarget(null)}
        destroyOnClose
        okText="确认改期"
      >
        <Form form={rForm} layout="vertical">
          <Form.Item name="hearing_time" label="新开庭时间" rules={[{ required: true, message: '请选择新的开庭时间' }]}>
            <DatePicker showTime={{ format: 'HH:mm' }} format="YYYY-MM-DD HH:mm" style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="court" label="新法院（留空沿用原值）" rules={[{ max: 200 }]}>
            <Input />
          </Form.Item>
          <Form.Item name="courtroom" label="新法庭（留空沿用原值）" rules={[{ max: 100 }]}>
            <Input />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
