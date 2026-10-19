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
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import * as adminApi from './admin-api'
import { AdminWithdrawals } from './admin-withdrawals'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key, i18n: { language: 'en' } }),
}))
vi.mock('@/lib/handle-server-error', () => ({ handleServerError: vi.fn() }))
const clients: QueryClient[] = []
afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
})
beforeEach(() => {
  vi.spyOn(adminApi, 'getManagedWithdrawals').mockResolvedValue({
    items: [
      {
        id: 11,
        tenant_id: 47,
        amount_minor: 10000,
        fee_minor: 100,
        net_minor: 9900,
        status: 'approved',
        review_note: '',
        payment_ref: '',
        created_at: 1,
        updated_at: 1,
        alipay_account: 'owner@example.com',
        alipay_name: 'Owner',
        reviewed_by: 1,
        reviewed_at: 1,
        paid_at: 0,
        timeline: '[]',
      },
    ],
    total: 1,
  })
})

function renderReview() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <AdminWithdrawals />
    </QueryClientProvider>
  )
}

test('a manual payout cannot be marked paid without a payment reference', async () => {
  const review = vi.spyOn(adminApi, 'reviewWithdrawal').mockResolvedValue()
  renderReview()
  const user = userEvent.setup()
  await user.click(await screen.findByRole('button', { name: 'Record Paid' }))
  const dialog = await screen.findByRole('alertdialog')
  const confirm = within(dialog).getByRole('button', { name: 'Continue' })
  expect(confirm).toBeDisabled()
  expect(review).not.toHaveBeenCalled()
  await user.type(
    within(dialog).getByLabelText('Payment Reference'),
    'alipay-confirmed-11'
  )
  await user.type(
    within(dialog).getByLabelText('Review Note'),
    'Receipt matched'
  )
  await user.click(confirm)
  await waitFor(() =>
    expect(review).toHaveBeenCalledExactlyOnceWith(11, {
      status: 'paid',
      payment_ref: 'alipay-confirmed-11',
      note: 'Receipt matched',
    })
  )
})

test('releasing frozen funds after confirmed failure requires an explanatory note', async () => {
  const review = vi.spyOn(adminApi, 'reviewWithdrawal').mockResolvedValue()
  renderReview()
  const user = userEvent.setup()
  await user.click(await screen.findByRole('button', { name: 'Mark Failed' }))
  const dialog = await screen.findByRole('alertdialog')
  const confirm = within(dialog).getByRole('button', { name: 'Continue' })
  expect(confirm).toBeDisabled()
  await user.type(
    within(dialog).getByLabelText('Review Note'),
    'Alipay confirmed transfer did not complete'
  )
  await user.click(confirm)
  await waitFor(() =>
    expect(review).toHaveBeenCalledExactlyOnceWith(11, {
      status: 'failed',
      payment_ref: '',
      note: 'Alipay confirmed transfer did not complete',
    })
  )
})

test('withdrawals after the first page remain reachable', async () => {
  vi.mocked(adminApi.getManagedWithdrawals).mockResolvedValue({
    items: [],
    total: 21,
  })
  renderReview()
  const user = userEvent.setup()
  const next = await screen.findByRole('button', { name: 'Next' })
  expect(screen.getByRole('button', { name: 'Previous' })).toBeDisabled()
  await user.click(next)
  await waitFor(() =>
    expect(adminApi.getManagedWithdrawals).toHaveBeenCalledWith(2, 20)
  )
  expect(await screen.findByRole('button', { name: 'Next' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Previous' })).toBeEnabled()
})
