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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'

import { useSiteConfig } from '@/features/site-config'
import { useAuthStore } from '@/stores/auth-store'

import {
  closePaymentPlaceholder,
  mergeWalletState,
  isLocallyEnded,
  openPaymentCheckout,
  preparePaymentWindow,
} from '../lib/payment-flow'
import {
  acknowledgeWalletPayment,
  actOnWalletOrder,
  createWalletPayment,
  getWalletPayment,
  restoreWalletPayment,
  resumeWalletPayment,
  WalletPaymentError,
} from '../payment-flow-api'
import type {
  PaymentOrder,
  PaymentQuote,
  WalletState,
} from '../payment-flow-types'

export function useWalletPaymentIdentity() {
  const userId = useAuthStore((state) => state.auth.user?.id)
  const session = useAuthStore((state) => state.auth.session?.sid)
  const { data: site } = useSiteConfig()
  return [window.location.host, site?.tenant_id, userId, session] as const
}

export function useWalletPaymentFlow(initialTradeNo?: string) {
  const identity = useWalletPaymentIdentity()
  const identityKey = identity.join(':')
  const currentIdentity = useRef(identityKey)
  const pollUntil = useRef(0)
  useEffect(() => {
    currentIdentity.current = identityKey
    pollUntil.current = Date.now() + 120_000
  }, [identityKey])
  const client = useQueryClient()
  const queryKey = ['wallet-payment', ...identity, 'current'] as const
  const restored = useRef('')
  const operationBusy = useRef(false)
  const [notice, setNotice] = useState('')
  const [attempt, setAttempt] = useState<{
    identity: string
    key: string
    quote: PaymentQuote
    revision: string
  } | null>(null)
  const currentAttempt = attempt?.identity === identityKey ? attempt : null
  const apply = (next: WalletState) => {
    if (currentIdentity.current !== identityKey) return
    client.setQueryData<WalletState>(queryKey, (previous) =>
      mergeWalletState(previous, next)
    )
  }
  const current = useQuery({
    queryKey,
    queryFn: async ({ signal }) => {
      let next: WalletState
      try {
        next =
          restored.current === identityKey
            ? await getWalletPayment(signal)
            : await restoreWalletPayment(initialTradeNo, signal)
      } catch (error) {
        if (
          !(error instanceof WalletPaymentError) ||
          error.code !== 'CURRENT_FLOW_ACTIVE' ||
          !error.wallet
        ) {
          throw error
        }
        next = error.wallet
      }
      restored.current = identityKey
      const previous = client.getQueryData<WalletState>(queryKey)
      next = mergeWalletState(previous, next)
      const status = next.flow.order?.display_status
      if (
        status !== previous?.flow.order?.display_status &&
        (status === 'success' || status === 'credit_review')
      ) {
        void client.invalidateQueries({
          queryKey: ['wallet-payment', ...identity, 'funding-summary'],
        })
      }
      return next
    },
    retry: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    refetchInterval: (query) => {
      const phase = query.state.data?.flow.phase
      if (
        document.hidden ||
        Date.now() > pollUntil.current ||
        !phase ||
        phase === 'idle' ||
        phase === 'result'
      ) {
        return false
      }
      return 5000
    },
    refetchIntervalInBackground: false,
  })
  const mutation = useMutation({
    mutationFn: async (run: () => Promise<void>) => run(),
    retry: false,
    onMutate: async () => {
      setNotice('')
      await client.cancelQueries({ queryKey, exact: true })
      pollUntil.current = Date.now() + 120_000
    },
    onError: (error) => {
      if (currentIdentity.current !== identityKey) return
      if (error instanceof WalletPaymentError && error.wallet) {
        apply(error.wallet)
      }
      setNotice(
        error instanceof WalletPaymentError
          ? error.code
          : 'WALLET_STATE_UNAVAILABLE'
      )
    },
    onSettled: () => {
      operationBusy.current = false
      void client.invalidateQueries({
        queryKey: ['wallet-payment', ...identity, 'history'],
      })
      void client.invalidateQueries({
        queryKey: ['wallet-payment', ...identity, 'funding-summary'],
      })
    },
  })
  const { refetch } = current
  const deadline = current.data?.flow.order?.deadline_at
  const serverTime = current.data?.server_time
  const phase = current.data?.flow.phase
  const fundsReceived =
    current.data?.flow.order?.display_status === 'credit_review'
  useEffect(() => {
    if (
      !deadline ||
      !serverTime ||
      !phase ||
      fundsReceived ||
      phase === 'idle' ||
      phase === 'result'
    ) {
      return
    }
    const timer = window.setTimeout(
      () => {
        pollUntil.current = Date.now() + 120_000
        void refetch()
      },
      Math.max(0, deadline - serverTime) * 1000
    )
    return () => window.clearTimeout(timer)
  }, [deadline, serverTime, phase, fundsReceived, refetch])
  useEffect(() => {
    const refresh = () => {
      if (document.hidden) return
      pollUntil.current = Date.now() + 120_000
      void refetch()
    }
    window.addEventListener('focus', refresh)
    document.addEventListener('visibilitychange', refresh)
    return () => {
      window.removeEventListener('focus', refresh)
      document.removeEventListener('visibilitychange', refresh)
    }
  }, [identityKey, refetch])

  const open = async (
    order: PaymentOrder,
    wallet: WalletState,
    target: ReturnType<typeof preparePaymentWindow>
  ) => {
    try {
      const result = await resumeWalletPayment(order, wallet)
      if (currentIdentity.current !== identityKey) {
        closePaymentPlaceholder(target)
        return
      }
      apply(result.wallet)
      const latest = client.getQueryData<WalletState>(queryKey)
      if (
        !result.checkout ||
        latest?.flow.id !== wallet.flow.id ||
        latest.flow.order?.trade_no !== order.trade_no ||
        latest.flow.order.display_status !== 'pending' ||
        isLocallyEnded(latest.flow.order) ||
        !latest.flow.order.allowed_actions.includes('resume')
      ) {
        closePaymentPlaceholder(target)
        return
      }
      if (!openPaymentCheckout(result.checkout, target)) {
        setNotice('PAYMENT_WINDOW_UNAVAILABLE')
      }
    } catch (error) {
      closePaymentPlaceholder(target)
      throw error
    }
  }
  const create = (quote: PaymentQuote) => {
    if (
      operationBusy.current ||
      mutation.isPending ||
      !current.data?.can_create ||
      current.data.flow.phase !== 'idle'
    ) {
      return
    }
    const value = currentAttempt ?? {
      identity: identityKey,
      key: crypto.randomUUID(),
      quote,
      revision: current.data.flow.revision,
    }
    operationBusy.current = true
    setAttempt(value)
    const target = preparePaymentWindow()
    mutation.mutate(async () => {
      try {
        const result = await createWalletPayment(
          value.quote.quote_token,
          value.revision,
          value.key
        )
        if (currentIdentity.current !== identityKey) {
          closePaymentPlaceholder(target)
          return
        }
        apply(result.wallet)
        setAttempt(null)
        if (
          result.wallet.flow.order?.trade_no === result.order.trade_no &&
          !isLocallyEnded(result.order) &&
          result.order.display_status === 'pending' &&
          result.order.allowed_actions.includes('resume')
        ) {
          await open(result.order, result.wallet, target)
        } else closePaymentPlaceholder(target)
      } catch (error) {
        closePaymentPlaceholder(target)
        if (
          error instanceof WalletPaymentError &&
          [
            'VALIDATION_FAILED',
            'QUOTE_EXPIRED',
            'QUOTE_STALE',
            'CREATE_DISABLED',
            'QUOTA_CAPACITY_EXCEEDED',
            'CURRENT_FLOW_ACTIVE',
            'FLOW_CHANGED',
          ].includes(error.code)
        ) {
          setAttempt(null)
        }
        throw error
      }
    })
  }
  const resume = () => {
    const wallet = current.data,
      order = wallet?.flow.order
    if (
      operationBusy.current ||
      mutation.isPending ||
      !wallet ||
      !order?.allowed_actions.includes('resume') ||
      order.display_status !== 'pending' ||
      isLocallyEnded(order)
    ) {
      return
    }
    operationBusy.current = true
    const target = preparePaymentWindow()
    mutation.mutate(() => open(order, wallet, target))
  }
  const act = (
    order: PaymentOrder,
    action: 'cancel' | 'end' | 'reconcile' | 'admin-reconcile',
    reason?: string
  ) => {
    if (
      operationBusy.current ||
      isLocallyEnded(order) ||
      ((action === 'end' || action === 'cancel') &&
        (order.display_status === 'success' ||
          order.display_status === 'credit_review'))
    ) {
      return
    }
    operationBusy.current = true
    mutation.mutate(async () => {
      const result = await actOnWalletOrder(order, action, reason)
      apply(result.wallet)
    })
  }
  const acknowledge = () => {
    const wallet = current.data
    if (
      !wallet?.flow.order?.allowed_actions.includes('acknowledge') ||
      operationBusy.current ||
      mutation.isPending
    ) {
      return
    }
    operationBusy.current = true
    mutation.mutate(async () => {
      apply(await acknowledgeWalletPayment(wallet))
      setAttempt(null)
    })
  }
  const restore = (tradeNo: string) => {
    if (operationBusy.current) return
    operationBusy.current = true
    mutation.mutate(async () => {
      apply(await restoreWalletPayment(tradeNo))
    })
  }
  return {
    wallet: current.data,
    loading: current.isPending,
    error: current.isError,
    notice,
    busy: mutation.isPending,
    creating: !!currentAttempt,
    create,
    resume,
    act,
    acknowledge,
    restore,
    retryCreate: () => {
      if (currentAttempt) create(currentAttempt.quote)
    },
    refresh: () => {
      pollUntil.current = Date.now() + 120_000
      void current.refetch()
    },
    apply,
  }
}
