import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'

import { getCurrentHost } from '@/lib/host-scope'
import { getServerErrorStatus } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { getInvoiceOrders, invoiceKey, verifyInvoicePayment } from '../api'
import type { InvoiceOrder, InvoiceOrderRef } from '../types'
import { invoiceOrderKey } from './format'
import {
  cachePaymentSyncResult,
  deferPaymentSync,
  paymentFailureCode,
  paymentSyncCacheKey,
  paymentSyncHash,
  readPaymentSyncCache,
} from './payment-sync-cache'

const paymentVerifications = new Map<string, Promise<{ verified: boolean }>>()

export type InvoicePaymentSyncFailure = {
  order: InvoiceOrderRef & { order_no: string }
  error: unknown
}

export function useInvoicePaymentSync(enabled: boolean) {
  const client = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const sessionId = useAuthStore((state) => state.auth.session?.sid)
  const host = getCurrentHost()
  const scope = `${host}:${userId}:${sessionId}`
  const activeScope = useRef<string | null>(null)
  const startedScope = useRef<string | null>(null)
  const running = useRef<string | null>(null)
  const failedOrders = useRef(new Map<string, InvoicePaymentSyncFailure>())
  const [feedback, setFeedback] = useState<{
    scope: string
    failures: InvoicePaymentSyncFailure[]
    deferredUntil: number | null
  }>({ scope, failures: [], deferredUntil: null })
  const failures = feedback.scope === scope ? feedback.failures : []
  const deferredUntil = feedback.scope === scope ? feedback.deferredUntil : null
  const storageKey = useRef<string | null>(null)
  const sync = useMutation({
    retry: false,
    meta: { errorToast: false },
    mutationFn: async () => {
      if (running.current === scope) return
      running.current = scope
      const isCurrentScope = () =>
        activeScope.current === scope &&
        getCurrentHost() === host &&
        useAuthStore.getState().auth.user?.id === userId &&
        useAuthStore.getState().auth.session?.sid === sessionId
      let attempted = false
      let snapshotRead = false
      let cacheKey: string | undefined
      try {
        const accountKey = paymentSyncCacheKey(
          await paymentSyncHash(JSON.stringify([host, userId]))
        )
        cacheKey = accountKey
        if (!isCurrentScope()) return
        storageKey.current = accountKey
        const synchronize = async () => {
          if (!isCurrentScope()) return
          const cache = readPaymentSyncCache(accountKey)
          setFeedback((previous) => ({
            scope,
            failures: previous.scope === scope ? previous.failures : [],
            deferredUntil: cache.deferredUntil || null,
          }))
          const snapshot = new Map<string, InvoiceOrder>()
          // Snapshot the pages before writes change eligibility and ordering.
          for (let p = 1; isCurrentScope(); p++) {
            const page = await getInvoiceOrders({
              eligibility: 'unavailable',
              source_type: 'topup',
              p,
              page_size: 100,
            })
            if (!isCurrentScope()) return
            for (const order of page.items) {
              if (order.source_type === 'topup') {
                snapshot.set(invoiceOrderKey(order), order)
              }
            }
            if (!page.items.length || p * page.page_size >= page.total) break
          }
          snapshotRead = true
          for (const key of failedOrders.current.keys()) {
            if (!snapshot.has(key)) failedOrders.current.delete(key)
          }
          const pending = await Promise.all(
            [...snapshot.values()]
              .filter((order) =>
                order.allowed_actions?.includes('verify_payment')
              )
              .map(async (order) => ({
                order,
                hash: await paymentSyncHash(
                  JSON.stringify([
                    host,
                    userId,
                    order.source_type,
                    order.order_id,
                  ])
                ),
              }))
          )
          if (!isCurrentScope()) return
          for (const { order, hash } of pending) {
            const result = cache.attempts[hash]
            if (!result) continue
            if (result.code === 'verified') {
              failedOrders.current.delete(invoiceOrderKey(order))
            } else {
              failedOrders.current.set(invoiceOrderKey(order), {
                order: {
                  source_type: order.source_type,
                  order_id: order.order_id,
                  order_no: order.order_no,
                },
                error: { status: 409, code: result.code },
              })
            }
          }
          // Resume untouched orders first when a previous batch hit the rate limit.
          const orders = pending.sort(
            (a, b) =>
              Number(failedOrders.current.has(invoiceOrderKey(a.order))) -
              Number(failedOrders.current.has(invoiceOrderKey(b.order)))
          )
          for (const { order, hash } of orders) {
            if (!isCurrentScope()) return
            const latest = readPaymentSyncCache(accountKey)
            const result = latest.attempts[hash]
            if (result) {
              if (result.code === 'verified') {
                failedOrders.current.delete(invoiceOrderKey(order))
              } else {
                failedOrders.current.set(invoiceOrderKey(order), {
                  order: {
                    source_type: order.source_type,
                    order_id: order.order_id,
                    order_no: order.order_no,
                  },
                  error: { status: 409, code: result.code },
                })
              }
              continue
            }
            if (latest.deferredUntil) {
              setFeedback((previous) => ({
                ...previous,
                deferredUntil: latest.deferredUntil,
              }))
              return
            }
            attempted = true
            try {
              const key = `${scope}:${invoiceOrderKey(order)}`
              let verification = paymentVerifications.get(key)
              if (!verification) {
                verification = verifyInvoicePayment(
                  { source_type: order.source_type, order_id: order.order_id },
                  crypto.randomUUID()
                )
                  .then(
                    (result) => {
                      if (result.verified !== true) {
                        throw new Error('PAYMENT_VERIFICATION_UNCONFIRMED')
                      }
                      cachePaymentSyncResult(accountKey, hash, 'verified')
                      return result
                    },
                    (error: unknown) => {
                      const code = paymentFailureCode(error)
                      if (code) cachePaymentSyncResult(accountKey, hash, code)
                      if (getServerErrorStatus(error) === 429) {
                        deferPaymentSync(accountKey, error)
                      }
                      throw error
                    }
                  )
                  .finally(() => paymentVerifications.delete(key))
                paymentVerifications.set(key, verification)
              }
              await verification
              if (!isCurrentScope()) return
              failedOrders.current.delete(invoiceOrderKey(order))
            } catch (error) {
              if (!isCurrentScope()) return
              if (!paymentFailureCode(error)) {
                throw error
              }
              failedOrders.current.set(invoiceOrderKey(order), {
                order: {
                  source_type: order.source_type,
                  order_id: order.order_id,
                  order_no: order.order_no,
                },
                error,
              })
            }
          }
        }
        if (typeof navigator !== 'undefined' && navigator.locks) {
          let entered = false
          try {
            await navigator.locks.request(accountKey, () => {
              entered = true
              return synchronize()
            })
          } catch (error) {
            if (entered) throw error
            await synchronize()
          }
        } else {
          await synchronize()
        }
      } catch (error) {
        if (!isCurrentScope()) return
        if (cacheKey && getServerErrorStatus(error) === 429) {
          const until =
            readPaymentSyncCache(cacheKey).deferredUntil ||
            deferPaymentSync(cacheKey, error)
          setFeedback((previous) => ({
            scope,
            failures: previous.scope === scope ? previous.failures : [],
            deferredUntil: until,
          }))
          return
        }
        throw error
      } finally {
        if (running.current === scope) running.current = null
        if (isCurrentScope()) {
          setFeedback((previous) => ({
            scope,
            failures: [...failedOrders.current.values()],
            deferredUntil:
              previous.scope === scope ? previous.deferredUntil : null,
          }))
          if (attempted || snapshotRead) {
            await client.invalidateQueries({ queryKey: invoiceKey(userId) })
          }
        }
      }
    },
  })
  const { mutate } = sync
  useEffect(() => {
    activeScope.current = enabled && userId ? scope : null
    if (activeScope.current && startedScope.current !== scope) {
      failedOrders.current.clear()
      storageKey.current = null
      startedScope.current = scope
      mutate()
    }
    return () => {
      activeScope.current = null
    }
  }, [enabled, userId, scope, mutate])
  useEffect(() => {
    if (!enabled || !userId || !deferredUntil) return
    const timer = window.setTimeout(
      () => mutate(),
      Math.max(0, deferredUntil - Date.now())
    )
    return () => window.clearTimeout(timer)
  }, [enabled, userId, scope, deferredUntil, mutate])
  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (
        event.key &&
        storageKey.current === event.key &&
        activeScope.current === scope
      ) {
        const until = readPaymentSyncCache(event.key).deferredUntil || null
        setFeedback((previous) => ({
          scope,
          failures: previous.scope === scope ? previous.failures : [],
          deferredUntil: until,
        }))
      }
    }
    window.addEventListener('storage', onStorage)
    return () => window.removeEventListener('storage', onStorage)
  }, [scope])
  return { ...sync, failures, deferredUntil }
}
