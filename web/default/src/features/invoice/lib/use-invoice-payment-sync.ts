import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'

import { getCurrentHost } from '@/lib/host-scope'
import { getServerErrorStatus } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { getInvoiceOrders, invoiceKey, verifyInvoicePayment } from '../api'
import type { InvoiceOrderRef } from '../types'
import { invoiceOrderKey } from './format'

const paymentVerifications = new Map<string, Promise<{ verified: boolean }>>()

export function useInvoicePaymentSync(enabled: boolean) {
  const client = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const sessionId = useAuthStore((state) => state.auth.session?.sid)
  const host = getCurrentHost()
  const scope = `${host}:${userId}:${sessionId}`
  const activeScope = useRef<string | null>(null)
  const startedScope = useRef<string | null>(null)
  const running = useRef(false)
  const failedOrders = useRef(new Set<string>())
  const sync = useMutation({
    retry: false,
    meta: { errorToast: false },
    mutationFn: async () => {
      if (running.current) return
      running.current = true
      const isCurrentScope = () =>
        activeScope.current === scope &&
        getCurrentHost() === host &&
        useAuthStore.getState().auth.user?.id === userId &&
        useAuthStore.getState().auth.session?.sid === sessionId
      let attempted = false
      try {
        const pending = new Map<string, InvoiceOrderRef>()
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
            if (
              order.source_type === 'topup' &&
              order.allowed_actions?.includes('verify_payment')
            ) {
              pending.set(invoiceOrderKey(order), {
                source_type: order.source_type,
                order_id: order.order_id,
              })
            }
          }
          if (!page.items.length || p * page.page_size >= page.total) break
        }
        let firstError: unknown
        // Resume untouched orders first when a previous batch hit the rate limit.
        const orders = [...pending.values()].sort(
          (a, b) =>
            Number(failedOrders.current.has(invoiceOrderKey(a))) -
            Number(failedOrders.current.has(invoiceOrderKey(b)))
        )
        for (const order of orders) {
          if (!isCurrentScope()) return
          attempted = true
          try {
            const key = `${scope}:${invoiceOrderKey(order)}`
            let verification = paymentVerifications.get(key)
            if (!verification) {
              verification = verifyInvoicePayment(
                order,
                crypto.randomUUID()
              ).finally(() => paymentVerifications.delete(key))
              paymentVerifications.set(key, verification)
            }
            await verification
          } catch (error) {
            firstError ??= error
            const status = getServerErrorStatus(error)
            // Shared outages, authentication failures and throttling stop the batch.
            if (!status || [401, 403, 429].includes(status) || status >= 500) {
              throw error
            }
            failedOrders.current.add(invoiceOrderKey(order))
          }
        }
        if (firstError) throw firstError
      } finally {
        running.current = false
        if (attempted && isCurrentScope()) {
          await client.invalidateQueries({ queryKey: invoiceKey(userId) })
        }
      }
    },
  })
  const { mutate } = sync
  useEffect(() => {
    activeScope.current = enabled && userId ? scope : null
    if (activeScope.current && startedScope.current !== scope) {
      startedScope.current = scope
      mutate()
    }
    return () => {
      activeScope.current = null
    }
  }, [enabled, userId, scope, mutate])
  return sync
}
