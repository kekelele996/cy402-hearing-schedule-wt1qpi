import { useEffect } from 'react'
import { Modal, Form, Input, DatePicker, Select, message } from 'antd'
import dayjs from 'dayjs'
import { scheduleHearing, rescheduleHearing } from '@/api/hearing'
import { MIN_HEARING_INTERVAL_HOURS, CASE_BLOCKED_FOR_HEARING } from '@/constants/hearing'
import type { CaseItem, Hearing } from '@/types'

export interface HearingFormValues {
  case_id?: number
  hearing_time: dayjs.Dayjs
  court: string
  courtroom: string
}

interface HearingFormModalProps {
  open: boolean
  // target 为空 = 为案件新增排期；target 为场次 = 对该场次改期
  target?: Hearing | null
  caseOptions: CaseItem[]
  defaultCaseId?: number
  onClose: () => void
  onSuccess: () => void
}

// HearingFormModal 新增排期与改期共用表单。
// 改期提交到 /hearings/:id/reschedule，由后端作废旧场次并生成新场次，
// 前端提交成功后整体刷新列表，绝不在本地改写旧场次。
export default function HearingFormModal({ open, target, caseOptions, defaultCaseId, onClose, onSuccess }: HearingFormModalProps) {
  const [form] = Form.useForm<HearingFormValues>()
  const isReschedule = Boolean(target)

  useEffect(() => {
    if (open) {
      form.resetFields()
      if (target) {
        form.setFieldsValue({
          hearing_time: dayjs(target.hearing_time),
          court: target.court,
          courtroom: target.courtroom,
        })
      } else if (defaultCaseId) {
        form.setFieldsValue({ case_id: defaultCaseId })
      }
    }
  }, [open, target, defaultCaseId, form])

  async function onOk() {
    const values = await form.validateFields()
    const payload = {
      hearing_time: values.hearing_time.format('YYYY-MM-DD HH:mm'),
      court: values.court.trim(),
      courtroom: values.courtroom.trim(),
    }
    if (isReschedule && target) {
      await rescheduleHearing(target.id, payload)
      message.success('庭审已改期，旧场次已作废')
    } else {
      if (!values.case_id) {
        message.error('请选择案件')
        return
      }
      await scheduleHearing({ case_id: values.case_id, ...payload })
      message.success('庭审已排期')
    }
    onSuccess()
    onClose()
  }

  return (
    <Modal
      title={isReschedule ? `改期：${target?.hearing_no || ''}` : '新增庭审排期'}
      open={open}
      onOk={onOk}
      onCancel={onClose}
      destroyOnClose
    >
      <Form form={form} layout="vertical">
        {!isReschedule && (
          <Form.Item
            name="case_id"
            label="归属案件"
            tooltip="主办律师取案件的主办律师；已结案/归档案件不可选"
            rules={[{ required: true, message: '请选择案件' }]}
          >
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="选择要排期的案件"
              options={caseOptions.map((c) => ({
                label: `${c.case_no} ${c.title}`,
                value: c.id,
                disabled: CASE_BLOCKED_FOR_HEARING.includes(c.status),
              }))}
            />
          </Form.Item>
        )}
        <Form.Item
          name="hearing_time"
          label="开庭时间"
          tooltip={`同一律师两场庭审间隔不足 ${MIN_HEARING_INTERVAL_HOURS} 小时将被整次拒绝`}
          rules={[
            { required: true, message: '请选择开庭时间' },
            {
              validator: (_, v: dayjs.Dayjs) =>
                v && v.isAfter(dayjs()) ? Promise.resolve() : Promise.reject(new Error('开庭时间必须是未来时间')),
            },
          ]}
        >
          <DatePicker showTime={{ format: 'HH:mm' }} format="YYYY-MM-DD HH:mm" style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item name="court" label="法院" rules={[{ required: true, message: '请填写法院' }, { max: 200 }]}>
          <Input placeholder="如：深圳市中级人民法院" />
        </Form.Item>
        <Form.Item name="courtroom" label="法庭" rules={[{ required: true, message: '请填写法庭' }, { max: 100 }]}>
          <Input placeholder="如：第17法庭" />
        </Form.Item>
      </Form>
    </Modal>
  )
}
