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
import axios from 'axios'
import { z } from 'zod'

import { api } from '@/lib/api'

import type {
  PaymentCheckout,
  PaymentHistory,
  PaymentOrder,
  PaymentOrderResult,
  PaymentQuote,
  PaymentQuoteRequest,
  PaymentStatusGroup,
  WalletState,
} from './payment-flow-types'

const decimal = z.string().regex(/^\d+$/)
const WALLET_RESTORE_TIMEOUT_MS = 20_000

function walletRestoreSignal(signal?: AbortSignal): AbortSignal {
  const timeout = AbortSignal.timeout(WALLET_RESTORE_TIMEOUT_MS)
  return signal ? AbortSignal.any([signal, timeout]) : timeout
}

const money = z.object({
  amount_minor: decimal,
  currency: z.string().regex(/^[A-Z]{3}$/),
  exponent: z.number().int().min(0).max(12),
})
const orderSchema = z.object({
  trade_no: z.string().min(1),
  version: decimal,
  display_status: z.enum([
    'creating',
    'pending',
    'payment_processing',
    'cancel_processing',
    'expiry_processing',
    'review_required',
    'credit_review',
    'success',
    'cancelled',
    'expired',
    'ended',
    'timed_out',
    'failed',
    'unavailable',
  ]),
  status_group: z.enum([
    'all',
    'pending',
    'processing',
    'review',
    'completed',
    'closed',
  ]),
  allowed_actions: z.array(
    z.enum([
      'resume',
      'cancel',
      'end',
      'reconcile',
      'contact_support',
      'acknowledge',
    ])
  ),
  expected_payment: money.nullable(),
  payable_payment: money.nullable(),
  pricing_mode: z.enum(['fixed', 'channel_adjusted', 'unknown']),
  pricing_notice_code: z.string().nullable(),
  credited_quota: decimal.nullable(),
  credit_display: z.object({ amount: z.string(), unit: z.string() }).nullable(),
  payment_provider: z.string(),
  payment_method: z.string(),
  created_at: z.number(),
  deadline_at: z.number().nullable(),
  deadline_source: z.enum(['provider', 'local_policy', 'unknown']),
  close_reason: z.enum(['user_cancel', 'timeout']).nullable(),
  closed_at: z.number().nullable(),
  local_end_reason: z.enum(['user_ended', 'timeout']).nullable().optional(),
  local_ended_at: z.number().nullable().optional(),
  completed_at: z.number().nullable(),
  last_checked_at: z.number().nullable(),
  reason_code: z.string().nullable(),
  received_payment: money.nullable(),
  review: z
    .object({
      reason_code: z.string(),
      owner_label: z.string(),
      last_checked_at: z.number().nullable(),
    })
    .nullable(),
  legacy_display: z
    .object({ amount: z.string().nullable(), money: z.string().nullable() })
    .nullable(),
  timeline: z
    .array(
      z.object({
        event_type: z.string(),
        occurred_at: z.number(),
        summary_code: z.string(),
      })
    )
    .optional(),
  user_id: z.number().optional(),
  review_owner: z.string().nullable().optional(),
  receipts: z
    .array(
      z.object({
        id: decimal,
        payment: money,
        paid_at: z.number().nullable(),
        received_at: z.number(),
        verification_status: z.string(),
        processing_status: z.string(),
        exception_reason: z.string().nullable(),
        review_owner: z.string().nullable(),
        evidence_source: z.string(),
      })
    )
    .optional(),
})
export const walletStateSchema = z
  .object({
    flow: z.object({
      id: z.string().nullable(),
      revision: decimal,
      phase: z.enum([
        'idle',
        'creating',
        'order',
        'processing',
        'review',
        'result',
      ]),
      order: orderSchema.nullable(),
    }),
    can_create: z.boolean(),
    create_block_reason_code: z.string().nullable(),
    other_unfinished_count: z.number().int().nonnegative(),
    server_time: z.number(),
  })
  .refine((state) =>
    state.flow.phase === 'idle'
      ? state.flow.order === null && state.flow.id === null
      : state.flow.order !== null && state.flow.id !== null && !state.can_create
  )

export class WalletPaymentError extends Error {
  constructor(
    public code: string,
    public wallet?: WalletState
  ) {
    super(code)
  }
}

// New lifecycle APIs deliberately never fall back to unguarded legacy /pay.
async function paymentRequest<T>(
  method: 'get' | 'post',
  path: string,
  body?: unknown,
  signal?: AbortSignal,
  headers?: Record<string, string>
): Promise<T> {
  let payload: { success?: boolean; code?: string; data?: T; message?: string }
  try {
    const response = await api.request({
      method,
      url: path,
      data: body,
      signal,
      headers,
      skipBusinessError: true,
    } as Parameters<typeof api.request>[0])
    payload = response.data
  } catch (error) {
    if (axios.isAxiosError(error) && error.response?.data) {
      const data = error.response.data
      const parsed = walletStateSchema.safeParse(data.data?.wallet)
      throw new WalletPaymentError(
        typeof data.code === 'string' ? data.code : 'WALLET_STATE_UNAVAILABLE',
        parsed.success ? parsed.data : undefined
      )
    }
    throw error
  }
  if (!payload?.success || payload.data === undefined) {
    const parsed = walletStateSchema.safeParse(
      (payload?.data as { wallet?: unknown } | undefined)?.wallet
    )
    throw new WalletPaymentError(
      payload?.code ?? 'WALLET_STATE_UNAVAILABLE',
      parsed.success ? parsed.data : undefined
    )
  }
  return payload.data
}
export async function restoreWalletPayment(
  tradeNo?: string,
  signal?: AbortSignal
) {
  return walletStateSchema.parse(
    await paymentRequest<WalletState>(
      'post',
      '/api/user/topup/current/restore',
      tradeNo ? { trade_no: tradeNo } : {},
      walletRestoreSignal(signal)
    )
  )
}
export async function getWalletPayment(signal?: AbortSignal) {
  return walletStateSchema.parse(
    await paymentRequest<WalletState>(
      'get',
      '/api/user/topup/current',
      undefined,
      signal
    )
  )
}
export async function quoteWalletPayment(
  request: PaymentQuoteRequest,
  signal?: AbortSignal
) {
  const quote = await paymentRequest<PaymentQuote>(
    'post',
    '/api/user/topup/quote',
    request,
    signal
  )
  const schema = orderSchema
    .pick({
      expected_payment: true,
      payable_payment: true,
      pricing_mode: true,
      pricing_notice_code: true,
      credited_quota: true,
      credit_display: true,
      payment_provider: true,
      payment_method: true,
    })
    .extend({
      quote_token: z.string().min(1),
      expires_at: z.number(),
      server_time: z.number(),
    })
  return schema
    .refine(
      (value) =>
        value.pricing_mode !== 'unknown' &&
        value.expected_payment !== null &&
        value.credited_quota !== null &&
        BigInt(value.credited_quota) > 0n
    )
    .parse(quote)
}
export async function createWalletPayment(
  quoteToken: string,
  revision: string,
  key: string
) {
  const result = await paymentRequest<PaymentOrderResult>(
    'post',
    '/api/user/topup/orders',
    { quote_token: quoteToken, expected_flow_revision: revision },
    undefined,
    { 'Idempotency-Key': key }
  )
  return {
    wallet: walletStateSchema.parse(result.wallet),
    order: orderSchema.parse(result.order),
  }
}
export async function resumeWalletPayment(
  order: PaymentOrder,
  wallet: WalletState
) {
  const result = await paymentRequest<{
    wallet: WalletState
    checkout: PaymentCheckout | null
  }>(
    'post',
    `/api/user/topup/orders/${encodeURIComponent(order.trade_no)}/resume`,
    { flow_id: wallet.flow.id, flow_revision: wallet.flow.revision }
  )
  const checkout = z
    .discriminatedUnion('kind', [
      z.object({ kind: z.literal('url'), url: z.string() }),
      z.object({
        kind: z.literal('form'),
        method: z.literal('POST'),
        action: z.string(),
        fields: z.record(z.string(), z.string()),
      }),
    ])
    .nullable()
    .parse(result.checkout)
  return { wallet: walletStateSchema.parse(result.wallet), checkout }
}
export async function actOnWalletOrder(
  order: PaymentOrder,
  action: 'cancel' | 'end' | 'reconcile' | 'admin-reconcile',
  reason?: string
) {
  let body: unknown = {}
  if (action === 'cancel' || action === 'end') {
    body = { expected_order_version: order.version }
  }
  if (action === 'admin-reconcile') body = { reason }
  const result = await paymentRequest<PaymentOrderResult>(
    'post',
    `/api/user/topup/orders/${encodeURIComponent(order.trade_no)}/${action}`,
    body
  )
  return {
    wallet: walletStateSchema.parse(result.wallet),
    order: orderSchema.parse(result.order),
  }
}
export async function acknowledgeWalletPayment(wallet: WalletState) {
  return walletStateSchema.parse(
    await paymentRequest<WalletState>(
      'post',
      '/api/user/topup/current/acknowledge',
      {
        flow_id: wallet.flow.id,
        flow_revision: wallet.flow.revision,
        trade_no: wallet.flow.order?.trade_no,
      }
    )
  )
}
export async function getWalletOrder(
  tradeNo: string,
  admin = false,
  signal?: AbortSignal
) {
  const result = await paymentRequest<{
    wallet: WalletState
    order: PaymentOrder
  }>(
    'get',
    `/api/user/topup/orders/${encodeURIComponent(tradeNo)}${admin ? '/admin' : ''}`,
    undefined,
    signal
  )
  return {
    wallet: walletStateSchema.parse(result.wallet),
    order: orderSchema.parse(result.order),
  }
}
export async function listWalletOrders(
  options: {
    admin: boolean
    page: number
    pageSize: number
    keyword: string
    exactTradeNo: string
    status: PaymentStatusGroup
    scope: 'wallet' | 'unfinished' | 'all'
  },
  signal?: AbortSignal
) {
  const params = new URLSearchParams({
    p: String(options.page),
    page_size: String(options.pageSize),
    status_group: options.status,
    scope: options.scope,
  })
  if (options.exactTradeNo) params.set('exact_trade_no', options.exactTradeNo)
  else if (options.keyword) params.set('keyword', options.keyword)
  const result = await paymentRequest<PaymentHistory>(
    'get',
    `/api/user/topup${options.admin ? '' : '/self'}?${params}`,
    undefined,
    signal
  )
  if (
    !Array.isArray(result.items) ||
    !Number.isSafeInteger(result.total) ||
    !result.counts
  ) {
    throw new WalletPaymentError('WALLET_STATE_UNAVAILABLE')
  }
  return {
    ...result,
    items: result.items.map((item) => orderSchema.parse(item)),
  }
}
