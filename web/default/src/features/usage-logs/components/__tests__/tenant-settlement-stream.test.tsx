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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { afterEach, expect, test } from 'vitest'

import { usageLogSchema } from '../../data/schema'
import { DetailsDialog } from '../dialogs/details-dialog'
import { StreamTpsCell } from '../timing-metrics-cell'

const clients: QueryClient[] = []

afterEach(() => {
  for (const client of clients) client.clear()
  clients.length = 0
})

function renderDetails(
  isAdmin: boolean,
  settlementStatus: 'settlement_pending' | 'settled' = 'settlement_pending'
): void {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  client.setQueryData(['status'], {}, { updatedAt: Date.now() + 60_000 })
  const log = usageLogSchema.parse({
    id: 1,
    user_id: 7,
    created_at: 1,
    type: 2,
    content: '',
    quota: 900000,
    tenant_settlement: {
      status: settlementStatus,
      reserved_quota: 100000,
      actual_quota: 900000,
    },
    other: JSON.stringify({
      stream_status: {
        status: 'error',
        end_reason: 'scanner_error',
        error_count: 1,
      },
      admin_info: {
        stream_status: {
          status: 'error',
          end_error: 'private endpoint diagnosis',
          errors: ['private upstream message'],
        },
      },
    }),
  })
  render(
    <QueryClientProvider client={client}>
      <DetailsDialog
        log={log}
        isAdmin={isAdmin}
        isRoot={false}
        open
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )
}

test('explains pending settlement while showing only the safe stream summary to customers', () => {
  renderDetails(false)
  expect(screen.getByText('Awaiting settlement')).toBeVisible()
  expect(screen.getByText('Pre-consumed').nextElementSibling).toHaveTextContent(
    '$0.2'
  )
  expect(
    screen.getByText('Pending additional charge').nextElementSibling
  ).toHaveTextContent('$1.6')
  expect(
    screen.getByText(
      'Incurred cost; the balance updates when settlement completes.'
    )
  ).toBeVisible()
  expect(screen.getByText('scanner_error')).toBeVisible()
  expect(
    screen.queryByText('private endpoint diagnosis')
  ).not.toBeInTheDocument()
  expect(screen.queryByText('private upstream message')).not.toBeInTheDocument()
})

test('does not describe a settled charge as awaiting a balance update', () => {
  renderDetails(false, 'settled')
  expect(screen.getByText('Settled')).toBeVisible()
  expect(
    screen.queryByText('Pending additional charge')
  ).not.toBeInTheDocument()
  expect(
    screen.queryByText(
      'Incurred cost; the balance updates when settlement completes.'
    )
  ).not.toBeInTheDocument()
})

test('retains raw stream diagnostics in the admin details', () => {
  renderDetails(true)
  expect(screen.getByText('private endpoint diagnosis')).toBeVisible()
  expect(screen.getByText('private upstream message')).toBeVisible()
})

test('shows an accessible stream failure marker from the public summary', () => {
  const rendered = render(
    <StreamTpsCell
      isStream
      streamStatus={{
        status: 'error',
        end_reason: 'scanner_error',
        error_count: 1,
      }}
    />
  )
  expect(
    screen.getByRole('img', { name: 'Stream Status: Error' })
  ).toBeVisible()
  rendered.rerender(
    <StreamTpsCell
      isStream
      streamStatus={{ status: 'ok', end_reason: 'done' }}
    />
  )
  expect(
    screen.queryByRole('img', { name: 'Stream Status: Error' })
  ).not.toBeInTheDocument()
})
