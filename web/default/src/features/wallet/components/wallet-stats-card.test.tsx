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
  focusManager,
  onlineManager,
  QueryClient,
  QueryClientProvider,
} from '@tanstack/react-query'
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import i18next from 'i18next'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import {
  getWalletFundingSummary,
  type WalletFundingSummary,
} from '../funding-summary-api'
import type { UserWalletData } from '../types'
import { WalletStatsCard } from './wallet-stats-card'

vi.mock('../funding-summary-api', () => ({
  getWalletFundingSummary: vi.fn(),
}))
const identity = vi.hoisted(() => ({ tenant: 0 as number | undefined }))
vi.mock('../hooks/use-wallet-payment-flow', () => ({
  useWalletPaymentIdentity: () => [
    'example.test',
    identity.tenant,
    1,
    'session',
  ],
}))
const user: UserWalletData = {
  id: 1,
  username: 'test',
  quota: 5_000_000,
  used_quota: 1_000_000,
  request_count: 123,
  aff_quota: 0,
  aff_history_quota: 0,
  aff_count: 0,
  group: 'default',
}
const summary: WalletFundingSummary = {
  cash_payments: [{ amount_minor: '1000', currency: 'USD', exponent: 2 }],
  confirmed_payment_count: 1,
  unknown_payment_count: 0,
  rebate_credited_quota: '0',
  rebate_credit_display: { amount: '0', unit: 'USD' },
  quota_epoch: 'usd_credit_v1',
  refund_adjusted: false,
  server_time: 1_790_000_000,
}

let client: QueryClient
beforeEach(() => {
  vi.resetAllMocks()
  identity.tenant = 0
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  })
})

it('shows completed payment totals while balance is still loading', async () => {
  vi.mocked(getWalletFundingSummary).mockResolvedValue(summary)
  render(
    <QueryClientProvider client={client}>
      <WalletStatsCard user={null} loading fundingSummaryEnabled />
    </QueryClientProvider>
  )
  expect(await screen.findByText('$10.00')).toBeVisible()
  expect(
    screen.getByRole('region', { name: 'Current Balance' })
  ).toHaveAttribute('aria-busy', 'true')
  expect(screen.getByText('Loading wallet balance...')).toBeVisible()
  expect(screen.getByText('Total consumed credits')).toBeVisible()
})

it('shows balance and usage while payment totals are still loading', () => {
  vi.mocked(getWalletFundingSummary).mockReturnValue(new Promise(() => {}))
  render(
    <QueryClientProvider client={client}>
      <WalletStatsCard user={user} fundingSummaryEnabled />
    </QueryClientProvider>
  )
  expect(screen.getByText('123')).toBeVisible()
  expect(
    within(screen.getByRole('region', { name: 'Current Balance' })).queryByText(
      'Not available'
    )
  ).not.toBeInTheDocument()
  expect(screen.getByText('Loading payment totals...')).toBeVisible()
})

it('keeps cached payment totals after refresh failure and retries only their query', async () => {
  client.setQueryData(
    ['wallet-payment', 'example.test', 0, 1, 'session', 'funding-summary'],
    summary
  )
  vi.mocked(getWalletFundingSummary)
    .mockRejectedValueOnce(new Error('unavailable'))
    .mockResolvedValue({
      ...summary,
      cash_payments: [{ amount_minor: '2000', currency: 'USD', exponent: 2 }],
    })
  const balanceRetry = vi.fn()
  render(
    <QueryClientProvider client={client}>
      <WalletStatsCard
        user={user}
        onBalanceRetry={balanceRetry}
        fundingSummaryEnabled
      />
    </QueryClientProvider>
  )
  expect(
    await screen.findByText(
      'We could not refresh your payment totals. The displayed totals may be out of date.'
    )
  ).toBeVisible()
  expect(screen.getByText('$10.00')).toBeVisible()
  expect(screen.getByText('Last updated:')).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'Retry payment totals' }))
  expect(await screen.findByText('$20.00')).toBeVisible()
  expect(balanceRetry).not.toHaveBeenCalled()
  expect(getWalletFundingSummary).toHaveBeenCalledTimes(2)
})

it('shows a local failure instead of zero for unavailable totals and preserves balance', async () => {
  vi.mocked(getWalletFundingSummary).mockRejectedValue(new Error('unavailable'))
  render(
    <QueryClientProvider client={client}>
      <WalletStatsCard user={user} fundingSummaryEnabled />
    </QueryClientProvider>
  )
  expect(
    await screen.findByText('We could not load your payment totals. Try again.')
  ).toBeVisible()
  expect(
    within(
      screen.getByRole('region', { name: 'Total paid top-ups' })
    ).getByText('Not available')
  ).toBeVisible()
  expect(screen.getByText('123')).toBeVisible()
  expect(screen.queryByText('No confirmed payments')).not.toBeInTheDocument()
})

it('retains cached balance and its timestamp after a refresh failure', () => {
  const retry = vi.fn()
  render(
    <QueryClientProvider client={client}>
      <WalletStatsCard
        user={user}
        balanceError={new Error('unavailable')}
        balanceUpdatedAt={1_790_000_000_000}
        onBalanceRetry={retry}
      />
    </QueryClientProvider>
  )
  expect(screen.getByText('123')).toBeVisible()
  expect(
    screen.getByText(
      'We could not refresh your wallet balance. The displayed balance may be out of date.'
    )
  ).toBeVisible()
  expect(screen.getByText('Last updated:')).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'Retry balance' }))
  expect(retry).toHaveBeenCalledTimes(1)
  expect(getWalletFundingSummary).not.toHaveBeenCalled()
})

it('does not read summary before tenant identity is confirmed', () => {
  identity.tenant = undefined
  render(
    <QueryClientProvider client={client}>
      <WalletStatsCard user={null} fundingSummaryEnabled />
    </QueryClientProvider>
  )
  expect(getWalletFundingSummary).not.toHaveBeenCalled()
})

it('cancels a prior tenant query and ignores its late result', async () => {
  let resolveOld: ((value: WalletFundingSummary) => void) | undefined
  let oldSignal: AbortSignal | undefined
  vi.mocked(getWalletFundingSummary)
    .mockImplementationOnce((signal) => {
      oldSignal = signal
      return new Promise((resolve) => {
        resolveOld = resolve
      })
    })
    .mockResolvedValue({
      ...summary,
      cash_payments: [{ amount_minor: '2000', currency: 'USD', exponent: 2 }],
    })
  const view = render(
    <QueryClientProvider client={client}>
      <WalletStatsCard user={null} fundingSummaryEnabled />
    </QueryClientProvider>
  )
  await waitFor(() => expect(getWalletFundingSummary).toHaveBeenCalledTimes(1))
  identity.tenant = 2
  view.rerender(
    <QueryClientProvider client={client}>
      <WalletStatsCard user={null} fundingSummaryEnabled />
    </QueryClientProvider>
  )
  expect(await screen.findByText('$20.00')).toBeVisible()
  expect(oldSignal?.aborted).toBe(true)
  await act(async () => resolveOld?.(summary))
  expect(screen.queryByText('$10.00')).not.toBeInTheDocument()
})
afterEach(() => {
  client.clear()
  focusManager.setFocused(undefined)
  onlineManager.setOnline(true)
  void i18next.changeLanguage('en')
})

it.each([
  ['zhCN', 'zh-CN', 'balance'],
  ['zhTW', 'zh-TW', 'balance'],
  ['zhCN', 'zh-CN', 'totals'],
  ['zhTW', 'zh-TW', 'totals'],
] as const)(
  'renders cached %s %s %s timestamps without losing the wallet',
  async (language, locale, source) => {
    const updatedAt = 1_790_000_000_000
    await i18next.changeLanguage(language)
    if (source === 'totals') {
      client.setQueryData(
        ['wallet-payment', 'example.test', 0, 1, 'session', 'funding-summary'],
        summary,
        { updatedAt }
      )
      vi.mocked(getWalletFundingSummary).mockReturnValue(new Promise(() => {}))
    }
    const view = render(
      <QueryClientProvider client={client}>
        <WalletStatsCard
          user={user}
          balanceUpdatedAt={source === 'balance' ? updatedAt : undefined}
          fundingSummaryEnabled={source === 'totals'}
        />
      </QueryClientProvider>
    )
    expect(screen.getByText('123')).toBeVisible()
    const time = view.container.querySelector('time')
    expect(time).toHaveAttribute('dateTime', new Date(updatedAt).toISOString())
    expect(time).toHaveTextContent(
      new Date(updatedAt).toLocaleTimeString(locale)
    )
  }
)

it('ends offline initial loading with a local error instead of a paused query', async () => {
  onlineManager.setOnline(false)
  vi.mocked(getWalletFundingSummary).mockRejectedValue(new Error('offline'))
  render(
    <QueryClientProvider client={client}>
      <WalletStatsCard user={user} fundingSummaryEnabled />
    </QueryClientProvider>
  )
  expect(
    await screen.findByText('We could not load your payment totals. Try again.')
  ).toBeVisible()
  expect(
    screen.queryByText('Loading payment totals...')
  ).not.toBeInTheDocument()
  expect(getWalletFundingSummary).toHaveBeenCalledTimes(1)
})

it('does not retry throttled totals on focus or reconnect during cooldown', async () => {
  vi.mocked(getWalletFundingSummary).mockRejectedValue({
    isAxiosError: true,
    response: {
      status: 429,
      data: {},
      headers: { 'retry-after': '60' },
    },
  })
  render(
    <QueryClientProvider client={client}>
      <WalletStatsCard user={user} fundingSummaryEnabled />
    </QueryClientProvider>
  )
  await screen.findByRole('alert')
  expect(
    screen.getByRole('button', { name: 'Retry payment totals' })
  ).toBeDisabled()
  await act(async () => {
    focusManager.setFocused(false)
    focusManager.setFocused(true)
    onlineManager.setOnline(false)
    onlineManager.setOnline(true)
  })
  expect(getWalletFundingSummary).toHaveBeenCalledTimes(1)
})

it('does not request schema18 funding data for the legacy wallet, even with cached statistics', () => {
  client.setQueryData(
    ['wallet-payment', 'example.test', 0, 1, 'session', 'funding-summary'],
    { cash_payments: [{ amount_minor: '1000', currency: 'USD', exponent: 2 }] }
  )
  render(
    <QueryClientProvider client={client}>
      <WalletStatsCard user={null} />
    </QueryClientProvider>
  )
  expect(screen.getByText('Current Balance')).toBeVisible()
  expect(screen.queryByText('$10.00')).not.toBeInTheDocument()
  expect(getWalletFundingSummary).not.toHaveBeenCalled()
})

it('requests and displays funding statistics when the lifecycle wallet enables them', async () => {
  vi.mocked(getWalletFundingSummary).mockResolvedValue({
    cash_payments: [{ amount_minor: '1000', currency: 'USD', exponent: 2 }],
    confirmed_payment_count: 1,
    unknown_payment_count: 0,
    rebate_credited_quota: '0',
    rebate_credit_display: { amount: '0', unit: 'USD' },
    quota_epoch: 'usd_credit_v1',
    refund_adjusted: false,
    server_time: 1_790_000_000,
  })
  render(
    <QueryClientProvider client={client}>
      <WalletStatsCard user={null} fundingSummaryEnabled />
    </QueryClientProvider>
  )
  expect(await screen.findByText('$10.00')).toBeVisible()
  expect(getWalletFundingSummary).toHaveBeenCalledTimes(1)
})
