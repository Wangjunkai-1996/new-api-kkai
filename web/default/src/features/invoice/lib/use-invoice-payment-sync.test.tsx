import { webcrypto } from 'node:crypto'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { AxiosError } from 'axios'
import { StrictMode } from 'react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/http-client'
import { useAuthStore } from '@/stores/auth-store'

import * as invoiceApi from '../api'
import type { InvoiceOrder } from '../types'
import {
  cachePaymentSyncResult,
  paymentSyncCacheKey,
  paymentSyncCacheTTL,
  paymentSyncHash,
} from './payment-sync-cache'
import { useInvoicePaymentSync } from './use-invoice-payment-sync'

const clients: QueryClient[] = []
const originalAdapter = api.defaults.adapter

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
  localStorage.clear()
  vi.stubGlobal('crypto', webcrypto)
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
  api.defaults.adapter = originalAdapter
  vi.useRealTimers()
  vi.unstubAllGlobals()
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
  ['a service outage', { response: { status: 503 } }],
  ['a network failure', new Error('Network Error')],
  ['an authentication failure', { response: { status: 401 } }],
  [
    'a validation error',
    { response: { status: 400, data: { code: 'VALIDATION_FAILED' } } },
  ],
  [
    'an unknown conflict',
    { response: { status: 409, data: { code: 'PAYMENT_UNVERIFIED' } } },
  ],
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

test.each(['PAYMENT_VERIFICATION_FAILED', 'PAYMENT_EXCEPTION'])(
  'keeps a specific %s order failure separate from two successful verifications',
  async (code) => {
    const error = { response: { status: 409, data: { code } } }
    vi.mocked(invoiceApi.getInvoiceOrders).mockResolvedValue({
      items: [
        candidate('historical', { order_no: 'HISTORICAL-PAYMENT-123' }),
        candidate('first'),
        candidate('second'),
      ],
      p: 1,
      page_size: 100,
      total: 3,
    })
    vi.mocked(invoiceApi.verifyInvoicePayment).mockRejectedValueOnce(error)
    const hook = renderSync()
    await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))

    expect(hook.result.current.error).toBeNull()
    expect(hook.result.current.failures).toEqual([
      {
        order: {
          source_type: 'topup',
          order_id: 'historical',
          order_no: 'HISTORICAL-PAYMENT-123',
        },
        error,
      },
    ])
    expect(invoiceApi.verifyInvoicePayment).toHaveBeenCalledTimes(3)
    expect(hook.invalidate).toHaveBeenCalledTimes(1)
  }
)

test('resumes untouched orders after cooldown without spending the next window on cached failures', async () => {
  const now = Date.now()
  const clock = vi.spyOn(Date, 'now').mockReturnValue(now)
  vi.mocked(invoiceApi.getInvoiceOrders).mockResolvedValue({
    items: [candidate('first'), candidate('second')],
    p: 1,
    page_size: 100,
    total: 2,
  })
  const individualError = {
    response: { status: 409, data: { code: 'PAYMENT_VERIFICATION_FAILED' } },
  }
  const interruption = { response: { status: 429 } }
  vi.mocked(invoiceApi.verifyInvoicePayment)
    .mockRejectedValueOnce(individualError)
    .mockRejectedValueOnce(interruption)
  const hook = renderSync()
  await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))
  expect(hook.result.current.error).toBeNull()
  expect(hook.result.current.deferredUntil).toBe(now + paymentSyncCacheTTL)
  expect(hook.result.current.failures).toEqual([
    {
      order: { source_type: 'topup', order_id: 'first', order_no: 'first' },
      error: individualError,
    },
  ])

  await act(async () => {
    await hook.result.current.mutateAsync()
  })
  expect(invoiceApi.verifyInvoicePayment).toHaveBeenCalledTimes(2)
  clock.mockReturnValue(now + paymentSyncCacheTTL + 1000)
  await act(async () => {
    await hook.result.current.mutateAsync()
  })

  expect(
    vi
      .mocked(invoiceApi.verifyInvoicePayment)
      .mock.calls.map(([order]) => order.order_id)
  ).toEqual(['first', 'second', 'second'])
  expect(hook.result.current.failures[0]?.order.order_id).toBe('first')
  expect(hook.result.current.deferredUntil).toBeNull()
})

test('keeps previous order failures until a complete fresh snapshot proves they are gone', async () => {
  const error = {
    response: { status: 409, data: { code: 'PAYMENT_VERIFICATION_FAILED' } },
  }
  vi.mocked(invoiceApi.verifyInvoicePayment).mockRejectedValueOnce(error)
  const hook = renderSync()
  await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))
  const failure = hook.result.current.failures[0]
  expect(failure?.order.order_id).toBe('first')

  vi.mocked(invoiceApi.getInvoiceOrders)
    .mockResolvedValueOnce({
      items: [candidate('second')],
      p: 1,
      page_size: 100,
      total: 101,
    })
    .mockRejectedValueOnce({ response: { status: 503 } })
  act(() => hook.result.current.mutate())
  await waitFor(() => expect(hook.result.current.isError).toBe(true))
  expect(hook.result.current.failures).toEqual([failure])

  vi.mocked(invoiceApi.getInvoiceOrders).mockResolvedValue({
    items: [],
    p: 1,
    page_size: 100,
    total: 0,
  })
  act(() => hook.result.current.mutate())
  await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))
  expect(hook.result.current.failures).toEqual([])
  expect(invoiceApi.verifyInvoicePayment).toHaveBeenCalledTimes(1)
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

test('stops subsequent writes after unmount', async () => {
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

  hook.unmount()
  await act(async () => {
    pending.resolve()
    await pending.promise
  })

  expect(invoiceApi.verifyInvoicePayment).toHaveBeenCalledTimes(1)
  expect(hook.invalidate).not.toHaveBeenCalled()
})

test('starts the new account while stopping old writes and withholding its previous failures', async () => {
  const pending = pendingVerification()
  vi.mocked(invoiceApi.getInvoiceOrders).mockResolvedValue({
    items: [
      candidate('old-failure'),
      candidate('old-pending'),
      candidate('old-tail'),
    ],
    p: 1,
    page_size: 100,
    total: 3,
  })
  vi.mocked(invoiceApi.verifyInvoicePayment)
    .mockRejectedValueOnce({
      response: { status: 409, data: { code: 'PAYMENT_EXCEPTION' } },
    })
    .mockReturnValueOnce(pending.promise)
  const hook = renderSync()
  await waitFor(() =>
    expect(invoiceApi.verifyInvoicePayment).toHaveBeenCalledTimes(2)
  )
  vi.mocked(invoiceApi.getInvoiceOrders).mockResolvedValue({
    items: [candidate('new-account')],
    p: 1,
    page_size: 100,
    total: 1,
  })
  act(() =>
    useAuthStore.getState().auth.setUser({ id: 43, username: 'other', role: 1 })
  )
  expect(hook.result.current.failures).toEqual([])
  expect(hook.result.current.deferredUntil).toBeNull()
  await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))
  await act(async () => {
    pending.resolve()
    await pending.promise
  })

  expect(
    vi
      .mocked(invoiceApi.verifyInvoicePayment)
      .mock.calls.map(([order]) => order.order_id)
  ).toEqual(['old-failure', 'old-pending', 'new-account'])
  expect(hook.result.current.failures).toEqual([])
  expect(hook.invalidate).toHaveBeenCalledExactlyOnceWith({
    queryKey: invoiceApi.invoiceKey(43),
  })
})

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

test('reuses safe attempted results on remount while keeping only hashes, codes and expiry in storage', async () => {
  vi.mocked(invoiceApi.getInvoiceOrders).mockResolvedValue({
    items: [
      candidate('secret-ref', { order_no: 'PRIVATE-ORDER-999' }),
      candidate('paid'),
    ],
    p: 1,
    page_size: 100,
    total: 2,
  })
  vi.mocked(invoiceApi.verifyInvoicePayment).mockRejectedValueOnce({
    response: {
      status: 409,
      data: { code: 'PAYMENT_EXCEPTION', message: 'private provider details' },
    },
  })
  const firstVisit = renderSync()
  await waitFor(() => expect(firstVisit.result.current.isSuccess).toBe(true))
  firstVisit.unmount()
  const secondVisit = renderSync()
  await waitFor(() => expect(secondVisit.result.current.isSuccess).toBe(true))

  expect(invoiceApi.verifyInvoicePayment).toHaveBeenCalledTimes(2)
  expect(secondVisit.result.current.failures).toEqual([
    {
      order: {
        source_type: 'topup',
        order_id: 'secret-ref',
        order_no: 'PRIVATE-ORDER-999',
      },
      error: { status: 409, code: 'PAYMENT_EXCEPTION' },
    },
  ])
  expect(localStorage.length).toBe(1)
  const cacheKey = localStorage.key(0) || ''
  expect(cacheKey).toMatch(/^invoice-payment-sync:v1:[a-f0-9]{64}$/)
  const persisted = localStorage.getItem(cacheKey) || ''
  expect(persisted).not.toMatch(
    /secret-ref|PRIVATE-ORDER|private provider|order_no|amount|response|sid|token/
  )
  const cache = JSON.parse(persisted)
  expect(Object.keys(cache.attempts)).toEqual(
    expect.arrayContaining([expect.stringMatching(/^[a-f0-9]{64}$/)])
  )
  expect(Object.values(cache.attempts)).toEqual(
    expect.arrayContaining([
      { code: 'PAYMENT_EXCEPTION', expiresAt: expect.any(Number) },
      { code: 'verified', expiresAt: expect.any(Number) },
    ])
  )
})

test('treats a real Axios 409 as an order failure while real 503 remains a global error', async () => {
  vi.mocked(invoiceApi.getInvoiceOrders).mockRestore()
  vi.mocked(invoiceApi.verifyInvoicePayment).mockRestore()
  const requests: string[] = []
  let outage = false
  api.defaults.adapter = async (config) => {
    if (config.method === 'get') {
      return {
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
        data: {
          success: true,
          data: {
            items: [candidate('historical'), candidate('good')],
            p: 1,
            page_size: 100,
            total: 2,
          },
        },
      }
    }
    const order = JSON.parse(config.data as string)
    requests.push(order.order_id)
    if (outage || order.order_id === 'historical') {
      throw new AxiosError(
        'HTTP failure',
        'ERR_BAD_REQUEST',
        config,
        undefined,
        {
          status: outage ? 503 : 409,
          statusText: 'Failure',
          headers: {},
          config,
          data: {
            success: false,
            code: outage
              ? 'PAYMENT_VERIFICATION_UNAVAILABLE'
              : 'PAYMENT_VERIFICATION_FAILED',
          },
        }
      )
    }
    return {
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
      data: { success: true, data: { verified: true } },
    }
  }
  const hook = renderSync()
  await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))
  expect(requests).toEqual(['historical', 'good'])
  expect(hook.result.current.failures[0]?.order.order_no).toBe('historical')
  expect(hook.result.current.error).toBeNull()

  localStorage.clear()
  outage = true
  act(() => hook.result.current.mutate())
  await waitFor(() => expect(hook.result.current.isError).toBe(true))
  expect(hook.result.current.deferredUntil).toBeNull()
  expect(hook.result.current.error).toMatchObject({ response: { status: 503 } })
})

test('a real bare Axios 429 survives remount and explicit retry, then resumes automatically after the legacy cooldown', async () => {
  vi.useFakeTimers()
  vi.mocked(invoiceApi.verifyInvoicePayment).mockRestore()
  let attempts = 0
  api.defaults.adapter = async (config) => {
    attempts++
    if (attempts === 1) {
      throw new AxiosError(
        'Too many requests',
        'ERR_BAD_REQUEST',
        config,
        undefined,
        {
          status: 429,
          statusText: 'Too Many Requests',
          headers: {},
          config,
          data: '',
        }
      )
    }
    return {
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
      data: { success: true, data: { verified: true } },
    }
  }
  const firstVisit = renderSync()
  await vi.waitFor(() => expect(firstVisit.result.current.isSuccess).toBe(true))
  const until = firstVisit.result.current.deferredUntil
  expect(until).toBeGreaterThan(Date.now())
  expect(firstVisit.result.current.error).toBeNull()
  firstVisit.unmount()

  const secondVisit = renderSync()
  await vi.waitFor(() =>
    expect(secondVisit.result.current.isSuccess).toBe(true)
  )
  expect(secondVisit.result.current.deferredUntil).toBe(until)
  act(() => secondVisit.result.current.mutate())
  await vi.waitFor(() =>
    expect(secondVisit.result.current.isSuccess).toBe(true)
  )
  expect(attempts).toBe(1)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(paymentSyncCacheTTL)
  })
  await vi.waitFor(() => expect(attempts).toBe(2))
  await vi.waitFor(() =>
    expect(secondVisit.result.current.isSuccess).toBe(true)
  )
  expect(secondVisit.result.current.deferredUntil).toBeNull()
})

test('serializes same-account visits with Web Locks and reuses the first visit result', async () => {
  let tail = Promise.resolve()
  const request = vi.fn((_name: string, callback: () => Promise<void>) => {
    const current = (async () => {
      await tail
      return callback()
    })()
    tail = current.catch(() => undefined)
    return current
  })
  vi.stubGlobal('navigator', { locks: { request } })
  const pending = pendingVerification()
  vi.mocked(invoiceApi.verifyInvoicePayment).mockReturnValueOnce(
    pending.promise
  )
  const firstVisit = renderSync()
  await waitFor(() =>
    expect(invoiceApi.verifyInvoicePayment).toHaveBeenCalledTimes(1)
  )
  const secondVisit = renderSync()
  await waitFor(() => expect(request).toHaveBeenCalledTimes(2))
  await act(async () => {
    pending.resolve()
    await pending.promise
  })
  await waitFor(() => expect(firstVisit.result.current.isSuccess).toBe(true))
  await waitFor(() => expect(secondVisit.result.current.isSuccess).toBe(true))
  expect(invoiceApi.verifyInvoicePayment).toHaveBeenCalledTimes(1)
  expect(secondVisit.invalidate).toHaveBeenCalledWith({
    queryKey: invoiceApi.invoiceKey(42),
  })
})

test('restores a failure another tab cached while the order snapshot was loading without Web Locks', async () => {
  vi.mocked(invoiceApi.getInvoiceOrders).mockImplementationOnce(async () => {
    const host = window.location.host
    const accountKey = paymentSyncCacheKey(
      await paymentSyncHash(JSON.stringify([host, 42]))
    )
    const orderHash = await paymentSyncHash(
      JSON.stringify([host, 42, 'topup', 'first'])
    )
    cachePaymentSyncResult(accountKey, orderHash, 'PAYMENT_EXCEPTION')
    return { items: [candidate('first')], p: 1, page_size: 100, total: 1 }
  })
  const hook = renderSync()
  await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))
  expect(invoiceApi.verifyInvoicePayment).not.toHaveBeenCalled()
  expect(hook.result.current.failures).toEqual([
    {
      order: { source_type: 'topup', order_id: 'first', order_no: 'first' },
      error: { status: 409, code: 'PAYMENT_EXCEPTION' },
    },
  ])
})
