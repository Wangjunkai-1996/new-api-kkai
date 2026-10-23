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
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { getTopupInfo } from './api'
import { Wallet } from './index'
import type { TopupInfo, WalletPageProps } from './types'

const identity = vi.hoisted(() => ({ user: 1 }))
vi.mock('./api', () => ({ getTopupInfo: vi.fn() }))
vi.mock('./hooks/use-wallet-payment-flow', () => ({
  useWalletPaymentIdentity: () => ['example.test', 0, identity.user, 'session'],
}))
vi.mock('./legacy-wallet-page', () => ({
  LegacyWalletPage: (props: WalletPageProps) => (
    <div data-testid='legacy-wallet'>
      {props.initialTradeNo}:{props.historyScope}
    </div>
  ),
}))
vi.mock('./lifecycle-wallet-page', () => ({
  LifecycleWalletPage: (props: WalletPageProps) => (
    <div data-testid='lifecycle-wallet'>
      {props.initialTradeNo}:{props.historyScope}
    </div>
  ),
}))
vi.mock('@/lib/handle-server-error', () => ({ handleServerError: vi.fn() }))
vi.mock('@/components/layout', () => {
  const Layout = Object.assign(
    (props: { children: React.ReactNode }) => <main>{props.children}</main>,
    {
      Title: (props: { children: React.ReactNode }) => (
        <h1>{props.children}</h1>
      ),
      Content: (props: { children: React.ReactNode }) => (
        <section>{props.children}</section>
      ),
    }
  )
  return { SectionPageLayout: Layout }
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
beforeEach(() => {
  vi.resetAllMocks()
  identity.user = 1
})
afterEach(() => {
  vi.useRealTimers()
})
it.each([false, undefined])(
  'uses legacy recharge only after successful readiness=%s',
  async (ready) => {
    vi.mocked(getTopupInfo).mockResolvedValue({
      success: true,
      data: info(ready),
    })
    render(<Wallet initialTradeNo='original-order' historyScope='admin' />)
    expect(await screen.findByTestId('legacy-wallet')).toHaveTextContent(
      'original-order:admin'
    )
    expect(screen.queryByTestId('lifecycle-wallet')).not.toBeInTheDocument()
    expect(getTopupInfo).toHaveBeenCalledTimes(1)
  }
)
it('mounts the existing lifecycle wallet only when canonical readiness is true', async () => {
  vi.mocked(getTopupInfo).mockResolvedValue({ success: true, data: info(true) })
  render(<Wallet initialTradeNo='original-order' historyScope='self' />)
  expect(await screen.findByTestId('lifecycle-wallet')).toHaveTextContent(
    'original-order:self'
  )
  expect(screen.queryByTestId('legacy-wallet')).not.toBeInTheDocument()
})
it('blocks purchase flows for unsuccessful or malformed readiness responses', async () => {
  vi.mocked(getTopupInfo)
    .mockResolvedValueOnce({ success: false, data: info(false) })
    .mockResolvedValue({
      success: true,
      data: {
        ...info(),
        wallet_lifecycle_ready: 'false' as unknown as boolean,
      },
    })
  render(<Wallet />)
  expect(await screen.findByRole('alert')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
  await waitFor(() => expect(getTopupInfo).toHaveBeenCalledTimes(2))
  expect(screen.getByRole('alert')).toBeInTheDocument()
  expect(screen.queryByTestId('legacy-wallet')).not.toBeInTheDocument()
  expect(screen.queryByTestId('lifecycle-wallet')).not.toBeInTheDocument()
})
it('blocks both purchase flows after initial failure and supports retry', async () => {
  vi.mocked(getTopupInfo)
    .mockRejectedValueOnce(new Error('network'))
    .mockResolvedValue({ success: true, data: info(false) })
  render(<Wallet />)
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'New payments are temporarily unavailable.'
  )
  expect(screen.queryByTestId('legacy-wallet')).not.toBeInTheDocument()
  expect(screen.queryByTestId('lifecycle-wallet')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
  expect(await screen.findByTestId('legacy-wallet')).toBeInTheDocument()
})
it('keeps the current mode on background failures and upgrades on focus after migration', async () => {
  vi.mocked(getTopupInfo)
    .mockResolvedValueOnce({ success: true, data: info(false) })
    .mockRejectedValueOnce(new Error('network'))
    .mockResolvedValue({ success: true, data: info(true) })
  render(<Wallet />)
  expect(await screen.findByTestId('legacy-wallet')).toBeInTheDocument()
  fireEvent(window, new Event('focus'))
  await waitFor(() => expect(getTopupInfo).toHaveBeenCalledTimes(2))
  expect(screen.getByTestId('legacy-wallet')).toBeInTheDocument()
  fireEvent(window, new Event('focus'))
  expect(await screen.findByTestId('lifecycle-wallet')).toBeInTheDocument()
  vi.mocked(getTopupInfo).mockRejectedValue(new Error('network'))
  fireEvent(window, new Event('focus'))
  await waitFor(() => expect(getTopupInfo).toHaveBeenCalledTimes(4))
  expect(screen.getByTestId('lifecycle-wallet')).toBeInTheDocument()
  expect(screen.queryByTestId('legacy-wallet')).not.toBeInTheDocument()
})
it('checks legacy readiness periodically and stops polling after activation', async () => {
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
  vi.mocked(getTopupInfo)
    .mockResolvedValueOnce({ success: true, data: info(false) })
    .mockResolvedValue({ success: true, data: info(true) })
  render(<Wallet />)
  expect(await screen.findByTestId('legacy-wallet')).toBeInTheDocument()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(30_000)
  })
  expect(await screen.findByTestId('lifecycle-wallet')).toBeInTheDocument()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60_000)
  })
  expect(getTopupInfo).toHaveBeenCalledTimes(2)
})
it('ignores an old readiness response that arrives after activation', async () => {
  let resolveOld:
    | ((value: Awaited<ReturnType<typeof getTopupInfo>>) => void)
    | undefined
  vi.mocked(getTopupInfo)
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveOld = resolve
        })
    )
    .mockResolvedValue({ success: true, data: info(true) })
  render(<Wallet />)
  await waitFor(() => expect(getTopupInfo).toHaveBeenCalledTimes(1))
  fireEvent(window, new Event('focus'))
  expect(await screen.findByTestId('lifecycle-wallet')).toBeInTheDocument()
  await act(async () => {
    resolveOld?.({ success: true, data: info(false) })
  })
  expect(screen.getByTestId('lifecycle-wallet')).toBeInTheDocument()
  expect(screen.queryByTestId('legacy-wallet')).not.toBeInTheDocument()
})
it('does not reuse a previous user readiness across an identity change', async () => {
  vi.mocked(getTopupInfo)
    .mockResolvedValueOnce({ success: true, data: info(true) })
    .mockRejectedValue(new Error('new identity unavailable'))
  const view = render(<Wallet />)
  expect(await screen.findByTestId('lifecycle-wallet')).toBeInTheDocument()
  identity.user = 2
  view.rerender(<Wallet />)
  expect(await screen.findByRole('alert')).toBeInTheDocument()
  expect(screen.queryByTestId('legacy-wallet')).not.toBeInTheDocument()
  expect(screen.queryByTestId('lifecycle-wallet')).not.toBeInTheDocument()
})
