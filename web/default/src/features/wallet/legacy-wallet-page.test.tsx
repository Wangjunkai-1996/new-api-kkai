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
import { beforeEach, expect, it, vi } from 'vitest'

import { LegacyWalletPage } from './legacy-wallet-page'
import type { CreemProduct, WalletPageProps } from './types'

const actions = vi.hoisted(() => ({
  quote: vi.fn(),
  payment: vi.fn(),
  creem: vi.fn(),
  redeem: vi.fn(),
  paymentStarted: vi.fn(),
  refreshUser: vi.fn(),
}))
const view = vi.hoisted(() => ({
  confirm: undefined as (() => Promise<void>) | undefined,
  confirmCreem: undefined as (() => Promise<void>) | undefined,
  redeem: undefined as (() => Promise<void>) | undefined,
}))
vi.mock('@/features/site-config', () => ({
  useSiteConfig: () => ({ data: { tenant_id: 12 } }),
}))
vi.mock('./hooks', () => ({
  usePayment: () => ({
    processing: false,
    calculatePaymentAmount: actions.quote,
    processPayment: actions.payment,
  }),
  useRedemption: () => ({ redeeming: false, redeemCode: actions.redeem }),
  useCreemPayment: () => ({
    processing: false,
    processCreemPayment: actions.creem,
  }),
  useWaffoPayment: () => ({ processing: false, processWaffoPayment: vi.fn() }),
  useWaffoPancakePayment: () => ({
    processing: false,
    processWaffoPancakePayment: vi.fn(),
  }),
}))
vi.mock('@/features/membership/components/wallet-summary', () => ({
  MembershipWalletSummary: () => null,
}))
vi.mock('./components/subscription-plans-card', () => ({
  SubscriptionPlansCard: () => null,
}))
vi.mock('./components/dialogs/legacy-billing-history-dialog', () => ({
  LegacyBillingHistoryDialog: () => null,
}))
vi.mock('./components/dialogs/payment-confirm-dialog', () => ({
  PaymentConfirmDialog: (props: {
    open: boolean
    onConfirm: () => Promise<void>
  }) => {
    view.confirm = props.onConfirm
    return props.open ? <p>Legacy confirmation</p> : null
  },
}))
vi.mock('./components/dialogs/creem-confirm-dialog', () => ({
  CreemConfirmDialog: (props: {
    open: boolean
    onConfirm: () => Promise<void>
  }) => {
    view.confirmCreem = props.onConfirm
    return props.open ? <p>Creem confirmation</p> : null
  },
}))
vi.mock('./components/recharge-form-card', () => ({
  RechargeFormCard: (props: {
    paymentQuoteValid: boolean
    onPaymentSubmit: () => void
    onCreemProductSelect: (product: CreemProduct) => void
    redemptionCode: string
    onRedemptionCodeChange: (value: string) => void
    onRedeem: () => Promise<void>
  }) => {
    view.redeem = props.onRedeem
    return (
      <>
        <button
          type='button'
          disabled={!props.paymentQuoteValid}
          onClick={props.onPaymentSubmit}
        >
          Pay
        </button>
        <button
          type='button'
          onClick={() =>
            props.onCreemProductSelect({
              name: 'Creem',
              productId: 'creem-1',
              price: 10,
              quota: 10,
              currency: 'USD',
            })
          }
        >
          Select Creem
        </button>
        <input
          aria-label='Redemption code'
          value={props.redemptionCode}
          onChange={(event) => props.onRedemptionCodeChange(event.target.value)}
        />
      </>
    )
  },
}))
function pageProps(): WalletPageProps {
  return {
    topupInfo: {
      enable_online_topup: true,
      enable_stripe_topup: false,
      pay_methods: [{ name: 'Alipay', type: 'alipay' }],
      min_topup: 1,
      stripe_min_topup: 1,
      amount_options: [10],
      discount: {},
    },
    presetAmounts: [{ value: 10 }],
    topupLoading: false,
    user: null,
    onRefreshUser: actions.refreshUser,
    historyOpen: false,
    historyUnfinished: false,
    onHistoryOpenChange: vi.fn(),
    writesEnabled: true,
    onLegacyPaymentStarted: actions.paymentStarted,
  }
}
beforeEach(() => {
  vi.clearAllMocks()
  actions.quote.mockResolvedValue(10)
  actions.payment.mockResolvedValue(false)
  actions.creem.mockResolvedValue(false)
  actions.redeem.mockResolvedValue(false)
})
it('guards already-open regular, Creem and redemption handlers after capability is suspended', async () => {
  const props = pageProps()
  const mounted = render(<LegacyWalletPage {...props} />)
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Pay' })).toBeEnabled()
  )
  fireEvent.click(screen.getByRole('button', { name: 'Pay' }))
  fireEvent.click(screen.getByRole('button', { name: 'Select Creem' }))
  fireEvent.change(screen.getByLabelText('Redemption code'), {
    target: { value: 'redeem-1' },
  })
  expect(screen.getByText('Legacy confirmation')).toBeVisible()
  expect(screen.getByText('Creem confirmation')).toBeVisible()
  mounted.rerender(<LegacyWalletPage {...props} writesEnabled={false} />)
  expect(screen.queryByText('Legacy confirmation')).not.toBeInTheDocument()
  expect(screen.queryByText('Creem confirmation')).not.toBeInTheDocument()
  await act(async () => {
    await view.confirm?.()
    await view.confirmCreem?.()
    await view.redeem?.()
  })
  expect(actions.payment).not.toHaveBeenCalled()
  expect(actions.creem).not.toHaveBeenCalled()
  expect(actions.redeem).not.toHaveBeenCalled()
  expect(actions.paymentStarted).not.toHaveBeenCalled()
})
it.each(['regular', 'creem'])(
  'latches the legacy mode before the %s request even when its result is unconfirmed',
  async (method) => {
    render(<LegacyWalletPage {...pageProps()} />)
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Pay' })).toBeEnabled()
    )
    if (method === 'regular') {
      fireEvent.click(screen.getByRole('button', { name: 'Pay' }))
      await act(async () => {
        await view.confirm?.()
      })
      expect(actions.payment).toHaveBeenCalledWith(10, 'alipay')
      expect(actions.paymentStarted.mock.invocationCallOrder[0]).toBeLessThan(
        actions.payment.mock.invocationCallOrder[0] ?? Infinity
      )
    } else {
      fireEvent.click(screen.getByRole('button', { name: 'Select Creem' }))
      await act(async () => {
        await view.confirmCreem?.()
      })
      expect(actions.creem).toHaveBeenCalledWith('creem-1')
      expect(actions.paymentStarted.mock.invocationCallOrder[0]).toBeLessThan(
        actions.creem.mock.invocationCallOrder[0] ?? Infinity
      )
    }
    expect(actions.paymentStarted).toHaveBeenCalledTimes(1)
    expect(actions.refreshUser).not.toHaveBeenCalled()
  }
)
