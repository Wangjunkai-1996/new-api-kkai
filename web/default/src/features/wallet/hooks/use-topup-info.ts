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
import { type Query, useQuery, useQueryClient } from '@tanstack/react-query'
import axios from 'axios'

import { getTopupInfo } from '../api'
import {
  generatePresetAmounts,
  mergePresetAmounts,
  getMinTopupAmount,
} from '../lib'
import { WalletPaymentError, walletRequestRetryAt } from '../payment-flow-api'
import type {
  TopupInfo,
  PresetAmount,
  CreemProduct,
  PaymentMethod,
  WaffoPayMethod,
} from '../types'
import { useWalletPaymentIdentity } from './use-wallet-payment-flow'

// ============================================================================
// Topup Info Hook
// ============================================================================

function parseJsonArray(data: unknown): unknown[] {
  if (Array.isArray(data)) {
    return data
  }

  if (typeof data === 'string') {
    try {
      const parsed = JSON.parse(data)
      return Array.isArray(parsed) ? parsed : []
    } catch {
      return []
    }
  }

  return []
}

function parsePaymentMethods(
  data: unknown,
  stripeMinTopup: number
): PaymentMethod[] {
  return parseJsonArray(data)
    .filter(
      (item): item is Record<string, unknown> =>
        !!item && typeof item === 'object'
    )
    .map((item) => {
      const rawMinTopup = Number(item.min_topup)
      const normalizedMinTopup = Number.isFinite(rawMinTopup) ? rawMinTopup : 0
      const type = typeof item.type === 'string' ? item.type : ''

      return {
        name: typeof item.name === 'string' ? item.name : '',
        type,
        color: typeof item.color === 'string' ? item.color : undefined,
        icon: typeof item.icon === 'string' ? item.icon : undefined,
        min_topup:
          type === 'stripe' && normalizedMinTopup <= 0
            ? stripeMinTopup
            : normalizedMinTopup,
      }
    })
    .filter((item) => item.name && item.type && item.type !== 'waffo')
}

function parseWaffoPayMethods(data: unknown): WaffoPayMethod[] {
  return parseJsonArray(data)
    .filter(
      (item): item is Record<string, unknown> =>
        !!item && typeof item === 'object'
    )
    .map((item) => ({
      name: typeof item.name === 'string' ? item.name : '',
      icon: typeof item.icon === 'string' ? item.icon : undefined,
      payMethodType:
        typeof item.payMethodType === 'string' ? item.payMethodType : undefined,
      payMethodName:
        typeof item.payMethodName === 'string' ? item.payMethodName : undefined,
    }))
    .filter((item) => item.name)
}

function parseCreemProducts(data: unknown): CreemProduct[] {
  return parseJsonArray(data)
    .filter(
      (item): item is Record<string, unknown> =>
        !!item && typeof item === 'object'
    )
    .map((item) => {
      const currency: CreemProduct['currency'] =
        item.currency === 'EUR' ? 'EUR' : 'USD'

      return {
        name: typeof item.name === 'string' ? item.name : '',
        productId: typeof item.productId === 'string' ? item.productId : '',
        price: Number(item.price) || 0,
        quota: Number(item.quota) || 0,
        currency,
      }
    })
    .filter((item) => item.name && item.productId)
}

function parseAmountOptions(data: unknown): number[] {
  return parseJsonArray(data)
    .map((item) => Number(item))
    .filter((item) => Number.isFinite(item) && item > 0)
}

function parseDiscountMap(data: unknown): Record<number, number> {
  if (!data) {
    return {}
  }

  let parsedData = data

  if (typeof data === 'string') {
    try {
      parsedData = JSON.parse(data)
    } catch {
      return {}
    }
  }

  if (
    !parsedData ||
    typeof parsedData !== 'object' ||
    Array.isArray(parsedData)
  ) {
    return {}
  }

  return Object.entries(parsedData).reduce<Record<number, number>>(
    (result, [key, value]) => {
      const numericKey = Number(key)
      const numericValue = Number(value)

      if (Number.isFinite(numericKey) && Number.isFinite(numericValue)) {
        result[numericKey] = numericValue
      }

      return result
    },
    {}
  )
}

interface WalletConfiguration {
  topupInfo: TopupInfo
  presetAmounts: PresetAmount[]
  capability: 'legacy' | 'lifecycle' | 'invalid'
  confirmedLifecycle: boolean
}

function canRefreshWalletConfiguration(query: Query<WalletConfiguration>) {
  const error = query.state.error
  const limited =
    (error instanceof WalletPaymentError && error.status === 429) ||
    (axios.isAxiosError(error) && error.response?.status === 429)
  if (!limited) return true
  const retryAt = walletRequestRetryAt(error, query.state.errorUpdatedAt)
  return retryAt !== undefined && Date.now() >= retryAt
}

export function useTopupInfo() {
  const client = useQueryClient()
  const identity = useWalletPaymentIdentity()
  const identityReady = identity.every(
    (part) => part !== undefined && part !== null && part !== ''
  )
  const queryKey = ['wallet-payment', ...identity, 'info']
  const query = useQuery({
    queryKey,
    enabled: identityReady,
    queryFn: async ({ signal }): Promise<WalletConfiguration> => {
      const response = await getTopupInfo(signal)
      if (!response.success || !response.data) {
        throw new Error('Wallet configuration unavailable')
      }
      const ready = response.data.wallet_lifecycle_ready
      let capability: 'legacy' | 'lifecycle' | 'invalid' = 'invalid'
      if (ready === undefined || ready === false) capability = 'legacy'
      if (ready === true) capability = 'lifecycle'
      const topupInfo: TopupInfo = {
        ...response.data,
        wallet_lifecycle_ready: typeof ready === 'boolean' ? ready : undefined,
        pay_methods: parsePaymentMethods(
          response.data.pay_methods,
          response.data.stripe_min_topup
        ),
        amount_options: parseAmountOptions(response.data.amount_options),
        discount: parseDiscountMap(response.data.discount),
        creem_products: parseCreemProducts(response.data.creem_products),
        waffo_pay_methods: parseWaffoPayMethods(
          response.data.waffo_pay_methods
        ),
      }
      const presetAmounts =
        topupInfo.amount_options.length > 0
          ? mergePresetAmounts(
              topupInfo.amount_options,
              topupInfo.discount || {}
            )
          : generatePresetAmounts(getMinTopupAmount(topupInfo))
      const confirmedLifecycle =
        capability === 'lifecycle' ||
        client.getQueryData<WalletConfiguration>(queryKey)
          ?.confirmedLifecycle === true
      return { topupInfo, presetAmounts, capability, confirmedLifecycle }
    },
    staleTime: 30_000,
    gcTime: 300_000,
    retryOnMount: false,
    refetchOnMount: (query) =>
      canRefreshWalletConfiguration(query) ? 'always' : false,
    refetchOnWindowFocus: canRefreshWalletConfiguration,
    refetchOnReconnect: canRefreshWalletConfiguration,
    networkMode: 'always',
    retry: false,
    meta: { errorToast: false },
    refetchInterval: (state) =>
      state.state.data?.capability === 'lifecycle' || state.state.error
        ? false
        : 30_000,
    refetchIntervalInBackground: false,
  })

  return {
    topupInfo: query.data?.topupInfo ?? null,
    presetAmounts: query.data?.presetAmounts ?? [],
    capability: query.data?.capability,
    confirmedLifecycle: query.data?.confirmedLifecycle === true,
    loading: query.isPending,
    refreshing: query.isFetching,
    fresh: query.isFetchedAfterMount && query.isSuccess,
    error: query.error,
    errorUpdatedAt: query.errorUpdatedAt,
    refetch: query.refetch,
    identityReady,
  }
}
