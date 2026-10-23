import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'

import { getCurrentHost } from '@/lib/host-scope'
import {
  getServerErrorSources,
  getServerErrorStatus,
} from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { getInvoiceOrders, invoiceKey, verifyInvoicePayment } from '../api'
import type { InvoiceOrder, InvoiceOrderRef } from '../types'
import { invoiceOrderKey } from './format'

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
  const running = useRef(false)
  const failedOrders = useRef(new Map<string, InvoicePaymentSyncFailure>())
  const [failures, setFailures] = useState<InvoicePaymentSyncFailure[]>([])
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
        for (const key of failedOrders.current.keys()) {
          if (!snapshot.has(key)) failedOrders.current.delete(key)
        }
        // Resume untouched orders first when a previous batch hit the rate limit.
        const orders = [...snapshot.values()]
          .filter((order) => order.allowed_actions?.includes('verify_payment'))
          .sort(
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
                { source_type: order.source_type, order_id: order.order_id },
                crypto.randomUUID()
              ).finally(() => paymentVerifications.delete(key))
              paymentVerifications.set(key, verification)
            }
            await verification
            if (!isCurrentScope()) return
            failedOrders.current.delete(invoiceOrderKey(order))
          } catch (error) {
            if (!isCurrentScope()) return
            const individualFailure =
              getServerErrorStatus(error) === 409 &&
              getServerErrorSources(error).some((source) =>
                ['PAYMENT_VERIFICATION_FAILED', 'PAYMENT_EXCEPTION'].includes(
                  String(source.code)
                )
              )
            if (!individualFailure) {
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
      } finally {
        running.current = false
        if (isCurrentScope()) {
          setFailures([...failedOrders.current.values()])
          if (attempted) {
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
      startedScope.current = scope
      mutate()
    }
    return () => {
      activeScope.current = null
    }
  }, [enabled, userId, scope, mutate])
  return { ...sync, failures }
}
