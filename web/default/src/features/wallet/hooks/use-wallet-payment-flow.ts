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
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react'

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
  getWalletOrder,
  restoreWalletPayment,
  resumeWalletPayment,
  WalletPaymentError,
  walletRequestRetryAt,
} from '../payment-flow-api'
import type {
  PaymentOrder,
  PaymentOrderResult,
  PaymentQuote,
  WalletState,
} from '../payment-flow-types'

function canRefreshWalletRead(error: unknown): boolean {
  if (!(error instanceof WalletPaymentError) || error.status !== 429) {
    return true
  }
  const retryAt = walletRequestRetryAt(error)
  return retryAt !== undefined && retryAt <= Date.now()
}

export function useWalletPaymentIdentity() {
  const userId = useAuthStore((state) => state.auth.user?.id)
  const session = useAuthStore((state) => state.auth.session?.sid)
  const { data: site } = useSiteConfig()
  const host = window.location.host
  const tenantId = site?.tenant_id
  return useMemo(
    () => [host, tenantId, userId, session] as const,
    [host, tenantId, userId, session]
  )
}

export function useWalletPaymentFlow(
  initialTradeNo?: string,
  writesEnabled = true
) {
  const identity = useWalletPaymentIdentity()
  const identityReady = identity.every(
    (part) => part !== undefined && part !== null && part !== ''
  )
  const identityKey = identity.join(':')
  const currentIdentity = useRef(identityKey)
  const writeGate = useMemo(
    () => ({ identityKey, writesEnabled }),
    [identityKey, writesEnabled]
  )
  const activeWriteGate = useRef<{
    key: typeof writeGate
    confirmed: boolean
    acceptingReads: boolean
  } | null>(null)
  const [confirmedGate, setConfirmedGate] = useState<typeof writeGate>()
  useLayoutEffect(() => {
    activeWriteGate.current = {
      key: writeGate,
      confirmed: false,
      acceptingReads: false,
    }
    return () => {
      activeWriteGate.current = null
    }
  }, [writeGate])
  const pollUntil = useRef(0)
  const [pollDeadline, setPollDeadline] = useState(() => Date.now() + 120_000)
  const [pollingPaused, setPollingPaused] = useState(false)
  const [operationError, setOperationError] = useState<unknown>()
  const [notice, setNotice] = useState('')
  const [dismissedReturn, setDismissedReturn] = useState('')
  const returnKey = `${identityKey}:${initialTradeNo ?? ''}`
  const returnDismissed = dismissedReturn === returnKey
  useEffect(() => {
    currentIdentity.current = identityKey
    pollUntil.current = Date.now() + 120_000
    setPollDeadline(pollUntil.current)
    setPollingPaused(false)
    setOperationError(undefined)
    setNotice('')
  }, [identityKey])
  const client = useQueryClient()
  const queryKey = useMemo(
    () => ['wallet-payment', ...identity, 'current'] as const,
    [identity]
  )
  const restored = useRef('')
  const lastFocusRefresh = useRef(0)
  const operationBusy = useRef(false)
  const [attempt, setAttempt] = useState<{
    identity: string
    key: string
    quote: PaymentQuote
    revision: string
  } | null>(null)
  const currentAttempt = attempt?.identity === identityKey ? attempt : null
  const apply = useCallback(
    (next: WalletState) => {
      if (currentIdentity.current !== identityKey) return
      client.setQueryData<WalletState>(queryKey, (previous) =>
        mergeWalletState(previous, next)
      )
    },
    [client, identityKey, queryKey]
  )
  const current = useQuery({
    queryKey,
    enabled: identityReady,
    networkMode: 'always',
    queryFn: async ({ signal }) => {
      let next = await getWalletPayment(signal)
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
    retryOnMount: (query) => canRefreshWalletRead(query.state.error),
    refetchOnMount: (query) =>
      canRefreshWalletRead(query.state.error) ? 'always' : false,
    retry: false,
    meta: { errorToast: false },
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    refetchInterval: (query) => {
      const phase = query.state.data?.flow.phase
      if (
        document.hidden ||
        !canRefreshWalletRead(query.state.error) ||
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
  const previousWriteGate = useRef(writeGate)
  const { refetch } = current
  useLayoutEffect(() => {
    const cache = client.getQueryCache()
    const query = cache.find({ queryKey, exact: true })
    const previous = previousWriteGate.current
    previousWriteGate.current = writeGate
    const requiresNewRead =
      previous !== writeGate &&
      previous.identityKey === writeGate.identityKey &&
      writeGate.writesEnabled &&
      identityReady
    if (activeWriteGate.current) {
      activeWriteGate.current.acceptingReads = identityReady && !requiresNewRead
    }
    const unsubscribe = cache.subscribe((event) => {
      if (
        event.type !== 'updated' ||
        event.query !== query ||
        activeWriteGate.current?.key !== writeGate
      ) {
        return
      }
      // Cached data and setQueryData are display-only until this observer sees
      // a successful network read. Shared in-flight reads still count once.
      if (
        activeWriteGate.current.acceptingReads &&
        event.action.type === 'success' &&
        !event.action.manual
      ) {
        activeWriteGate.current.confirmed = true
        setConfirmedGate(writeGate)
      } else if (
        event.action.type === 'error' &&
        event.query.state.status === 'error'
      ) {
        activeWriteGate.current.confirmed = false
        setConfirmedGate((previous) =>
          previous === writeGate ? undefined : previous
        )
      }
    })
    // A read started while writes were disabled cannot re-authorize payment.
    let cancelled = false
    if (requiresNewRead) {
      void client.cancelQueries({ queryKey, exact: true }).then(() => {
        if (cancelled || activeWriteGate.current?.key !== writeGate) return
        activeWriteGate.current.acceptingReads = true
        if (canRefreshWalletRead(client.getQueryState(queryKey)?.error)) {
          void refetch()
        }
      })
    }
    return () => {
      cancelled = true
      unsubscribe()
    }
  }, [client, identityReady, queryKey, refetch, writeGate])
  const writesReady =
    identityReady &&
    writesEnabled &&
    confirmedGate === writeGate &&
    current.isSuccess
  const canWrite = useCallback(
    () =>
      identityReady &&
      activeWriteGate.current?.key === writeGate &&
      writeGate.writesEnabled &&
      activeWriteGate.current.confirmed &&
      client.getQueryState(queryKey)?.status === 'success',
    [client, identityReady, queryKey, writeGate]
  )
  const returned = useQuery({
    queryKey: ['wallet-payment', ...identity, 'return-order', initialTradeNo],
    networkMode: 'always',
    queryFn: ({ signal }) => {
      if (!initialTradeNo) throw new Error('Payment return unavailable')
      return getWalletOrder(initialTradeNo, false, signal)
    },
    enabled:
      identityReady &&
      !!initialTradeNo &&
      !returnDismissed &&
      current.data?.flow.order?.trade_no !== initialTradeNo,
    retry: false,
    retryOnMount: (query) => canRefreshWalletRead(query.state.error),
    refetchOnMount: (query) => canRefreshWalletRead(query.state.error),
    meta: { errorToast: false },
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    refetchInterval: (query) => {
      const order = query.state.data?.order
      if (
        document.hidden ||
        Date.now() >= pollUntil.current ||
        query.state.error ||
        !order ||
        ['completed', 'closed'].includes(order.status_group)
      ) {
        return false
      }
      return 5000
    },
  })
  const blocked = (scope: string) =>
    operationError instanceof WalletPaymentError &&
    operationError.status === 429 &&
    !!operationError.retryAt &&
    operationError.retryAt > Date.now() &&
    (!operationError.rateLimitScope || operationError.rateLimitScope === scope)
  const restartPolling = useCallback(() => {
    pollUntil.current = Date.now() + 120_000
    setPollDeadline(pollUntil.current)
    setPollingPaused(false)
  }, [])
  useEffect(() => {
    const timer = window.setTimeout(
      () => setPollingPaused(true),
      Math.max(0, pollDeadline - Date.now())
    )
    return () => window.clearTimeout(timer)
  }, [pollDeadline])
  const mutation = useMutation({
    mutationFn: async (run: () => Promise<void>) => run(),
    retry: false,
    onMutate: async () => {
      setNotice('')
      setOperationError(undefined)
      await client.cancelQueries({ queryKey, exact: true })
      restartPolling()
      return identityKey
    },
    onError: (error, _run, operationIdentity) => {
      if (currentIdentity.current !== operationIdentity) return
      setOperationError(error)
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
  const recoveryRevision = current.data?.flow.revision
  const recoverable =
    writesReady &&
    !initialTradeNo &&
    current.data?.flow.phase === 'idle' &&
    !current.data.can_create &&
    ['RECOVERABLE_ORDER_EXISTS', 'CURRENT_FLOW_ACTIVE'].includes(
      current.data.create_block_reason_code ?? ''
    )
  const { mutate } = mutation
  useEffect(() => {
    const recoveryKey = `${identityKey}:${recoveryRevision}`
    if (
      !recoverable ||
      restored.current === recoveryKey ||
      operationBusy.current
    ) {
      return
    }
    operationBusy.current = true
    mutate(async () => {
      if (!canWrite()) return
      restored.current = recoveryKey
      apply(await restoreWalletPayment())
    })
  }, [identityKey, recoveryRevision, recoverable, mutate, apply, canWrite])
  const deadline = current.data?.flow.order?.deadline_at
  const serverTime = current.data?.server_time
  const phase = current.data?.flow.phase
  const fundsReceived =
    current.data?.flow.order?.display_status === 'credit_review'
  useEffect(() => {
    if (
      !identityReady ||
      !deadline ||
      !serverTime ||
      deadline <= serverTime ||
      !phase ||
      fundsReceived ||
      phase === 'idle' ||
      phase === 'result'
    ) {
      return
    }
    const timer = window.setTimeout(
      () => {
        const error = client.getQueryState(queryKey)?.error
        if (document.hidden || !canRefreshWalletRead(error)) {
          return
        }
        restartPolling()
        void refetch()
      },
      Math.max(0, deadline - serverTime) * 1000
    )
    return () => window.clearTimeout(timer)
  }, [
    deadline,
    identityReady,
    serverTime,
    phase,
    fundsReceived,
    refetch,
    restartPolling,
    client,
    queryKey,
  ])
  const { refetch: refetchReturned } = returned
  const returnError = returned.error
  useEffect(() => {
    const refresh = () => {
      if (
        !identityReady ||
        document.hidden ||
        Date.now() - lastFocusRefresh.current < 1000
      ) {
        return
      }
      if (!canRefreshWalletRead(current.error)) {
        return
      }
      lastFocusRefresh.current = Date.now()
      restartPolling()
      void refetch()
      if (
        initialTradeNo &&
        !returnDismissed &&
        current.data?.flow.order?.trade_no !== initialTradeNo
      ) {
        if (canRefreshWalletRead(returnError)) {
          void refetchReturned()
        }
      }
    }
    window.addEventListener('focus', refresh)
    document.addEventListener('visibilitychange', refresh)
    return () => {
      window.removeEventListener('focus', refresh)
      document.removeEventListener('visibilitychange', refresh)
    }
  }, [
    identityKey,
    identityReady,
    refetch,
    current.error,
    current.data?.flow.order?.trade_no,
    initialTradeNo,
    returnDismissed,
    returnError,
    refetchReturned,
    restartPolling,
  ])

  const open = async (
    order: PaymentOrder,
    wallet: WalletState,
    target: ReturnType<typeof preparePaymentWindow>
  ) => {
    try {
      if (!canWrite()) {
        closePaymentPlaceholder(target)
        return
      }
      const result = await resumeWalletPayment(order, wallet)
      if (currentIdentity.current !== identityKey || !canWrite()) {
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
  const create = (
    quote: PaymentQuote,
    preparedTarget?: ReturnType<typeof preparePaymentWindow>
  ) => {
    const wallet = client.getQueryData<WalletState>(queryKey)
    if (
      currentIdentity.current !== identityKey ||
      !canWrite() ||
      blocked('wallet_checkout') ||
      operationBusy.current ||
      mutation.isPending ||
      client.getQueryState(queryKey)?.status === 'error' ||
      !wallet?.can_create ||
      wallet.flow.phase !== 'idle'
    ) {
      if (preparedTarget) closePaymentPlaceholder(preparedTarget)
      return
    }
    const value = currentAttempt ?? {
      identity: identityKey,
      key: crypto.randomUUID(),
      quote,
      revision: wallet.flow.revision,
    }
    operationBusy.current = true
    setAttempt(value)
    const target = preparedTarget ?? preparePaymentWindow()
    mutation.mutate(async () => {
      try {
        if (!canWrite()) {
          closePaymentPlaceholder(target)
          return
        }
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
    if (!canWrite() || blocked('wallet_checkout')) return
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
  const acceptOrderAction = async (
    result: PaymentOrderResult,
    action: string
  ): Promise<boolean> => {
    if (currentIdentity.current !== identityKey) return false
    apply(result.wallet)
    const wallet = client.getQueryData<WalletState>(queryKey)
    if (
      !canWrite() ||
      action !== 'end' ||
      !wallet?.flow.id ||
      wallet.flow.id !== result.wallet.flow.id ||
      wallet.flow.revision !== result.wallet.flow.revision ||
      wallet.flow.phase !== 'result' ||
      wallet.flow.order?.trade_no !== result.order.trade_no ||
      wallet.flow.order.version !== result.order.version ||
      wallet.flow.order.display_status !== 'ended' ||
      !wallet.flow.order.allowed_actions.includes('acknowledge')
    ) {
      return false
    }
    apply(await acknowledgeWalletPayment(wallet))
    if (
      currentIdentity.current !== identityKey ||
      client.getQueryData<WalletState>(queryKey)?.flow.phase !== 'idle'
    ) {
      return false
    }
    setAttempt((previous) => (previous === attempt ? null : previous))
    return true
  }
  const act = (
    order: PaymentOrder,
    action: 'cancel' | 'end' | 'reconcile' | 'admin-reconcile',
    reason?: string
  ) => {
    if (
      !canWrite() ||
      blocked(
        action === 'reconcile' || action === 'admin-reconcile'
          ? 'wallet_reconcile'
          : 'wallet_state'
      ) ||
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
      if (!canWrite()) return
      const result = await actOnWalletOrder(order, action, reason)
      await acceptOrderAction(result, action)
    })
  }
  const acknowledge = () => {
    const wallet = current.data
    if (
      !canWrite() ||
      blocked('wallet_state') ||
      !wallet?.flow.order?.allowed_actions.includes('acknowledge') ||
      operationBusy.current ||
      mutation.isPending
    ) {
      return
    }
    operationBusy.current = true
    mutation.mutate(async () => {
      if (!canWrite()) return
      apply(await acknowledgeWalletPayment(wallet))
      setDismissedReturn(returnKey)
      setAttempt(null)
    })
  }
  const restore = (tradeNo?: string) => {
    if (!canWrite() || operationBusy.current || blocked('wallet_state')) return
    operationBusy.current = true
    mutation.mutate(async () => {
      if (canWrite()) apply(await restoreWalletPayment(tradeNo))
    })
  }
  const resumeReturned = () => {
    const order = returned.data?.order
    if (
      !canWrite() ||
      !order ||
      order.display_status !== 'pending' ||
      !order.allowed_actions.includes('resume') ||
      isLocallyEnded(order) ||
      current.isError ||
      current.data?.flow.order ||
      operationBusy.current ||
      blocked('wallet_state') ||
      blocked('wallet_checkout')
    ) {
      return
    }
    operationBusy.current = true
    const target = preparePaymentWindow()
    mutation.mutate(async () => {
      try {
        if (!canWrite()) {
          closePaymentPlaceholder(target)
          return
        }
        const wallet = await restoreWalletPayment(order.trade_no)
        apply(wallet)
        if (wallet.flow.order?.trade_no !== order.trade_no) {
          closePaymentPlaceholder(target)
          return
        }
        await open(wallet.flow.order, wallet, target)
      } catch (error) {
        closePaymentPlaceholder(target)
        throw error
      }
    })
  }
  return {
    wallet: current.data,
    loading:
      current.isPending ||
      (writesEnabled && !current.isError && confirmedGate !== writeGate),
    writesReady,
    error: current.isError,
    readError: current.error,
    operationError,
    pollingPaused,
    returnedOrder:
      !returnDismissed && initialTradeNo !== current.data?.flow.order?.trade_no
        ? returned.data?.order
        : undefined,
    returnedServerTime: returned.data?.wallet.server_time,
    returnedLoading:
      !!initialTradeNo &&
      !returnDismissed &&
      initialTradeNo !== current.data?.flow.order?.trade_no &&
      returned.isPending,
    returnedError:
      !returnDismissed && initialTradeNo !== current.data?.flow.order?.trade_no
        ? returned.error
        : null,
    dismissReturned: () => setDismissedReturn(returnKey),
    resumeReturned,
    refreshReturned: () => {
      if (
        !identityReady ||
        (returned.error instanceof WalletPaymentError &&
          returned.error.retryAt &&
          returned.error.retryAt > Date.now())
      ) {
        return
      }
      restartPolling()
      void returned.refetch()
    },
    notice,
    busy: mutation.isPending,
    creating: !!currentAttempt,
    create,
    resume,
    act,
    acceptOrderAction,
    acknowledge,
    restore,
    recoverable,
    retryRecovery: () => restore(),
    retryCreate: () => {
      if (currentAttempt) create(currentAttempt.quote)
    },
    refresh: () => {
      if (
        !identityReady ||
        (current.error instanceof WalletPaymentError &&
          current.error.retryAt &&
          current.error.retryAt > Date.now())
      ) {
        return
      }
      setOperationError(undefined)
      restartPolling()
      void current.refetch()
    },
    apply,
  }
}
