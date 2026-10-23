import { AxiosHeaders } from 'axios'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import {
  cachePaymentSyncResult,
  deferPaymentSync,
  paymentSyncCacheTTL,
  readPaymentSyncCache,
} from './payment-sync-cache'

const now = Date.parse('2026-10-24T00:00:00Z')
const orderHash = 'a'.repeat(64)

beforeEach(() => {
  localStorage.clear()
  vi.spyOn(Date, 'now').mockReturnValue(now)
})

afterEach(() => vi.restoreAllMocks())

test.each([
  ['numeric Axios header', new AxiosHeaders({ 'Retry-After': '120' }), 120_000],
  [
    'HTTP date',
    { 'rEtRy-AfTeR': new Date(now + 180_000).toUTCString() },
    180_000,
  ],
  ['missing header', {}, paymentSyncCacheTTL],
  ['invalid header', { 'Retry-After': 'later' }, paymentSyncCacheTTL],
  [
    'expired date',
    { 'Retry-After': new Date(now - 1000).toUTCString() },
    paymentSyncCacheTTL,
  ],
  ['zero delay', { 'Retry-After': '0' }, paymentSyncCacheTTL],
  ['fractional delay', { 'Retry-After': '0.1' }, 1000],
  ['excessive delay', { 'Retry-After': '9999999999' }, 24 * 60 * 60 * 1000],
])('honors %s without a retry loop', (name, headers, delay) => {
  const key = `retry-${name}`
  const until = deferPaymentSync(key, { response: { status: 429, headers } })
  expect(until).toBe(now + delay)
  expect(readPaymentSyncCache(key).deferredUntil).toBe(until)
})

test('keeps earlier business failures skipped while delayed untouched orders resume', () => {
  const key = 'failure-expiry'
  cachePaymentSyncResult(key, orderHash, 'PAYMENT_VERIFICATION_FAILED')
  const until = deferPaymentSync(key, {
    response: { status: 429, headers: {} },
  })
  vi.mocked(Date.now).mockReturnValue(until)
  expect(readPaymentSyncCache(key)).toEqual({
    deferredUntil: 0,
    attempts: {
      [orderHash]: {
        code: 'PAYMENT_VERIFICATION_FAILED',
        expiresAt: until + paymentSyncCacheTTL,
      },
    },
  })
  vi.mocked(Date.now).mockReturnValue(until + paymentSyncCacheTTL)
  expect(readPaymentSyncCache(key)).toEqual({ deferredUntil: 0, attempts: {} })
})

test('retains cooldown and results in memory when persistent storage cannot be written', () => {
  const key = 'storage-quota-failure'
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new DOMException('Quota exceeded', 'QuotaExceededError')
  })
  cachePaymentSyncResult(key, orderHash, 'verified')
  const until = deferPaymentSync(key, {
    response: { status: 429, headers: {} },
  })
  expect(readPaymentSyncCache(key)).toEqual({
    deferredUntil: until,
    attempts: {
      [orderHash]: { code: 'verified', expiresAt: now + paymentSyncCacheTTL },
    },
  })
})

test('drops untrusted references, codes and expired results when reading persistent data', () => {
  const key = 'untrusted-cache'
  localStorage.setItem(
    key,
    JSON.stringify({
      deferredUntil: now - 1,
      attempts: {
        'private-order-123': { code: 'verified', expiresAt: now + 1000 },
        [orderHash]: {
          code: 'PAYMENT_VERIFICATION_FAILED',
          expiresAt: now - 1,
        },
        ['b'.repeat(64)]: { code: 'unknown', expiresAt: now + 1000 },
        ['c'.repeat(64)]: {
          code: 'verified',
          expiresAt: now + 1000,
          secret: 'discard',
        },
      },
      secret: 'discard',
    })
  )
  expect(readPaymentSyncCache(key)).toEqual({
    deferredUntil: 0,
    attempts: { ['c'.repeat(64)]: { code: 'verified', expiresAt: now + 1000 } },
  })
})
