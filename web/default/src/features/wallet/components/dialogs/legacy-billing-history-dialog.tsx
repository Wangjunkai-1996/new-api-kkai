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
import { Link } from '@tanstack/react-router'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useSiteConfig } from '@/features/site-config'
import { useDebounce } from '@/hooks/use-debounce'
import { hasPermission } from '@/lib/admin-permissions'
import { formatCurrencyFromUSD } from '@/lib/currency'
import { formatNumber } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { getAllBillingHistory, getUserBillingHistory } from '../../api'
import { useWalletPaymentIdentity } from '../../hooks/use-wallet-payment-flow'
import {
  formatTimestamp,
  getPaymentMethodName,
  getStatusConfig,
} from '../../lib/billing'

interface LegacyBillingHistoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  scope: 'self' | 'admin'
}

export function LegacyBillingHistoryDialog(
  props: LegacyBillingHistoryDialogProps
) {
  const { t } = useTranslation()
  const { data: site } = useSiteConfig()
  const identity = useWalletPaymentIdentity()
  const user = useAuthStore((state) => state.auth.user)
  const admin = props.scope === 'admin'
  const permitted =
    !admin ||
    ((user?.role ?? 0) >= ROLE.ADMIN && hasPermission(user, 'billing', 'read'))
  const [page, setPage] = useState(1)
  const [keyword, setKeyword] = useState('')
  const search = useDebounce(keyword)
  const history = useQuery({
    queryKey: ['wallet-legacy-history', ...identity, props.scope, page, search],
    queryFn: async () => {
      const response = admin
        ? await getAllBillingHistory(page, 10, search)
        : await getUserBillingHistory(page, 10, search)
      if (!response.success || !response.data) {
        throw new Error('Billing history unavailable')
      }
      return response.data
    },
    enabled: props.open && permitted && keyword === search,
    retry: false,
  })
  const records = history.data?.items ?? []
  const total = history.data?.total ?? 0
  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={admin ? t('All top-up orders') : t('Order History')}
      description={t('View your topup transaction records and payment history')}
      contentClassName='max-sm:h-dvh max-sm:w-screen max-sm:max-w-none max-sm:rounded-none sm:max-w-4xl'
      bodyClassName='space-y-4'
    >
      {!permitted ? (
        <p role='alert'>{t('Admin access required')}</p>
      ) : (
        <>
          <div className='flex gap-2'>
            <Input
              aria-label={t('Search by order number...')}
              placeholder={t('Search by order number...')}
              value={keyword}
              onChange={(event) => {
                setKeyword(event.target.value)
                setPage(1)
              }}
            />
            <Button
              variant='outline'
              onClick={() => void history.refetch()}
              disabled={history.isFetching}
            >
              {t('Refresh')}
            </Button>
          </div>
          {history.isPending && <p role='status'>{t('Loading...')}</p>}
          {history.isError && (
            <p role='alert'>{t('Failed to load billing history')}</p>
          )}
          {!history.isPending && !history.isError && !records.length && (
            <p>{t('No orders found')}</p>
          )}
          <div className='space-y-3' aria-busy={history.isFetching}>
            {records.map((record) => {
              const status = getStatusConfig(record.status)
              return (
                <article
                  key={record.id}
                  className='space-y-3 rounded-lg border p-4'
                >
                  <div className='flex flex-wrap items-start justify-between gap-2'>
                    <div className='min-w-0'>
                      <code className='text-sm break-all'>
                        {record.trade_no}
                      </code>
                      <p className='text-muted-foreground text-xs'>
                        {formatTimestamp(record.create_time)}
                      </p>
                    </div>
                    <StatusBadge
                      label={t(status.label)}
                      variant={status.variant}
                      showDot
                      copyable={false}
                    />
                  </div>
                  <dl className='grid grid-cols-2 gap-3 text-sm sm:grid-cols-3'>
                    <div>
                      <dt className='text-muted-foreground'>
                        {t('Payment Method')}
                      </dt>
                      <dd>{getPaymentMethodName(record.payment_method, t)}</dd>
                    </div>
                    <div>
                      <dt className='text-muted-foreground'>{t('Amount')}</dt>
                      <dd>
                        {formatCurrencyFromUSD(record.amount, {
                          digitsLarge: 2,
                          digitsSmall: 2,
                          abbreviate: false,
                        })}
                      </dd>
                    </div>
                    <div>
                      <dt className='text-muted-foreground'>{t('Payment')}</dt>
                      <dd>{formatNumber(record.money)}</dd>
                    </div>
                  </dl>
                  {!admin &&
                    site?.tenant_id === 0 &&
                    record.status === 'success' && (
                      <Button
                        variant='outline'
                        size='sm'
                        render={
                          <Link
                            to='/invoices/new'
                            search={{
                              source_type: 'topup',
                              order_id: record.trade_no,
                            }}
                            onClick={() => props.onOpenChange(false)}
                          />
                        }
                      >
                        {t('Apply for invoice')}
                      </Button>
                    )}
                </article>
              )
            })}
          </div>
          <div className='flex items-center justify-between gap-3 text-sm'>
            <span>
              {t('Total')}: {total}
            </span>
            <div className='flex items-center gap-2'>
              <Button
                variant='outline'
                size='icon'
                aria-label={t('Previous page')}
                onClick={() => setPage(page - 1)}
                disabled={page <= 1 || history.isFetching}
              >
                <ChevronLeft aria-hidden='true' />
              </Button>
              <span>
                {page} / {Math.max(1, Math.ceil(total / 10))}
              </span>
              <Button
                variant='outline'
                size='icon'
                aria-label={t('Next page')}
                onClick={() => setPage(page + 1)}
                disabled={page * 10 >= total || history.isFetching}
              >
                <ChevronRight aria-hidden='true' />
              </Button>
            </div>
          </div>
        </>
      )}
    </Dialog>
  )
}
