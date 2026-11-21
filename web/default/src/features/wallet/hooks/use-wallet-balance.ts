/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/
import { useQuery } from '@tanstack/react-query'
import axios from 'axios'
import { useMemo } from 'react'

import { getWalletBalance } from '../api'
import { walletRequestRetryAt } from '../payment-flow-api'
import type { UserWalletData } from '../types'
import { useWalletPaymentIdentity } from './use-wallet-payment-flow'

function canRefreshWalletBalance(query: {
  state: { error: unknown; errorUpdatedAt: number }
}): boolean {
  const error = query.state.error
  if (!axios.isAxiosError(error) || error.response?.status !== 429) return true
  const retryAt = walletRequestRetryAt(error, query.state.errorUpdatedAt)
  return retryAt !== undefined && Date.now() >= retryAt
}

function refetchWalletBalanceOnMount(query: {
  state: { error: unknown; errorUpdatedAt: number }
}): boolean | 'always' {
  const error = query.state.error
  if (!axios.isAxiosError(error) || error.response?.status !== 429) {
    return 'always'
  }
  return canRefreshWalletBalance(query)
}

export function useWalletBalance({
  enabled = true,
}: { enabled?: boolean } = {}) {
  const identity = useWalletPaymentIdentity()
  const identityReady = identity.every(
    (part) => part !== undefined && part !== null && part !== ''
  )
  const queryKey = useMemo(
    () => ['wallet-payment', ...identity, 'balance'] as const,
    [identity]
  )
  const query = useQuery<
    UserWalletData,
    Error,
    UserWalletData,
    typeof queryKey
  >({
    queryKey,
    enabled: ({ state }) =>
      enabled && identityReady && canRefreshWalletBalance({ state }),
    queryFn: async ({ signal }) => {
      const response = await getWalletBalance(signal)
      const data = response?.data
      if (
        response?.success !== true ||
        !data ||
        !Number.isSafeInteger(data.quota)
      ) {
        throw new Error('Wallet balance unavailable')
      }
      return data
    },
    staleTime: 10_000,
    refetchOnMount: refetchWalletBalanceOnMount,
    refetchOnWindowFocus: true,
    refetchOnReconnect: true,
    networkMode: 'always',
    retry: false,
    meta: { errorToast: false },
  })

  return { ...query, identityReady }
}
