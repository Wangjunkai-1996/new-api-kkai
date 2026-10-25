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
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { RebateApprovals } from '../components/admin/rebate-approvals'
import { RebateRequestActions } from '../components/admin/rebate-request-actions'
import type { RebateRequestAdmin } from '../types'

const request: RebateRequestAdmin = {
  id: 7,
  userId: 4,
  userName: 'sales-four',
  requestKind: 'sales',
  status: 'approved',
  amount: 1250,
  creditCurrency: 'usd_credit_v1',
  rebateRecordIds: [2],
  issuedAt: null,
  createdAt: '2026-10-01T00:00:00Z',
  updatedAt: '2026-10-01T00:00:00Z',
}

test('sales approval uses its own list and confirms external issue without balance payout', async () => {
  const user = userEvent.setup()
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url.endsWith('/status')) {
      return {
        data: {
          success: true,
          data: { available: true, settlementEnabled: true },
        },
      }
    }
    return {
      data: {
        success: true,
        data: { items: [request], total: 1, page: 1, pageSize: 20 },
      },
    }
  })
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: {
      success: true,
      data: {
        id: 7,
        requestKind: 'sales',
        issuedAt: '2026-10-14T00:00:00Z',
        issueNote: 'paid-ref-17',
      },
    },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RebateApprovals requestKind='sales' />
    </QueryClientProvider>
  )
  await screen.findByText('sales-four')
  expect(
    screen.getByRole('combobox', { name: 'Filter by status' })
  ).toHaveTextContent('Pending')
  expect(get).toHaveBeenCalledWith(
    '/invitations/api/admin/rebate-requests',
    expect.objectContaining({
      params: expect.objectContaining({ requestKind: 'sales' }),
    })
  )
  expect(
    screen.queryByRole('button', { name: /Approve and Pay/ })
  ).not.toBeInTheDocument()
  expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Actions' }))
  expect(
    screen.queryByRole('menuitem', { name: 'Approve and Pay' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('menuitem', { name: 'Mark Completed' })
  ).not.toBeInTheDocument()
  await user.click(screen.getByRole('menuitem', { name: 'Confirm Issue' }))
  expect(screen.getByRole('button', { name: 'Confirm' })).toBeDisabled()
  await user.type(
    screen.getByLabelText('Issue Reference or Note'),
    'paid-ref-17'
  )
  await user.click(screen.getByRole('button', { name: 'Confirm' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/invitations/api/admin/rebate-requests/7/confirm-issued',
      { note: 'paid-ref-17' },
      { skipErrorHandler: true }
    )
  )
  expect(post).toHaveBeenCalledTimes(1)
})

test('an externally issued sales request has no reset or payout actions', () => {
  render(
    <RebateRequestActions
      request={{ ...request, issuedAt: '2026-10-14T00:00:00Z' }}
      onAction={vi.fn()}
    />
  )
  expect(
    screen.queryByRole('button', { name: 'Actions' })
  ).not.toBeInTheDocument()
})

test('maintenance disables external issue confirmation while leaving review reset available', async () => {
  const user = userEvent.setup()
  render(
    <RebateRequestActions
      request={request}
      settlementEnabled={false}
      onAction={vi.fn()}
    />
  )
  await user.click(screen.getByRole('button', { name: 'Actions' }))
  expect(
    screen.getByRole('menuitem', { name: 'Confirm Issue' })
  ).toHaveAttribute('aria-disabled', 'true')
  expect(
    screen.getByRole('menuitem', { name: 'Reset Review' })
  ).not.toHaveAttribute('aria-disabled', 'true')
})

test.each([
  { name: 'populated', items: [request], total: 21 },
  { name: 'empty', items: [], total: 0 },
])('hides a cached $name approval list when refreshing fails', async (data) => {
  const user = userEvent.setup()
  let requestsFail = false
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url.endsWith('/status')) {
      return {
        data: {
          success: true,
          data: { available: true, settlementEnabled: true },
        },
      }
    }
    if (url.endsWith('/sales-profile')) {
      return {
        data: { success: true, data: { active: true, registered: true } },
      }
    }
    return {
      data: requestsFail
        ? { success: false, message: 'Database error' }
        : {
            success: true,
            data: {
              items: data.items,
              total: data.total,
              page: 1,
              pageSize: 20,
            },
          },
    }
  })
  const post = vi.spyOn(api, 'post')
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RebateApprovals requestKind='sales' />
    </QueryClientProvider>
  )
  await screen.findByText(
    data.items.length ? 'sales-four' : 'No rebate requests'
  )
  if (data.items.length) {
    expect(screen.getByRole('button', { name: 'Next page' })).toBeEnabled()
    await user.click(screen.getByRole('button', { name: 'Actions' }))
    await user.click(screen.getByRole('menuitem', { name: 'Confirm Issue' }))
    await user.type(
      screen.getByLabelText('Issue Reference or Note'),
      'paid-ref-17'
    )
    expect(screen.getByRole('button', { name: 'Confirm' })).toBeEnabled()
  }

  requestsFail = true
  await act(async () => {
    await client.refetchQueries({
      queryKey: ['kkai', 'invitations', 'admin', 'requests'],
    })
  })

  await screen.findByText('Failed to load rebate requests')
  expect(screen.getByText('Database error')).toBeInTheDocument()
  expect(screen.queryByText('No rebate requests')).not.toBeInTheDocument()
  expect(screen.queryByText('sales-four')).not.toBeInTheDocument()
  expect(screen.queryByRole('table')).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Next page' })
  ).not.toBeInTheDocument()
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(post).not.toHaveBeenCalled()

  requestsFail = false
  await user.click(screen.getByRole('button', { name: 'Retry' }))
  await screen.findByText(
    data.items.length ? 'sales-four' : 'No rebate requests'
  )
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  expect(post).not.toHaveBeenCalled()
})
