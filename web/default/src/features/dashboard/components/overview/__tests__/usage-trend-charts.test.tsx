import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import type { QuotaDataItem } from '@/features/dashboard/types'

import UsageTrendCharts from '../usage-trend-charts'

vi.mock('@visactor/react-vchart', () => ({
  VChart: () => <div data-testid='vchart' />,
}))

const first = Date.parse('2026-01-01T00:00:00+08:00') / 1000
const rows: QuotaDataItem[] = [
  {
    created_at: first,
    model_name: 'model-a',
    count: 1234,
    quota: 500000,
  },
  {
    created_at: first + 86400,
    model_name: 'model-b',
    count: 5,
    quota: 500000,
  },
]

afterEach(() => {
  cleanup()
})

it('exposes chart data through keyboard-expandable tables', async () => {
  const user = userEvent.setup()
  render(
    <UsageTrendCharts
      rows={rows}
      granularity='day'
      range={{
        start_timestamp: first,
        end_timestamp: first + 86400,
      }}
    />
  )

  const tableViews = screen.getAllByText('Table view')
  expect(tableViews).toHaveLength(2)
  tableViews[0].focus()
  expect(tableViews[0]).toHaveFocus()
  await user.click(tableViews[0])
  await user.click(tableViews[1])

  const tables = screen.getAllByRole('table')
  expect(tables).toHaveLength(2)
  expect(
    within(tables[0])
      .getAllByRole('columnheader')
      .map((header) => header.textContent)
  ).toEqual(['Time', 'Model', 'Count'])
  expect(within(tables[0]).getAllByText('model-a')).not.toHaveLength(0)
  expect(within(tables[0]).getByText('1,234')).toBeVisible()
  expect(within(tables[1]).getByText('model-a')).toBeVisible()
  expect(within(tables[1]).getByText('1,234')).toBeVisible()
})
