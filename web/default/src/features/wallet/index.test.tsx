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
  QueryClient,
  QueryClientProvider,
} from '@tanstack/react-query'
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import { AxiosError } from 'axios'
import { useState } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { getTopupInfo, getWalletBalance } from './api'
import { Wallet } from './index'
import type { TopupInfo, UserWalletData, WalletPageProps } from './types'

const identity = vi.hoisted(() => ({
  host: 'example.test',
  tenant: 0 as number | undefined,
  user: 1,
  sid: 'session',
}))
vi.mock('./api', () => ({ getTopupInfo: vi.fn(), getWalletBalance: vi.fn() }))
vi.mock('./hooks/use-wallet-payment-flow', () => ({
  useWalletPaymentIdentity: () => [
    identity.host,
    identity.tenant,
    identity.user,
    identity.sid,
  ],
}))
vi.mock('./legacy-wallet-page', () => ({
  LegacyWalletPage: (props: WalletPageProps) => (
    <div data-testid='legacy-wallet' data-writes={props.writesEnabled}>
      {props.initialTradeNo}:{props.historyScope}
      <button type='button' onClick={props.onLegacyPaymentStarted}>
        Start legacy payment
      </button>
      {props.historyOpen && <p>Legacy history</p>}
    </div>
  ),
}))
vi.mock('./lifecycle-wallet-page', () => ({
  LifecycleWalletPage: function MockLifecyclePage(props: WalletPageProps) {
    const [amount, setAmount] = useState('10')
    return (
      <div data-testid='lifecycle-wallet' data-writes={props.writesEnabled}>
        {props.initialTradeNo}:{props.historyScope}
        <input
          aria-label='Payment input'
          value={amount}
          onChange={(event) => setAmount(event.target.value)}
        />
        {props.historyOpen && <p>Lifecycle history</p>}
      </div>
    )
  },
}))
vi.mock('./components/wallet-stats-card', () => ({
  WalletStatsCard: (props: {
    user: UserWalletData | null
    loading: boolean
    fundingSummaryEnabled?: boolean
  }) => (
    <div data-testid='wallet-overview'>
      {props.loading ? 'Balance loading' : props.user?.quota}
      {props.fundingSummaryEnabled && <p>Wallet funding statistics enabled</p>}
    </div>
  ),
}))
vi.mock('@/components/layout', () => {
  return {
    Main: (props: { children: React.ReactNode }) => (
      <main>{props.children}</main>
    ),
  }
})
function info(ready?: boolean): TopupInfo {
  return {
    wallet_lifecycle_ready: ready,
    enable_online_topup: true,
    enable_stripe_topup: false,
    pay_methods: [],
    min_topup: 1,
    stripe_min_topup: 1,
    amount_options: [10],
    discount: {},
  }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}
const clients: QueryClient[] = []
function renderWallet(
  props: React.ComponentProps<typeof Wallet> = {},
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
) {
  clients.push(client)
  const view = render(
    <QueryClientProvider client={client}>
      <Wallet {...props} />
    </QueryClientProvider>
  )
  return {
    ...view,
    client,
    rerenderWallet: () =>
      view.rerender(
        <QueryClientProvider client={client}>
          <Wallet {...props} />
        </QueryClientProvider>
      ),
  }
}
beforeEach(() => {
  vi.resetAllMocks()
  Object.assign(identity, {
    host: 'example.test',
    tenant: 0,
    user: 1,
    sid: 'session',
  })
  vi.mocked(getWalletBalance).mockResolvedValue({
    success: true,
    data: { quota: 125 } as UserWalletData,
  })
})
afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.useRealTimers()
})
it('renders the wallet shell and balance while configuration is still pending', async () => {
  const pending = deferred<Awaited<ReturnType<typeof getTopupInfo>>>()
  vi.mocked(getTopupInfo).mockReturnValue(pending.promise)
  renderWallet()
  expect(screen.getByRole('heading', { name: 'Wallet' })).toBeVisible()
  expect(screen.getByRole('button', { name: 'Order History' })).toBeEnabled()
  expect(screen.getAllByTestId('wallet-overview')).toHaveLength(1)
  await waitFor(() =>
    expect(screen.getByTestId('wallet-overview')).toHaveTextContent('125')
  )
  expect(getTopupInfo).toHaveBeenCalledTimes(1)
  expect(getWalletBalance).toHaveBeenCalledTimes(1)
  expect(screen.getByText('Loading wallet payment options...')).toBeVisible()
  expect(
    screen.queryByText('Restoring your top-up order...')
  ).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Order History' }))
  expect(
    screen.getByText(
      'Order history will load after payment availability is confirmed.'
    )
  ).toBeVisible()
  expect(screen.queryByText('Legacy history')).not.toBeInTheDocument()
  expect(screen.queryByText('Lifecycle history')).not.toBeInTheDocument()
  await act(async () => pending.resolve({ success: true, data: info(true) }))
  expect(await screen.findByText('Lifecycle history')).toBeVisible()
})
it.each([false, undefined])(
  'uses legacy recharge only after successful readiness=%s',
  async (ready) => {
    vi.mocked(getTopupInfo).mockResolvedValue({
      success: true,
      data: info(ready),
    })
    renderWallet({ initialTradeNo: 'original-order', historyScope: 'admin' })
    expect(await screen.findByTestId('legacy-wallet')).toHaveTextContent(
      'original-order:admin'
    )
    expect(screen.getByTestId('legacy-wallet')).toHaveAttribute(
      'data-writes',
      'true'
    )
    expect(screen.queryByTestId('lifecycle-wallet')).not.toBeInTheDocument()
    expect(getTopupInfo).toHaveBeenCalledTimes(1)
  }
)
it('mounts lifecycle with a single persistent overview and passes route context', async () => {
  vi.mocked(getTopupInfo).mockResolvedValue({ success: true, data: info(true) })
  renderWallet({ initialTradeNo: 'original-order', historyScope: 'self' })
  expect(await screen.findByTestId('lifecycle-wallet')).toHaveTextContent(
    'original-order:self'
  )
  expect(screen.getByText('Wallet funding statistics enabled')).toBeVisible()
  expect(screen.getAllByTestId('wallet-overview')).toHaveLength(1)
})
it('blocks failed and malformed initial configuration without hiding the balance', async () => {
  vi.mocked(getTopupInfo)
    .mockResolvedValueOnce({ success: false, data: info(false) })
    .mockResolvedValueOnce({
      success: true,
      data: {
        ...info(),
        wallet_lifecycle_ready: 'false' as unknown as boolean,
      },
    })
    .mockResolvedValue({ success: true, data: info(false) })
  renderWallet()
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'New payments are temporarily unavailable.'
  )
  expect(screen.getByTestId('wallet-overview')).toHaveTextContent('125')
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
  await waitFor(() =>
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Payment availability changed.'
    )
  )
  expect(screen.queryByTestId('legacy-wallet')).not.toBeInTheDocument()
  expect(screen.queryByTestId('lifecycle-wallet')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
  expect(await screen.findByTestId('legacy-wallet')).toBeVisible()
})
it('retains cached lifecycle content while a remount refresh is pending or fails', async () => {
  vi.mocked(getTopupInfo).mockResolvedValueOnce({
    success: true,
    data: info(true),
  })
  const first = renderWallet()
  await screen.findByTestId('lifecycle-wallet')
  first.unmount()
  const pending = deferred<Awaited<ReturnType<typeof getTopupInfo>>>()
  vi.mocked(getTopupInfo)
    .mockReturnValueOnce(pending.promise)
    .mockRejectedValue(new Error('network'))
  const next = renderWallet({}, first.client)
  expect(await screen.findByTestId('lifecycle-wallet')).toHaveAttribute(
    'data-writes',
    'true'
  )
  expect(screen.getByTestId('wallet-overview')).toHaveTextContent('125')
  await act(async () => pending.resolve({ success: true, data: info(true) }))
  await act(async () => {
    await next.client.invalidateQueries({
      queryKey: ['wallet-payment', 'example.test', 0, 1, 'session', 'info'],
    })
  })
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'The displayed options may be out of date.'
  )
  expect(screen.getByTestId('lifecycle-wallet')).toHaveAttribute(
    'data-writes',
    'true'
  )
})
it.each([false, 'malformed'])(
  'preserves lifecycle input and suspends writes after readiness changes to %s',
  async (ready) => {
    vi.mocked(getTopupInfo)
      .mockResolvedValueOnce({ success: true, data: info(true) })
      .mockResolvedValue({ success: true, data: info(ready as boolean) })
    const view = renderWallet()
    await screen.findByTestId('lifecycle-wallet')
    fireEvent.change(screen.getByLabelText('Payment input'), {
      target: { value: '207' },
    })
    await act(async () => {
      await view.client.invalidateQueries({
        queryKey: ['wallet-payment', 'example.test', 0, 1, 'session', 'info'],
      })
    })
    await waitFor(() =>
      expect(screen.getByTestId('lifecycle-wallet')).toHaveAttribute(
        'data-writes',
        'false'
      )
    )
    expect(screen.getByLabelText('Payment input')).toHaveValue('207')
    expect(screen.queryByTestId('legacy-wallet')).not.toBeInTheDocument()
    expect(screen.getAllByTestId('wallet-overview')).toHaveLength(1)
  }
)
it('requires a fresh config before mounting cached legacy and can upgrade safely', async () => {
  vi.mocked(getTopupInfo).mockResolvedValueOnce({
    success: true,
    data: info(false),
  })
  const first = renderWallet()
  await screen.findByTestId('legacy-wallet')
  first.unmount()
  const pending = deferred<Awaited<ReturnType<typeof getTopupInfo>>>()
  vi.mocked(getTopupInfo).mockReturnValue(pending.promise)
  renderWallet({}, first.client)
  expect(screen.queryByTestId('legacy-wallet')).not.toBeInTheDocument()
  await act(async () => pending.resolve({ success: true, data: info(true) }))
  expect(await screen.findByTestId('lifecycle-wallet')).toBeVisible()
})
it('retains a legacy page after a payment starts instead of losing an unresolved attempt on upgrade', async () => {
  vi.mocked(getTopupInfo)
    .mockResolvedValueOnce({ success: true, data: info(false) })
    .mockResolvedValue({ success: true, data: info(true) })
  const view = renderWallet()
  fireEvent.click(
    await screen.findByRole('button', { name: 'Start legacy payment' })
  )
  await act(async () => {
    await view.client.invalidateQueries({
      queryKey: ['wallet-payment', 'example.test', 0, 1, 'session', 'info'],
    })
  })
  await waitFor(() =>
    expect(screen.getByTestId('legacy-wallet')).toHaveAttribute(
      'data-writes',
      'false'
    )
  )
  expect(screen.queryByTestId('lifecycle-wallet')).not.toBeInTheDocument()
})
it('deduplicates simultaneous config and balance observers', async () => {
  const pending = deferred<Awaited<ReturnType<typeof getTopupInfo>>>()
  const pendingBalance =
    deferred<Awaited<ReturnType<typeof getWalletBalance>>>()
  vi.mocked(getTopupInfo).mockReturnValue(pending.promise)
  vi.mocked(getWalletBalance).mockReturnValue(pendingBalance.promise)
  const first = renderWallet()
  renderWallet({}, first.client)
  await waitFor(() => expect(getTopupInfo).toHaveBeenCalledTimes(1))
  expect(getWalletBalance).toHaveBeenCalledTimes(1)
})
it.each(['user', 'tenant', 'sid', 'host'] as const)(
  'isolates a changed %s from late previous identity responses',
  async (field) => {
    const old = deferred<Awaited<ReturnType<typeof getTopupInfo>>>()
    vi.mocked(getTopupInfo)
      .mockReturnValueOnce(old.promise)
      .mockRejectedValue(new Error('new identity unavailable'))
    const view = renderWallet()
    await waitFor(() => expect(getTopupInfo).toHaveBeenCalledTimes(1))
    const oldSignal = vi.mocked(getTopupInfo).mock.calls[0]?.[0]
    if (field === 'user') identity.user = 2
    if (field === 'tenant') identity.tenant = 2
    if (field === 'sid') identity.sid = 'new-session'
    if (field === 'host') identity.host = 'other.test'
    view.rerenderWallet()
    expect(await screen.findByRole('alert')).toBeVisible()
    await act(async () => old.resolve({ success: true, data: info(true) }))
    expect(oldSignal?.aborted).toBe(true)
    expect(screen.queryByTestId('legacy-wallet')).not.toBeInTheDocument()
    expect(screen.queryByTestId('lifecycle-wallet')).not.toBeInTheDocument()
  }
)
it('does not start private config or balance reads before tenant identity is known', () => {
  identity.tenant = undefined
  renderWallet()
  expect(getTopupInfo).not.toHaveBeenCalled()
  expect(getWalletBalance).not.toHaveBeenCalled()
})
it('periodically checks legacy capability and stops after lifecycle activation', async () => {
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
  vi.mocked(getTopupInfo)
    .mockResolvedValueOnce({ success: true, data: info(false) })
    .mockResolvedValue({ success: true, data: info(true) })
  renderWallet()
  expect(await screen.findByTestId('legacy-wallet')).toBeVisible()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(30_000)
  })
  expect(await screen.findByTestId('lifecycle-wallet')).toBeVisible()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60_000)
  })
  expect(getTopupInfo).toHaveBeenCalledTimes(2)
})

it('retains confirmed lifecycle mode across remounts with contradictory cached capability', async () => {
  vi.mocked(getTopupInfo)
    .mockResolvedValueOnce({ success: true, data: info(true) })
    .mockResolvedValue({ success: true, data: info(false) })
  const first = renderWallet()
  await screen.findByTestId('lifecycle-wallet')
  await act(async () => {
    await first.client.invalidateQueries({
      queryKey: ['wallet-payment', 'example.test', 0, 1, 'session', 'info'],
    })
  })
  await waitFor(() =>
    expect(screen.getByTestId('lifecycle-wallet')).toHaveAttribute(
      'data-writes',
      'false'
    )
  )
  first.unmount()
  const pending = deferred<Awaited<ReturnType<typeof getTopupInfo>>>()
  vi.mocked(getTopupInfo).mockReturnValue(pending.promise)
  renderWallet({}, first.client)
  expect(await screen.findByTestId('lifecycle-wallet')).toHaveAttribute(
    'data-writes',
    'false'
  )
  expect(screen.queryByTestId('legacy-wallet')).not.toBeInTheDocument()
  await act(async () => pending.resolve({ success: true, data: info(true) }))
  await waitFor(() =>
    expect(screen.getByTestId('lifecycle-wallet')).toHaveAttribute(
      'data-writes',
      'true'
    )
  )
})
it('refreshes stale configuration through Query focus events and respects the retry-after deadline', async () => {
  const limited = new AxiosError('limited', undefined, undefined, undefined, {
    status: 429,
    headers: { 'retry-after': '600' },
    data: {},
  } as never)
  vi.mocked(getTopupInfo)
    .mockResolvedValueOnce({ success: true, data: info(false) })
    .mockRejectedValue(limited)
  const view = renderWallet()
  await screen.findByTestId('legacy-wallet')
  await act(async () => {
    await view.client.invalidateQueries({
      queryKey: ['wallet-payment', 'example.test', 0, 1, 'session', 'info'],
      refetchType: 'none',
    })
    focusManager.setFocused(false)
    focusManager.setFocused(true)
  })
  await waitFor(() => expect(getTopupInfo).toHaveBeenCalledTimes(2))
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Too many requests.'
  )
  await act(async () => {
    focusManager.setFocused(false)
    focusManager.setFocused(true)
  })
  expect(getTopupInfo).toHaveBeenCalledTimes(2)
  focusManager.setFocused(undefined)
})

it('preserves retry-after across remounts instead of automatically repeating a rejected config read', async () => {
  const limited = new AxiosError('limited', undefined, undefined, undefined, {
    status: 429,
    headers: { 'retry-after': '600' },
    data: {},
  } as never)
  vi.mocked(getTopupInfo).mockRejectedValue(limited)
  const first = renderWallet()
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Too many requests.'
  )
  first.unmount()
  renderWallet({}, first.client)
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Too many requests.'
  )
  expect(getTopupInfo).toHaveBeenCalledTimes(1)
  expect(screen.queryByTestId('legacy-wallet')).not.toBeInTheDocument()
})

it('does not repeat a rate-limited balance read on focus or remount', async () => {
  vi.mocked(getTopupInfo).mockResolvedValue({ success: true, data: info(true) })
  const limited = new AxiosError('limited', undefined, undefined, undefined, {
    status: 429,
    headers: { 'retry-after': '600' },
    data: {},
  } as never)
  vi.mocked(getWalletBalance).mockRejectedValue(limited)
  const first = renderWallet()
  await screen.findByTestId('lifecycle-wallet')
  expect(getWalletBalance).toHaveBeenCalledTimes(1)
  await act(async () => {
    focusManager.setFocused(false)
    focusManager.setFocused(true)
  })
  expect(getWalletBalance).toHaveBeenCalledTimes(1)
  first.unmount()
  renderWallet({}, first.client)
  await screen.findByTestId('lifecycle-wallet')
  expect(getWalletBalance).toHaveBeenCalledTimes(1)
  focusManager.setFocused(undefined)
})
