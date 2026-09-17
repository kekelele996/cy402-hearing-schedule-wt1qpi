import { useEffect, useState } from 'react'
import { Card, Table, Select, DatePicker, Button, Space, Tabs, Tag, message } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import { useHearingStore } from '@/stores/hearingStore'
import { useUserStore } from '@/stores/userStore'
import { useCaseStore } from '@/stores/caseStore'
import HearingCard from '@/components/common/HearingCard'
import HearingFormModal from '@/components/common/HearingFormModal'
import { HearingStatus, HearingStatusOptions } from '@/constants/hearing'
import { formatDateTime } from '@/utils/dateFormat'
import type { Hearing } from '@/types'

const { RangePicker } = DatePicker

export default function Hearings() {
  const store = useHearingStore()
  const userStore = useUserStore()
  const caseStore = useCaseStore()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [lawyerFilter, setLawyerFilter] = useState<number>()
  const [statusFilter, setStatusFilter] = useState<string>()
  const [range, setRange] = useState<[dayjs.Dayjs, dayjs.Dayjs] | null>(null)
  const [modalOpen, setModalOpen] = useState(false)
  const [target, setTarget] = useState<Hearing | null>(null)

  useEffect(() => {
    userStore.fetchLawyers()
    caseStore.fetchList({ page: 1, page_size: 200 })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function buildParams() {
    const params: Record<string, unknown> = { page, page_size: pageSize }
    if (lawyerFilter) params.lead_lawyer_id = lawyerFilter
    if (statusFilter) params.status = statusFilter
    if (range) {
      params.start_time = range[0].format('YYYY-MM-DD')
      params.end_time = range[1].format('YYYY-MM-DD')
    }
    return params
  }

  function refresh() {
    store.fetchList(buildParams())
  }

  useEffect(() => {
    refresh()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, pageSize, lawyerFilter, statusFilter, range])

  function openCreate() {
    setTarget(null)
    setModalOpen(true)
  }

  function openReschedule(h: Hearing) {
    setTarget(h)
    setModalOpen(true)
  }

  const lawyerName = (id: number) => userStore.lawyers.find((l) => l.id === id)?.real_name || `#${id}`
  const caseTitle = (id: number) => {
    const c = caseStore.list.find((x) => x.id === id)
    return c ? `${c.case_no} ${c.title}` : `案件 #${id}`
  }

  const columns = [
    { title: '场次号', dataIndex: 'hearing_no' },
    { title: '案件', dataIndex: 'case_id', render: (v: number) => caseTitle(v) },
    { title: '开庭时间', dataIndex: 'hearing_time', render: (v: string) => formatDateTime(v) },
    { title: '法院', dataIndex: 'court' },
    { title: '法庭', dataIndex: 'courtroom' },
    { title: '主办律师', dataIndex: 'lead_lawyer_id', render: (v: number) => lawyerName(v) },
    {
      title: '状态',
      dataIndex: 'status',
      render: (v: string) => <Tag color={v === HearingStatus.SCHEDULED ? 'purple' : 'default'}>
        {v === HearingStatus.SCHEDULED ? '待开庭' : '已作废（改期）'}
      </Tag>,
    },
    {
      title: '操作',
      render: (_: unknown, row: Hearing) =>
        row.status === HearingStatus.SCHEDULED ? (
          <Button size="small" onClick={() => openReschedule(row)}>改期</Button>
        ) : (
          <span style={{ color: '#999' }}>已留档</span>
        ),
    },
  ]

  return (
    <Card
      title="庭审排期"
      extra={
        <Space>
          <Select
            allowClear
            placeholder="主办律师"
            style={{ width: 150 }}
            value={lawyerFilter}
            onChange={setLawyerFilter}
            options={userStore.lawyers.map((l) => ({ label: l.real_name || l.username, value: l.id }))}
          />
          <Select
            allowClear
            placeholder="状态"
            style={{ width: 140 }}
            value={statusFilter}
            onChange={setStatusFilter}
            options={HearingStatusOptions}
          />
          <RangePicker value={range} onChange={(v) => setRange(v as [dayjs.Dayjs, dayjs.Dayjs] | null)} />
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>排期</Button>
        </Space>
      }
    >
      <Tabs
        items={[
          {
            key: 'all',
            label: '全部场次（含改期留档）',
            children: (
              <Table<Hearing>
                rowKey="id"
                dataSource={store.list}
                columns={columns}
                pagination={{
                  current: page,
                  pageSize,
                  total: store.total,
                  showTotal: (t) => `共 ${t} 条`,
                  onChange: (p, ps) => { setPage(p); setPageSize(ps) },
                }}
              />
            ),
          },
          {
            key: 'upcoming',
            label: '我的待办（仅有效场次）',
            children: <UpcomingPanel onReschedule={openReschedule} />,
          },
        ]}
      />
      <HearingFormModal
        open={modalOpen}
        target={target}
        caseOptions={caseStore.list}
        onClose={() => setModalOpen(false)}
        onSuccess={() => {
          refresh()
          message.loading({ content: '列表已刷新', duration: 0.8 })
        }}
      />
    </Card>
  )
}

// UpcomingPanel 律师待办：只消费 /hearings/upcoming，已作废旧场次永不出现。
function UpcomingPanel({ onReschedule }: { onReschedule: (h: Hearing) => void }) {
  const store = useHearingStore()
  const userStore = useUserStore()

  useEffect(() => {
    userStore.fetchMe().then((me) => store.fetchUpcoming(me.id))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  if (store.upcoming.length === 0) {
    return <span style={{ color: '#999' }}>暂无待开庭场次</span>
  }
  return (
    <Space direction="vertical" style={{ width: '100%' }} size={12}>
      {store.upcoming.map((h) => (
        <HearingCard key={h.id} item={h} lawyerName={userStore.me?.real_name} onReschedule={onReschedule} />
      ))}
    </Space>
  )
}
