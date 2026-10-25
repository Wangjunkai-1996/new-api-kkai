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

import {
  paySubscriptionBalance,
  paySubscriptionCreem,
  paySubscriptionEpay,
  paySubscriptionStripe,
  paySubscriptionWaffoPancake,
} from '../../api'
import type { PlanRecord } from '../../types'
import { SubscriptionPurchaseDialog } from './subscription-purchase-dialog'

vi.mock('@/hooks/use-group-display-names', () => ({
  useGroupDisplayNames: () => ({}),
}))
vi.mock('@/hooks/use-system-config', () => ({
  useSystemConfig: () => ({ currency: { quotaPerUnit: 1 } }),
}))
vi.mock('../../api', () => ({
  paySubscriptionBalance: vi.fn(),
  paySubscriptionCreem: vi.fn(),
  paySubscriptionEpay: vi.fn(),
  paySubscriptionStripe: vi.fn(),
  paySubscriptionWaffoPancake: vi.fn(),
}))
const plan: PlanRecord = {
  plan: {
    id: 1,
    title: 'Test plan',
    price_amount: 10,
    currency: 'USD',
    duration_unit: 'month',
    duration_value: 1,
    quota_reset_period: 'never',
    enabled: true,
    sort_order: 0,
    allow_balance_pay: true,
    allow_wallet_overflow: true,
    max_purchase_per_user: 0,
    total_amount: 100,
    stripe_price_id: 'test',
    creem_product_id: 'test',
    waffo_pancake_product_id: 'test',
  },
}
beforeEach(() => vi.clearAllMocks())

it('keeps an open purchase dialog readable and blocks every payment method after writes pause', () => {
  const onPurchaseStarted = vi.fn()
  const view = (writesEnabled: boolean) => (
    <SubscriptionPurchaseDialog
      open
      onOpenChange={vi.fn()}
      plan={plan}
      userQuota={100}
      enableStripe
      enableCreem
      enableWaffoPancake
      enableOnlineTopUp
      epayMethods={[{ type: 'alipay', name: 'Alipay' }]}
      writesEnabled={writesEnabled}
      onPurchaseStarted={onPurchaseStarted}
    />
  )
  const rendered = render(view(true))
  expect(screen.getByRole('button', { name: 'Pay with Balance' })).toBeEnabled()
  rendered.rerender(view(false))
  expect(screen.getByText('Test plan')).toBeVisible()
  for (const name of [
    'Pay with Balance',
    'Stripe',
    'Creem',
    'Waffo Pancake',
    'Pay',
  ]) {
    const button = screen.getByRole('button', { name })
    expect(button).toBeDisabled()
    fireEvent.click(button)
  }
  for (const request of [
    paySubscriptionBalance,
    paySubscriptionCreem,
    paySubscriptionEpay,
    paySubscriptionStripe,
    paySubscriptionWaffoPancake,
  ]) {
    expect(request).not.toHaveBeenCalled()
  }
  expect(onPurchaseStarted).not.toHaveBeenCalled()
})

it('marks a purchase as started before sending the request even when its outcome is unknown', async () => {
  const onPurchaseStarted = vi.fn()
  vi.mocked(paySubscriptionBalance).mockImplementation(async () => {
    expect(onPurchaseStarted).toHaveBeenCalledTimes(1)
    throw new Error('response unavailable')
  })
  render(
    <SubscriptionPurchaseDialog
      open
      onOpenChange={vi.fn()}
      plan={plan}
      userQuota={100}
      onPurchaseStarted={onPurchaseStarted}
    />
  )
  fireEvent.click(screen.getByRole('button', { name: 'Pay with Balance' }))
  await waitFor(() =>
    expect(paySubscriptionBalance).toHaveBeenCalledWith({ plan_id: 1 })
  )
  expect(onPurchaseStarted).toHaveBeenCalledTimes(1)
})

it('does not open a checkout returned after payment capability pauses or resend that request', async () => {
  let resolve: (
    value: Awaited<ReturnType<typeof paySubscriptionStripe>>
  ) => void
  const pending = new Promise<
    Awaited<ReturnType<typeof paySubscriptionStripe>>
  >((done) => {
    resolve = done
  })
  vi.mocked(paySubscriptionStripe).mockReturnValue(pending)
  const open = vi.spyOn(window, 'open').mockImplementation(() => null)
  const view = (writesEnabled: boolean) => (
    <SubscriptionPurchaseDialog
      open
      onOpenChange={vi.fn()}
      plan={plan}
      userQuota={100}
      enableStripe
      writesEnabled={writesEnabled}
    />
  )
  const rendered = render(view(true))
  fireEvent.click(screen.getByRole('button', { name: 'Stripe' }))
  rendered.rerender(view(false))
  await act(async () =>
    resolve({
      success: true,
      message: 'success',
      data: { pay_link: 'https://example.test/checkout' },
    })
  )
  expect(open).not.toHaveBeenCalled()
  expect(
    screen.getByText(
      'The payment request was received, but payment availability changed. Check Order History before trying again.'
    )
  ).toBeVisible()
  rendered.rerender(view(true))
  expect(screen.getByRole('button', { name: 'Stripe' })).toBeDisabled()
  expect(paySubscriptionStripe).toHaveBeenCalledTimes(1)
})
