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
import { beforeEach, expect, it, vi } from 'vitest'

import {
  actOnWalletOrder,
  createWalletPayment,
  getWalletOrder,
  quoteWalletPayment,
  restoreWalletPayment,
  walletStateSchema,
} from './payment-flow-api'
import {
  paymentOrder,
  paymentQuote,
  paymentWallet,
} from './payment-flow-fixtures'

const api = vi.hoisted(() => ({ request: vi.fn() }))
vi.mock('@/lib/api', () => ({ api }))
beforeEach(() => vi.resetAllMocks())
it('accepts a legitimate idle wallet with purchases disabled', () => {
  expect(
    walletStateSchema.parse(
      paymentWallet(null, {
        can_create: false,
        create_block_reason_code: 'PAYMENT_DISABLED',
      })
    ).can_create
  ).toBe(false)
  expect(
    walletStateSchema.safeParse(
      paymentWallet(paymentOrder(), { can_create: true })
    ).success
  ).toBe(false)
})
it('preserves separate operation order and latest wallet on creation replay', async () => {
  const old = paymentOrder({ trade_no: 'OLD-ORDER' }),
    current = paymentOrder({ trade_no: 'CURRENT-ORDER' })
  api.request.mockResolvedValue({
    data: {
      success: true,
      data: { order: old, wallet: paymentWallet(current) },
    },
  })
  const result = await createWalletPayment(
    'signed-quote',
    '9',
    'original-idempotency-key'
  )
  expect(result.order.trade_no).toBe('OLD-ORDER')
  expect(result.wallet.flow.order?.trade_no).toBe('CURRENT-ORDER')
  expect(api.request).toHaveBeenCalledWith(
    expect.objectContaining({
      url: '/api/user/topup/orders',
      headers: { 'Idempotency-Key': 'original-idempotency-key' },
      data: { quote_token: 'signed-quote', expected_flow_revision: '9' },
    })
  )
})
it('rejects missing new lifecycle support without trying an old purchase endpoint', async () => {
  api.request.mockResolvedValue({
    data: { success: false, code: 'WALLET_STATE_UNAVAILABLE' },
  })
  await expect(restoreWalletPayment()).rejects.toThrow(
    'WALLET_STATE_UNAVAILABLE'
  )
  expect(api.request).toHaveBeenCalledTimes(1)
  expect(api.request).toHaveBeenCalledWith(
    expect.objectContaining({ url: '/api/user/topup/current/restore' })
  )
})
it('bounds wallet restore reads so a stalled recovery leaves the page retryable', async () => {
  const timeout = vi.spyOn(AbortSignal, 'timeout')
  try {
    api.request.mockResolvedValue({
      data: { success: true, data: paymentWallet() },
    })

    await restoreWalletPayment()

    expect(timeout).toHaveBeenCalledWith(20_000)
    expect(api.request).toHaveBeenCalledWith(
      expect.objectContaining({
        signal: expect.any(AbortSignal),
        url: '/api/user/topup/current/restore',
      })
    )
  } finally {
    timeout.mockRestore()
  }
})
it('does not allow an unknown legacy pricing snapshot to confirm a new quote', async () => {
  api.request.mockResolvedValue({
    data: {
      success: true,
      data: { ...paymentQuote(), pricing_mode: 'unknown' },
    },
  })
  await expect(
    quoteWalletPayment({
      payment_provider: 'epay',
      payment_method: 'alipay',
      amount: '10',
    })
  ).rejects.toThrow()
})
it('preserves only whitelisted administrator payment evidence without losing receipt ID precision', async () => {
  const order = paymentOrder({
    receipts: [
      {
        id: '9223372036854775807',
        payment: { amount_minor: '1000', currency: 'CNY', exponent: 2 },
        paid_at: 1_790_000_000,
        received_at: 1_790_000_010,
        verification_status: 'verified',
        processing_status: 'credited',
        exception_reason: null,
        review_owner: null,
        evidence_source: 'provider_query',
      },
    ],
  })
  api.request.mockResolvedValue({
    data: {
      success: true,
      data: {
        wallet: paymentWallet(order),
        order: {
          ...order,
          receipts: [
            {
              ...order.receipts?.[0],
              transaction_id: 'private-provider-id',
              checkout_url: 'https://private.example/token',
            },
          ],
        },
      },
    },
  })
  const result = await getWalletOrder(order.trade_no, true)
  expect(result.order.receipts).toEqual(order.receipts)
  expect(result.order.receipts?.[0]).not.toHaveProperty('checkout_url')
  expect(result.order.receipts?.[0]).not.toHaveProperty('transaction_id')
})
it('ends a top-up using its order version while preserving the latest separate wallet flow', async () => {
  const ended = paymentOrder({
    display_status: 'ended',
    status_group: 'closed',
    allowed_actions: ['contact_support'],
    local_end_reason: 'user_ended',
    local_ended_at: 1_790_000_010,
  })
  const latest = paymentWallet(paymentOrder({ trade_no: 'CURRENT-ORDER' }))
  api.request.mockResolvedValue({
    data: { success: true, data: { order: ended, wallet: latest } },
  })
  const result = await actOnWalletOrder(paymentOrder(), 'end')
  expect(api.request).toHaveBeenCalledWith(
    expect.objectContaining({
      url: '/api/user/topup/orders/ORDER-1/end',
      data: { expected_order_version: '1' },
    })
  )
  expect(result.order.display_status).toBe('ended')
  expect(result.order.local_end_reason).toBe('user_ended')
  expect(result.wallet.flow.order?.trade_no).toBe('CURRENT-ORDER')
})
