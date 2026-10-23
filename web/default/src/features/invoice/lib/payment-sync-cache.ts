import {
  getServerErrorSources,
  getServerErrorStatus,
} from '@/lib/server-error-message'

export const paymentSyncCacheTTL = 20 * 60 * 1000
const maxCooldown = 24 * 60 * 60 * 1000
const cachePrefix = 'invoice-payment-sync:v1:'

type PaymentFailureCode = 'PAYMENT_VERIFICATION_FAILED' | 'PAYMENT_EXCEPTION'
type PaymentResultCode = PaymentFailureCode | 'verified'
type PaymentSyncCache = {
  deferredUntil: number
  attempts: Record<string, { code: PaymentResultCode; expiresAt: number }>
}

const memory = new Map<string, PaymentSyncCache>()
const memoryOnly = new Set<string>()

export async function paymentSyncHash(value: string): Promise<string> {
  const digest = await crypto.subtle.digest(
    'SHA-256',
    new TextEncoder().encode(value)
  )
  return [...new Uint8Array(digest)]
    .map((byte) => byte.toString(16).padStart(2, '0'))
    .join('')
}

export function paymentSyncCacheKey(accountHash: string): string {
  return `${cachePrefix}${accountHash}`
}

export function paymentFailureCode(error: unknown): PaymentFailureCode | null {
  if (getServerErrorStatus(error) !== 409) return null
  for (const source of getServerErrorSources(error)) {
    if (
      source.code === 'PAYMENT_VERIFICATION_FAILED' ||
      source.code === 'PAYMENT_EXCEPTION'
    ) {
      return source.code
    }
  }
  return null
}

export function readPaymentSyncCache(key: string): PaymentSyncCache {
  let stored: unknown = memory.get(key)
  if (!memoryOnly.has(key)) {
    try {
      const value = localStorage.getItem(key)
      stored = value ? JSON.parse(value) : undefined
    } catch {
      // Storage may be disabled; the current document still shares results.
      memoryOnly.add(key)
    }
  }
  const cache: PaymentSyncCache = { deferredUntil: 0, attempts: {} }
  const now = Date.now()
  if (stored && typeof stored === 'object') {
    const value = stored as Partial<PaymentSyncCache>
    if (
      typeof value.deferredUntil === 'number' &&
      Number.isFinite(value.deferredUntil) &&
      value.deferredUntil > now &&
      value.deferredUntil <= now + maxCooldown
    ) {
      cache.deferredUntil = value.deferredUntil
    }
    if (value.attempts && typeof value.attempts === 'object') {
      for (const [hash, attempt] of Object.entries(value.attempts)) {
        if (
          !/^[a-f0-9]{64}$/.test(hash) ||
          !attempt ||
          typeof attempt !== 'object'
        ) {
          continue
        }
        const { code, expiresAt } = attempt
        if (
          code !== 'verified' &&
          code !== 'PAYMENT_VERIFICATION_FAILED' &&
          code !== 'PAYMENT_EXCEPTION'
        ) {
          continue
        }
        if (
          !Number.isFinite(expiresAt) ||
          expiresAt <= now ||
          expiresAt > now + maxCooldown + paymentSyncCacheTTL
        ) {
          continue
        }
        cache.attempts[hash] = { code, expiresAt }
      }
    }
  }
  memory.set(key, cache)
  return cache
}

function savePaymentSyncCache(key: string, cache: PaymentSyncCache): void {
  memory.set(key, cache)
  try {
    if (!cache.deferredUntil && !Object.keys(cache.attempts).length) {
      localStorage.removeItem(key)
    } else {
      localStorage.setItem(key, JSON.stringify(cache))
    }
  } catch {
    // A failed persistent write must not lose this document's cooldown.
    memoryOnly.add(key)
  }
}

export function cachePaymentSyncResult(
  key: string,
  hash: string,
  code: PaymentResultCode
): void {
  const cache = readPaymentSyncCache(key)
  cache.attempts[hash] = { code, expiresAt: Date.now() + paymentSyncCacheTTL }
  savePaymentSyncCache(key, cache)
}

export function deferPaymentSync(key: string, error: unknown): number {
  const now = Date.now()
  let retryAfter: unknown
  for (const source of getServerErrorSources(error)) {
    const headers = (source.response as { headers?: unknown } | undefined)
      ?.headers
    if (!headers || typeof headers !== 'object') continue
    const values = headers as Record<string, unknown> & {
      get?: (name: string) => unknown
    }
    retryAfter =
      typeof values.get === 'function'
        ? values.get('retry-after')
        : Object.entries(values).find(
            ([name]) => name.toLowerCase() === 'retry-after'
          )?.[1]
    if (retryAfter != null) break
  }
  const raw =
    typeof retryAfter === 'string' || typeof retryAfter === 'number'
      ? String(retryAfter).trim()
      : ''
  const target = /^\d+(?:\.\d+)?$/.test(raw)
    ? now + Number(raw) * 1000
    : Date.parse(raw)
  const delay =
    Number.isFinite(target) && target > now ? target - now : paymentSyncCacheTTL
  const cache = readPaymentSyncCache(key)
  cache.deferredUntil = Math.max(
    cache.deferredUntil,
    now + Math.min(maxCooldown, Math.max(1000, delay))
  )
  // Keep earlier failed attempts out of the way when the delayed batch resumes.
  for (const attempt of Object.values(cache.attempts)) {
    if (attempt.code !== 'verified') {
      attempt.expiresAt = Math.max(
        attempt.expiresAt,
        cache.deferredUntil + paymentSyncCacheTTL
      )
    }
  }
  savePaymentSyncCache(key, cache)
  return cache.deferredUntil
}
