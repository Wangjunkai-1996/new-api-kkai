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
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import { useDebounce } from '@/hooks/use-debounce'

import { listWalletOrders } from '../payment-flow-api'
import type { PaymentStatusGroup } from '../payment-flow-types'
import { useWalletPaymentIdentity } from './use-wallet-payment-flow'

interface UseBillingHistoryOptions {
  enabled?: boolean
  initialPage?: number
  initialPageSize?: number
  scope?: 'self' | 'admin'
  unfinished?: boolean
}
export function useBillingHistory(options: UseBillingHistoryOptions = {}) {
  const identity = useWalletPaymentIdentity()
  const [page, setPage] = useState(options.initialPage ?? 1)
  const [pageSize, setPageSize] = useState(options.initialPageSize ?? 10)
  const [keyword, setKeyword] = useState('')
  const [exact, setExact] = useState(false)
  const [status, setStatus] = useState<PaymentStatusGroup>('all')
  const debouncedKeyword = useDebounce(keyword)
  const admin = options.scope === 'admin'
  const scope = options.unfinished ? 'unfinished' : 'wallet'
  const query = useQuery({
    queryKey: [
      'wallet-payment',
      ...identity,
      'history',
      admin,
      scope,
      status,
      page,
      pageSize,
      debouncedKeyword,
      exact,
    ],
    queryFn: ({ signal }) =>
      listWalletOrders(
        {
          admin,
          page,
          pageSize,
          keyword: exact ? '' : debouncedKeyword,
          exactTradeNo: exact ? debouncedKeyword : '',
          status,
          scope,
        },
        signal
      ),
    enabled: options.enabled !== false && keyword === debouncedKeyword,
    retry: false,
    staleTime: 0,
    refetchOnWindowFocus: false,
  })
  return {
    records: query.data?.items ?? [],
    total: query.data?.total ?? 0,
    counts: query.data?.counts,
    serverTime: query.data?.server_time ?? 0,
    todayPaymentTotal: query.data?.today_payment_total ?? null,
    todayPaymentDate: query.data?.today_payment_date ?? null,
    page,
    pageSize,
    keyword,
    status,
    exact,
    isAdmin: admin,
    loading: query.isFetching,
    error: query.isError,
    handlePageChange: setPage,
    handlePageSizeChange: (value: number) => {
      setPageSize(value)
      setPage(1)
    },
    handleSearch: (value: string) => {
      setKeyword(value)
      setPage(1)
    },
    handleStatusChange: (value: PaymentStatusGroup) => {
      setStatus(value)
      setPage(1)
    },
    handleExactChange: (value: boolean) => {
      setExact(value)
      setPage(1)
    },
    refresh: query.refetch,
  }
}
