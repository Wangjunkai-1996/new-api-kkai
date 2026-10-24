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
import { act, fireEvent, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { expect, it, vi } from 'vitest'

import { LifecycleWalletPage as Wallet } from './lifecycle-wallet-page'
import { quoteWalletPayment } from './payment-flow-api'
import {
  paymentOrder,
  paymentQuote,
  paymentWallet,
} from './payment-flow-fixtures'
import type { PaymentQuote } from './payment-flow-types'
import type { CreemProduct, TopupInfo } from './types'

const site = vi.hoisted(() => ({ tenant_id: 0 }))
const flow = vi.hoisted(() => ({
  wallet: undefined as import('./payment-flow-types').WalletState | undefined,
  create: vi.fn(),
}))
const topup = vi.hoisted(() => ({
  info: null as import('./types').TopupInfo | null,
}))
vi.mock('./hooks/use-wallet-payment-flow', () => ({
  useWalletPaymentIdentity: () => ['test', 0, 11, 'session'],
  useWalletPaymentFlow: () => ({
    wallet: flow.wallet,
    loading: false,
    error: false,
    creating: false,
    busy: false,
    notice: '',
    apply: vi.fn(),
    restore: vi.fn(),
    refresh: vi.fn(),
    act: vi.fn(),
    resume: vi.fn(),
    acknowledge: vi.fn(),
    create: flow.create,
  }),
}))
vi.mock('./payment-flow-api', () => ({ quoteWalletPayment: vi.fn() }))
vi.mock('@/features/site-config', () => ({
  useSiteConfig: () => ({ data: site }),
}))
vi.mock('@/lib/api', () => ({
  getSelf: async () => ({ success: true, data: { quota: 10000 } }),
}))
vi.mock('@/components/layout', () => {
  const SectionPageLayout = (props: { children: ReactNode }) => (
    <div>{props.children}</div>
  )
  SectionPageLayout.Title = SectionPageLayout
  SectionPageLayout.Actions = SectionPageLayout
  SectionPageLayout.Content = SectionPageLayout
  return { SectionPageLayout }
})
vi.mock('@/features/membership/components/wallet-summary', () => ({
  MembershipWalletSummary: () => <p>Membership benefits</p>,
}))
vi.mock('./components/subscription-plans-card', () => ({
  SubscriptionPlansCard: () => <p>Subscription benefits</p>,
}))
vi.mock('./components/recharge-form-card', () => ({
  RechargeFormCard: (props: {
    topupInfo: TopupInfo | null
    topupAmount: number
    onTopupAmountChange: (amount: number) => void
    quote?: PaymentQuote
    paymentQuoteValid: boolean
    calculating: boolean
    onPaymentSubmit: () => void
    creemProducts?: CreemProduct[]
    onCreemProductSelect: (product: CreemProduct) => void
  }) => (
    <>
      <p>Customer topup</p>
      {props.topupInfo && (
        <>
          <input
            aria-label='Top-up amount'
            value={props.topupAmount}
            onChange={(event) =>
              props.onTopupAmountChange(Number(event.target.value))
            }
          />
          {props.quote && <p>{props.quote.quote_token}</p>}
          <button
            type='button'
            disabled={!props.paymentQuoteValid || props.calculating}
            onClick={props.onPaymentSubmit}
          >
            Review and Pay
          </button>
          {props.creemProducts?.map((product) => (
            <button
              key={product.productId}
              type='button'
              onClick={() => props.onCreemProductSelect(product)}
            >
              {product.name}
            </button>
          ))}
        </>
      )}
    </>
  ),
}))
vi.mock('./components/wallet-stats-card', () => ({
  WalletStatsCard: (props: { fundingSummaryEnabled?: boolean }) =>
    props.fundingSummaryEnabled ? (
      <p>Wallet funding statistics enabled</p>
    ) : null,
}))
vi.mock('./components/dialogs/billing-history-dialog', () => ({
  BillingHistoryDialog: (props: { open: boolean; unfinished?: boolean }) =>
    props.open ? (
      <p>{props.unfinished ? 'Unfinished history' : 'Wallet history'}</p>
    ) : null,
}))
vi.mock('./components/dialogs/creem-confirm-dialog', () => ({
  CreemConfirmDialog: () => null,
}))
vi.mock('./components/dialogs/payment-confirm-dialog', () => ({
  PaymentConfirmDialog: () => null,
}))
vi.mock('./hooks', () => ({
  usePayment: () => ({
    processing: false,
    calculatePaymentAmount: vi.fn(),
    processPayment: vi.fn(),
  }),
  useRedemption: () => ({ redeeming: false, redeemCode: vi.fn() }),
  useCreemPayment: () => ({ processing: false, processCreemPayment: vi.fn() }),
  useWaffoPayment: () => ({ processing: false, processWaffoPayment: vi.fn() }),
  useWaffoPancakePayment: () => ({
    processing: false,
    processWaffoPancakePayment: vi.fn(),
  }),
}))

it.each([0, 12])(
  'Host tenant %s offers topup and only platform Hosts mount subscription modules',
  async (tenantId) => {
    site.tenant_id = tenantId
    flow.wallet = paymentWallet()
    render(
      <QueryClientProvider client={new QueryClient()}>
        <Wallet
          topupInfo={topup.info}
          presetAmounts={[]}
          topupLoading={false}
        />
      </QueryClientProvider>
    )
    expect(await screen.findByText('Customer topup')).toBeVisible()
    expect(screen.getByText('Wallet funding statistics enabled')).toBeVisible()
    for (const label of ['Membership benefits', 'Subscription benefits']) {
      if (tenantId === 0) expect(screen.getByText(label)).toBeVisible()
      else expect(screen.queryByText(label)).not.toBeInTheDocument()
    }
  }
)

it('hides independent balance and purchase modules during a current order', () => {
  const order = paymentOrder({ allowed_actions: ['reconcile'] })
  flow.wallet = paymentWallet(order)
  render(
    <QueryClientProvider client={new QueryClient()}>
      <Wallet topupInfo={topup.info} presetAmounts={[]} topupLoading={false} />
    </QueryClientProvider>
  )
  expect(screen.queryByText('Customer topup')).not.toBeInTheDocument()
  expect(screen.queryByText('Subscription benefits')).not.toBeInTheDocument()
  expect(screen.queryByText('Membership benefits')).not.toBeInTheDocument()
  expect(
    screen.getByRole('heading', { name: 'Awaiting payment' })
  ).toBeVisible()
})
it('keeps independent idle capabilities visible when online purchase is disabled', () => {
  site.tenant_id = 0
  flow.wallet = paymentWallet(null, {
    can_create: false,
    create_block_reason_code: 'PAYMENT_DISABLED',
  })
  render(
    <QueryClientProvider client={new QueryClient()}>
      <Wallet topupInfo={topup.info} presetAmounts={[]} topupLoading={false} />
    </QueryClientProvider>
  )
  expect(screen.getByText('Customer topup')).toBeVisible()
  expect(screen.getByText('Subscription benefits')).toBeVisible()
  expect(screen.getByText('Online top-up is currently disabled.')).toBeVisible()
})
it('opens the other-unfinished entry separately from the normal wallet history', () => {
  flow.wallet = paymentWallet(paymentOrder(), { other_unfinished_count: 2 })
  render(
    <QueryClientProvider client={new QueryClient()}>
      <Wallet topupInfo={topup.info} presetAmounts={[]} topupLoading={false} />
    </QueryClientProvider>
  )
  fireEvent.click(
    screen.getByRole('button', { name: 'View other unfinished orders (2)' })
  )
  expect(screen.getByText('Unfinished history')).toBeVisible()
  fireEvent.click(
    screen.getAllByRole('button', { name: 'Order History' })[0] as HTMLElement
  )
  expect(screen.getByText('Wallet history')).toBeVisible()
})
it('debounces quote inputs and hides a previous amount until the final amount is quoted', async () => {
  vi.useFakeTimers()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const quoteMock = vi.mocked(quoteWalletPayment)
  quoteMock.mockReset().mockImplementation(async (request) => ({
    ...paymentQuote(),
    quote_token: `QUOTE-${request.amount}`,
  }))
  flow.create.mockReset()
  flow.wallet = paymentWallet()
  topup.info = {
    enable_online_topup: false,
    enable_stripe_topup: true,
    pay_methods: [],
    min_topup: 10,
    stripe_min_topup: 10,
    amount_options: [],
    discount: {},
  }
  const mounted = render(
    <QueryClientProvider client={client}>
      <Wallet topupInfo={topup.info} presetAmounts={[]} topupLoading={false} />
    </QueryClientProvider>
  )
  try {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(301)
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(quoteMock).toHaveBeenCalledTimes(1)
    expect(screen.getByText('QUOTE-10')).toBeVisible()
    fireEvent.change(screen.getByLabelText('Top-up amount'), {
      target: { value: '20' },
    })
    fireEvent.change(screen.getByLabelText('Top-up amount'), {
      target: { value: '200' },
    })
    expect(screen.queryByText('QUOTE-10')).not.toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Review and Pay' })
    ).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Review and Pay' }))
    expect(flow.create).not.toHaveBeenCalled()
    expect(quoteMock).toHaveBeenCalledTimes(1)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(301)
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(quoteMock).toHaveBeenCalledTimes(2)
    expect(quoteMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ amount: '200' }),
      expect.any(AbortSignal)
    )
    expect(screen.getByText('QUOTE-200')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Review and Pay' })).toBeEnabled()
    fireEvent.click(screen.getByRole('button', { name: 'Review and Pay' }))
    expect(flow.create).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ quote_token: 'QUOTE-200' })
    )
    expect(
      screen.queryByRole('heading', { name: 'Confirm top-up order' })
    ).not.toBeInTheDocument()
  } finally {
    mounted.unmount()
    client.clear()
    topup.info = null
    vi.useRealTimers()
  }
})

it.each(['epay', 'creem'])(
  '%s refreshes an expired quote without creating, then submits with one click',
  async (provider) => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const quoteMock = vi.mocked(quoteWalletPayment)
    quoteMock.mockReset().mockImplementation(async (request) => ({
      ...paymentQuote(),
      payment_provider: request.payment_provider,
      payment_method: request.payment_method,
      quote_token: `EXPIRED-${request.payment_provider}`,
      expires_at: 1,
    }))
    flow.create.mockReset()
    flow.wallet = paymentWallet()
    topup.info = {
      enable_online_topup: provider === 'epay',
      enable_stripe_topup: false,
      enable_creem_topup: provider === 'creem',
      pay_methods:
        provider === 'epay' ? [{ name: 'Alipay', type: 'alipay' }] : [],
      min_topup: 10,
      stripe_min_topup: 10,
      amount_options: [],
      discount: {},
      creem_products: [
        {
          name: 'Creem top-up',
          productId: 'product-1',
          price: 10,
          quota: 10,
          currency: 'USD',
        },
      ],
    }
    const mounted = render(
      <QueryClientProvider client={client}>
        <Wallet
          topupInfo={topup.info}
          presetAmounts={[]}
          topupLoading={false}
        />
      </QueryClientProvider>
    )
    try {
      if (provider === 'creem') {
        fireEvent.click(screen.getByRole('button', { name: 'Creem top-up' }))
      }
      await screen.findByText(
        provider === 'creem' ? '10 USD' : `EXPIRED-${provider}`
      )
      expect(flow.create).not.toHaveBeenCalled()
      const freshQuote = {
        ...paymentQuote(),
        payment_provider: provider,
        payment_method: provider === 'epay' ? 'alipay' : 'creem',
        quote_token: `FRESH-${provider}`,
        credit_display: { amount: '20', unit: 'USD' },
      }
      quoteMock.mockResolvedValue(freshQuote)
      const paymentButtons = () =>
        screen.getAllByRole('button', { name: 'Review and Pay' })
      fireEvent.click(paymentButtons()[paymentButtons().length - 1])
      expect(flow.create).not.toHaveBeenCalled()
      await screen.findByText(
        provider === 'creem' ? '20 USD' : `FRESH-${provider}`
      )
      fireEvent.click(paymentButtons()[paymentButtons().length - 1])
      expect(flow.create).toHaveBeenCalledExactlyOnceWith(freshQuote)
      expect(
        screen.queryByRole('heading', { name: 'Confirm top-up order' })
      ).not.toBeInTheDocument()
    } finally {
      mounted.unmount()
      client.clear()
      topup.info = null
    }
  }
)
