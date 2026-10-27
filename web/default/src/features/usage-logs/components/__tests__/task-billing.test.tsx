/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { useSystemConfigStore } from '@/stores/system-config-store'

import type { TaskLog } from '../../types'
import { useTaskLogsColumns } from '../columns/task-logs-columns'
import { TaskBillingDetails } from '../task-billing'

vi.mock('@/features/pricing/hooks/use-pricing-data', () => ({
  usePricingData: () => ({
    models: [
      {
        model_name: 'sora-2',
        billing_expr: 'tier("current", u("seconds") * 99)',
        billing_usage_schema: {
          seconds: {
            type: 'number',
            unit: 'second',
            description: 'Video duration',
          },
        },
      },
    ],
  }),
}))

const task: TaskLog = {
  id: 1,
  user_id: 7,
  platform: 'sora',
  task_id: 'task-video',
  action: 'text_to_video',
  channel_id: 60,
  group: 'default',
  quota: 320000,
  submit_time: 1,
  status: 'SUCCESS',
  properties: {
    origin_model_name: 'sora-2',
    upstream_model_name: 'sora-2-upstream',
  },
}

function UserTaskTable(props: { log: TaskLog }) {
  const columns = useTaskLogsColumns(false, false)
  // oxlint-disable-next-line react/incompatible-library -- Exercise TanStack Table directly in the test fixture.
  const table = useReactTable({
    data: [props.log],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <table>
      <tbody>
        {table.getRowModel().rows.map((row) => (
          <tr key={row.id}>
            {row
              .getVisibleCells()
              .filter((cell) => ['model', 'quota'].includes(cell.column.id))
              .map((cell) => (
                <td key={cell.id}>
                  {flexRender(cell.column.columnDef.cell, cell.getContext())}
                </td>
              ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}

beforeEach(() => {
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
})

afterEach(() => {
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  localStorage.clear()
})

test('lets ordinary users see the requested model and open the recorded video billing breakdown', async () => {
  render(
    <UserTaskTable
      log={{
        ...task,
        quota: 1600000,
        billing: {
          amount_available: true,
          state: 'completed',
          pricing_epoch: 'usd_credit_v1',
          model_price: 0.1,
          billing_unit: 'second',
          group_ratio: 0.8,
          seconds: 8,
          size: 1.666667,
          duration: 2,
          resolution: 1.5,
        },
      }}
    />
  )

  expect(screen.getByText('sora-2')).toBeVisible()
  expect(screen.queryByText('sora-2-upstream')).not.toBeInTheDocument()
  expect(screen.getByText('Text to Video')).toBeVisible()
  expect(screen.queryByText('Unknown')).not.toBeInTheDocument()
  expect(screen.getByText('$3.2')).toBeVisible()

  await userEvent.click(screen.getByRole('button', { name: 'Billing Details' }))

  const dialog = within(screen.getByRole('dialog', { name: 'Billing Details' }))
  expect(dialog.getByText('Billing processed')).toBeVisible()
  expect(dialog.queryByText('Settled')).not.toBeInTheDocument()
  expect(dialog.getByText('Per Second')).toBeVisible()
  expect(dialog.getByText('$0.1/second')).toBeVisible()
  expect(dialog.getByText('Group Ratio')).toBeVisible()
  expect(dialog.getByText('0.8000x')).toBeVisible()
  expect(dialog.getByText('8 second')).toBeVisible()
  expect(dialog.getByText('Size multiplier')).toBeVisible()
  expect(dialog.getByText('1.666667×')).toBeVisible()
  expect(dialog.getByText('Duration multiplier')).toBeVisible()
  expect(dialog.getByText('2×')).toBeVisible()
  expect(dialog.getByText('Resolution multiplier')).toBeVisible()
  expect(dialog.getByText('1.5×')).toBeVisible()
  expect(dialog.getAllByText('Current charge')).toHaveLength(2)
  expect(dialog.getAllByText('$3.2')).toHaveLength(2)
})

test('shows the saved per-second expression price and usage even when current pricing has changed', () => {
  render(
    <TaskBillingDetails
      log={{
        ...task,
        billing: {
          amount_available: true,
          state: 'completed',
          pricing_epoch: 'usd_credit_v1',
          billing_mode: 'tiered_expr',
          expr_b64: btoa('tier("recorded", u("seconds") * 0.08)'),
          matched_tier: 'recorded',
          usage_facts: { seconds: 8 },
        },
      }}
    />
  )

  expect(screen.getAllByText('$0.08/s').length).toBeGreaterThan(0)
  expect(screen.getAllByText('Video duration').length).toBeGreaterThan(0)
  expect(screen.queryByText('Matched')).not.toBeInTheDocument()
  expect(screen.getByText('Estimated tier')).toBeVisible()
  expect(screen.getByText('Usage parameters')).toBeVisible()
  expect(screen.getByText('seconds').nextElementSibling).toHaveTextContent('8')
  expect(screen.getAllByText('$0.64')).toHaveLength(2)
  expect(screen.queryByText('$99/s')).not.toBeInTheDocument()
})

test('falls back to the upstream model when the requested model was not recorded', () => {
  render(
    <UserTaskTable
      log={{
        ...task,
        properties: { upstream_model_name: 'sora-2-upstream' },
      }}
    />
  )

  expect(screen.getByText('sora-2-upstream')).toBeVisible()
  expect(screen.getByText('Text to Video')).toBeVisible()
})

test.each([
  { status: 'FAILURE', state: 'reserved', expected: 'Pre-consumed' },
  { status: 'SUCCESS', state: 'accepted', expected: 'Awaiting settlement' },
  { status: 'FAILURE', state: 'refunded', expected: 'Refunded' },
])(
  'uses persisted billing state $state when generation status is $status',
  ({ status, state, expected }) => {
    render(<TaskBillingDetails log={{ ...task, status, billing: { state } }} />)

    expect(screen.getByText(expected)).toBeVisible()
    if (state !== 'refunded') {
      expect(screen.queryByText('Refunded')).not.toBeInTheDocument()
    }
  }
)

test.each([
  {
    name: 'legacy pricing',
    billing: {
      amount_available: true,
      pricing_epoch: 'legacy_075',
      model_price: 0.1,
      seconds: 8,
    },
  },
  { name: 'missing pricing snapshot', billing: { amount_available: true } },
])(
  'keeps the recorded cost without inventing prices or refunds for $name',
  ({ billing }) => {
    render(<TaskBillingDetails log={{ ...task, status: 'FAILURE', billing }} />)

    expect(screen.getByText('$0.64')).toBeVisible()
    expect(
      screen.getByText('Billing details unavailable for this record')
    ).toBeVisible()
    expect(screen.queryByText('Model Price')).not.toBeInTheDocument()
    expect(screen.queryByText('Group Ratio')).not.toBeInTheDocument()
    expect(screen.queryByText('Per Second')).not.toBeInTheDocument()
    expect(screen.queryByText('Refunded')).not.toBeInTheDocument()
  }
)

test.each([undefined, { amount_available: false }, { state: 'completed' }])(
  'does not turn unavailable billing amounts into free generations: %j',
  (billing) => {
    render(<TaskBillingDetails log={{ ...task, quota: 0, billing }} />)

    expect(screen.getByLabelText('Amount unavailable')).toHaveTextContent('—')
    expect(screen.queryByText('$0')).not.toBeInTheDocument()
    expect(screen.queryByText('Refunded')).not.toBeInTheDocument()
    expect(screen.queryByText('Settled')).not.toBeInTheDocument()
  }
)

test('keeps an explicitly available zero charge without inventing a refund', () => {
  render(
    <TaskBillingDetails
      log={{
        ...task,
        quota: 0,
        billing: { amount_available: true, state: 'completed' },
      }}
    />
  )

  expect(screen.getByText('$0')).toBeVisible()
  expect(screen.getByText('Billing processed')).toBeVisible()
  expect(screen.queryByText('Refunded')).not.toBeInTheDocument()
})
