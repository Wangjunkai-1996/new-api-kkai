import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { StrictMode } from 'react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import * as invoiceApi from '../api'
import type { InvoiceOrder } from '../types'
import { useInvoicePaymentSync } from './use-invoice-payment-sync'

const clients: QueryClient[] = []

function candidate(
  id: string,
  overrides: Partial<InvoiceOrder> = {}
): InvoiceOrder {
  return {
    source_type: 'topup',
    order_id: id,
    order_no: id,
    paid_at: '2026-10-22T00:00:00Z',
    payment_label: 'epay',
    amount_minor: null,
    currency: 'CNY',
    currency_exponent: 2,
    eligible: false,
    reason_codes: ['PAYMENT_UNVERIFIED'],
    merge_group_id: 'seller_cny',
    allowed_invoice_types: ['ordinary'],
    allowed_actions: ['verify_payment'],
    ...overrides,
  }
}

function pendingVerification() {
  let finish: (value: { verified: boolean }) => void = () => undefined
  const promise = new Promise<{ verified: boolean }>((resolve) => {
    finish = resolve
  })
  return { promise, resolve: () => finish({ verified: true }) }
}

function renderSync(enabled = true, strict = false) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  const invalidate = vi.spyOn(client, 'invalidateQueries')
  const hook = renderHook(({ enabled }) => useInvoicePaymentSync(enabled), {
    initialProps: { enabled },
    wrapper: ({ children }) => (
      <QueryClientProvider client={client}>
        {strict ? <StrictMode>{children}</StrictMode> : children}
      </QueryClientProvider>
    ),
  })
  return { ...hook, invalidate }
}

beforeEach(() => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 42, username: 'customer', role: 1 })
  vi.spyOn(invoiceApi, 'getInvoiceOrders').mockResolvedValue({
    items: [candidate('first')],
    p: 1,
    page_size: 100,
    total: 1,
  })
  vi.spyOn(invoiceApi, 'verifyInvoicePayment').mockResolvedValue({
    verified: true,
  })
})

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  useAuthStore.getState().auth.reset()
})

test('waits for the policy to enable payment synchronization', async () => {
  const hook = renderSync(false)
  expect(invoiceApi.getInvoiceOrders).not.toHaveBeenCalled()
  expect(invoiceApi.verifyInvoicePayment).not.toHaveBeenCalled()

  hook.rerender({ enabled: true })
  await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))
  hook.rerender({ enabled: true })
  expect(invoiceApi.verifyInvoicePayment).toHaveBeenCalledTimes(1)
})

test('synchronizes once under StrictMode and refreshes invoice queries', async () => {
  const hook = renderSync(true, true)
  await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))

  expect(invoiceApi.getInvoiceOrders).toHaveBeenCalledTimes(1)
  expect(invoiceApi.verifyInvoicePayment).toHaveBeenCalledExactlyOnceWith(
    { source_type: 'topup', order_id: 'first' },
    expect.any(String)
  )
  expect(hook.invalidate).toHaveBeenCalledExactlyOnceWith({
    queryKey: invoiceApi.invoiceKey(42),
  })
})

test('snapshots every page before writes, deduplicates orders and respects allowed actions', async () => {
  const events: string[] = []
  vi.mocked(invoiceApi.getInvoiceOrders).mockImplementation(
    async ({ p = 1 }) => {
      events.push(`read:${p}`)
      const items =
        p === 1
          ? [candidate('first'), candidate('blocked', { allowed_actions: [] })]
          : [
              candidate('first'),
              candidate('second'),
              candidate('member', { source_type: 'membership' }),
            ]
      return { items, p, page_size: 100, total: 101 }
    }
  )
  vi.mocked(invoiceApi.verifyInvoicePayment).mockImplementation(
    async (order) => {
      events.push(`verify:${order.order_id}`)
      return { verified: true }
    }
  )
  const hook = renderSync()
  await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))

  expect(events).toEqual(['read:1', 'read:2', 'verify:first', 'verify:second'])
  expect(invoiceApi.getInvoiceOrders).toHaveBeenNthCalledWith(1, {
    eligibility: 'unavailable',
    source_type: 'topup',
    p: 1,
    page_size: 100,
  })
  expect(invoiceApi.verifyInvoicePayment).toHaveBeenNthCalledWith(
    2,
    { source_type: 'topup', order_id: 'second' },
    expect.any(String)
  )
  expect(hook.invalidate).toHaveBeenCalledTimes(1)
})

test.each([
  ['rate limiting', { response: { status: 429 } }],
  ['a service outage', { response: { status: 503 } }],
  ['a network failure', new Error('Network Error')],
])(
  'stops the batch after %s without automatic retries',
  async (_name, error) => {
    vi.mocked(invoiceApi.getInvoiceOrders).mockResolvedValue({
      items: [candidate('first'), candidate('second')],
      p: 1,
      page_size: 100,
      total: 2,
    })
    vi.mocked(invoiceApi.verifyInvoicePayment).mockRejectedValue(error)
    const hook = renderSync()
    await waitFor(() => expect(hook.result.current.isError).toBe(true))
    hook.rerender({ enabled: true })

    expect(hook.result.current.error).toBe(error)
    expect(invoiceApi.verifyInvoicePayment).toHaveBeenCalledTimes(1)
    expect(hook.invalidate).toHaveBeenCalledTimes(1)
  }
)

test('continues after an individual business failure and refreshes successful changes', async () => {
  const error = {
    response: { status: 409, data: { code: 'PAYMENT_UNVERIFIED' } },
  }
  vi.mocked(invoiceApi.getInvoiceOrders).mockResolvedValue({
    items: [candidate('first'), candidate('second')],
    p: 1,
    page_size: 100,
    total: 2,
  })
  vi.mocked(invoiceApi.verifyInvoicePayment).mockRejectedValueOnce(error)
  const hook = renderSync()
  await waitFor(() => expect(hook.result.current.isError).toBe(true))

  expect(hook.result.current.error).toBe(error)
  expect(invoiceApi.verifyInvoicePayment).toHaveBeenCalledTimes(2)
  expect(hook.invalidate).toHaveBeenCalledTimes(1)
})

test('retries interrupted orders before earlier individual business failures', async () => {
  vi.mocked(invoiceApi.getInvoiceOrders).mockResolvedValue({
    items: [candidate('first'), candidate('second')],
    p: 1,
    page_size: 100,
    total: 2,
  })
  vi.mocked(invoiceApi.verifyInvoicePayment)
    .mockRejectedValueOnce({ response: { status: 409 } })
    .mockRejectedValueOnce({ response: { status: 429 } })
  const hook = renderSync()
  await waitFor(() => expect(hook.result.current.isError).toBe(true))

  act(() => hook.result.current.mutate())
  await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))

  expect(
    vi
      .mocked(invoiceApi.verifyInvoicePayment)
      .mock.calls.map(([order]) => order.order_id)
  ).toEqual(['first', 'second', 'second', 'first'])
  expect(hook.invalidate).toHaveBeenCalledTimes(2)
})

test('does not write a partial snapshot when a later page fails', async () => {
  vi.mocked(invoiceApi.getInvoiceOrders)
    .mockResolvedValueOnce({
      items: [candidate('first')],
      p: 1,
      page_size: 100,
      total: 101,
    })
    .mockRejectedValueOnce({ response: { status: 503 } })
  const hook = renderSync()
  await waitFor(() => expect(hook.result.current.isError).toBe(true))

  expect(invoiceApi.verifyInvoicePayment).not.toHaveBeenCalled()
  expect(hook.invalidate).not.toHaveBeenCalled()
})

test.each(['unmount', 'account change'] as const)(
  'stops subsequent writes after %s',
  async (exit) => {
    const pending = pendingVerification()
    vi.mocked(invoiceApi.getInvoiceOrders).mockResolvedValue({
      items: [candidate('first'), candidate('second')],
      p: 1,
      page_size: 100,
      total: 2,
    })
    vi.mocked(invoiceApi.verifyInvoicePayment).mockReturnValueOnce(
      pending.promise
    )
    const hook = renderSync()
    await waitFor(() =>
      expect(invoiceApi.verifyInvoicePayment).toHaveBeenCalledTimes(1)
    )

    if (exit === 'unmount') {
      hook.unmount()
    } else {
      act(() =>
        useAuthStore
          .getState()
          .auth.setUser({ id: 43, username: 'other', role: 1 })
      )
    }
    await act(async () => {
      pending.resolve()
      await pending.promise
    })

    expect(invoiceApi.verifyInvoicePayment).toHaveBeenCalledTimes(1)
    expect(hook.invalidate).not.toHaveBeenCalled()
  }
)

test('shares an in-flight verification when the invoice workspace is reopened', async () => {
  const pending = pendingVerification()
  vi.mocked(invoiceApi.verifyInvoicePayment).mockReturnValueOnce(
    pending.promise
  )
  const firstVisit = renderSync()
  await waitFor(() =>
    expect(invoiceApi.verifyInvoicePayment).toHaveBeenCalledTimes(1)
  )
  firstVisit.unmount()

  const secondVisit = renderSync()
  await waitFor(() =>
    expect(invoiceApi.getInvoiceOrders).toHaveBeenCalledTimes(2)
  )
  await act(async () => {
    pending.resolve()
    await pending.promise
  })
  await waitFor(() => expect(secondVisit.result.current.isSuccess).toBe(true))

  expect(invoiceApi.verifyInvoicePayment).toHaveBeenCalledTimes(1)
  expect(firstVisit.invalidate).not.toHaveBeenCalled()
  expect(secondVisit.invalidate).toHaveBeenCalledExactlyOnceWith({
    queryKey: invoiceApi.invoiceKey(42),
  })
})
