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
import { Users } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useAuthStore } from '@/stores/auth-store'

import { getSalesCustomers } from '../../api'
import { requireInvitationData } from '../../api/result'
import { formatInvitationDate, formatSalesAmount } from '../../format'
import type { SalesCustomer, SalesRebates } from '../../types'
import { PaginationControls } from '../pagination-controls'
import { SalesCustomerRecharges } from './sales-customer-recharges'

const PAGE_SIZE = 20

export const SalesCustomers = () => {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [page, setPage] = useState(1)
  const [customer, setCustomer] = useState<SalesCustomer | null>(null)
  const query = useQuery({
    refetchInterval: 30_000,
    queryKey: ['kkai', 'invitations', 'sales-customers', userId, page],
    queryFn: async () =>
      requireInvitationData(
        await getSalesCustomers({ page, pageSize: PAGE_SIZE }),
        'Failed to load invited customers'
      ),
    staleTime: 0,
  })

  if (query.isPending) return <Skeleton className='h-80 w-full' />
  if (query.isError) {
    return (
      <ErrorState
        title={t('Failed to load invited customers')}
        description={query.error.message}
        onRetry={() => void query.refetch()}
      />
    )
  }

  const data = query.data
  const formatAmount = (amount: number) =>
    formatSalesAmount(amount, data.creditCurrency)
  return (
    <div className='space-y-4'>
      <Card>
        <CardHeader>
          <CardTitle>{t('Sales Overview')}</CardTitle>
        </CardHeader>
        <CardContent className='space-y-4'>
          <dl className='grid grid-cols-2 gap-4 sm:grid-cols-3'>
            <div>
              <dt className='text-muted-foreground text-sm'>
                {t('Total Invitations')}
              </dt>
              <dd className='text-2xl font-semibold tabular-nums'>
                {data.summary.invitedCount}
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground text-sm'>
                {t('Effective Invitations')}
              </dt>
              <dd className='text-2xl font-semibold tabular-nums'>
                {data.summary.effectiveInvitedCount}
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground text-sm'>
                {t('Confirmed Credited Recharges')}
              </dt>
              <dd className='text-2xl font-semibold tabular-nums'>
                {formatAmount(data.summary.rechargeAmount)}
              </dd>
            </div>
          </dl>
          <p className='text-muted-foreground text-sm'>
            {t(
              'A customer counts as effective after a successful paid wallet recharge, even if the balance is later spent.'
            )}
          </p>
          <SalesRebateAmounts
            rebates={data.summary.rebates}
            currency={data.creditCurrency}
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              'Rebate totals include recorded customer rewards. Recharges without a rebate record may still be syncing.'
            )}
          </p>
          <p className='text-muted-foreground text-sm'>
            {t(
              'New order rebates cool down for 7 days after payment. Expiry allows a request; approval remains manual.'
            )}
          </p>
          <p className='text-muted-foreground text-sm'>
            {data.creditCurrency === 'legacy_075'
              ? t('Legacy Site Credits')
              : t('USD Site Credits')}{' '}
            ·{' '}
            {t(
              'Amounts use confirmed credited value, not the original payment amount.'
            )}
          </p>
          {data.summary.unverifiedRechargeCount > 0 && (
            <p className='text-muted-foreground text-sm'>
              {t('{{count}} historical recharge amounts need verification', {
                count: data.summary.unverifiedRechargeCount,
              })}
            </p>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>{t('Invited Customers')}</CardTitle>
        </CardHeader>
        <CardContent>
          {!data.items.length && (
            <EmptyState
              icon={Users}
              title={t('No invited customers')}
              bordered
            />
          )}
          {data.items.length > 0 && (
            <>
              <div className='hidden overflow-x-auto rounded-md border md:block'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('Customer')}</TableHead>
                      <TableHead>{t('Invited At')}</TableHead>
                      <TableHead>{t('First Paid Recharge')}</TableHead>
                      <TableHead>{t('Confirmed Credited Recharges')}</TableHead>
                      <TableHead>{t('Rebate Amounts')}</TableHead>
                      <TableHead>{t('Details')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {data.items.map((item) => (
                      <TableRow key={item.userId}>
                        <TableCell>
                          <span className='font-medium'>{item.userName}</span>
                          <div className='mt-1'>
                            <Badge
                              variant={item.effective ? 'default' : 'outline'}
                            >
                              {item.effective
                                ? t('Effective')
                                : t('Not Yet Effective')}
                            </Badge>
                          </div>
                        </TableCell>
                        <TableCell>
                          {formatInvitationDate(item.invitedAt)}
                        </TableCell>
                        <TableCell>
                          {formatInvitationDate(item.firstRechargeAt)}
                        </TableCell>
                        <TableCell className='tabular-nums'>
                          {formatAmount(item.rechargeAmount)}
                          {item.unverifiedRechargeCount > 0 && (
                            <p className='text-muted-foreground mt-1 text-xs'>
                              {t(
                                '{{count}} historical recharge amounts need verification',
                                { count: item.unverifiedRechargeCount }
                              )}
                            </p>
                          )}
                        </TableCell>
                        <TableCell className='min-w-72'>
                          <SalesRebateAmounts
                            rebates={item.rebates}
                            currency={data.creditCurrency}
                            compact
                          />
                        </TableCell>
                        <TableCell>
                          <Button
                            variant='outline'
                            size='sm'
                            onClick={() => setCustomer(item)}
                          >
                            {t('Recharge Details')}
                          </Button>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
              <div className='space-y-3 md:hidden'>
                {data.items.map((item) => (
                  <article
                    key={item.userId}
                    className='space-y-3 rounded-md border p-3'
                  >
                    <div className='flex items-center justify-between gap-3'>
                      <span className='font-medium'>{item.userName}</span>
                      <Badge variant={item.effective ? 'default' : 'outline'}>
                        {item.effective
                          ? t('Effective')
                          : t('Not Yet Effective')}
                      </Badge>
                    </div>
                    <dl className='grid grid-cols-2 gap-2 text-sm'>
                      <dt>{t('Invited At')}</dt>
                      <dd className='text-right'>
                        {formatInvitationDate(item.invitedAt)}
                      </dd>
                      <dt>{t('First Paid Recharge')}</dt>
                      <dd className='text-right'>
                        {formatInvitationDate(item.firstRechargeAt)}
                      </dd>
                      <dt>{t('Confirmed Credited Recharges')}</dt>
                      <dd className='text-right tabular-nums'>
                        {formatAmount(item.rechargeAmount)}
                      </dd>
                    </dl>
                    {item.unverifiedRechargeCount > 0 && (
                      <p className='text-muted-foreground text-xs'>
                        {t(
                          '{{count}} historical recharge amounts need verification',
                          { count: item.unverifiedRechargeCount }
                        )}
                      </p>
                    )}
                    <SalesRebateAmounts
                      rebates={item.rebates}
                      currency={data.creditCurrency}
                      compact
                    />
                    <Button
                      className='w-full'
                      variant='outline'
                      size='sm'
                      onClick={() => setCustomer(item)}
                    >
                      {t('Recharge Details')}
                    </Button>
                  </article>
                ))}
              </div>
            </>
          )}
          <PaginationControls
            page={page}
            pageSize={PAGE_SIZE}
            total={data.total}
            onPageChange={setPage}
          />
        </CardContent>
      </Card>
      {customer && (
        <SalesCustomerRecharges
          key={customer.userId}
          customer={customer}
          currency={data.creditCurrency}
          onClose={() => setCustomer(null)}
        />
      )}
    </div>
  )
}

const SalesRebateAmounts = (props: {
  rebates: SalesRebates
  currency: string
  compact?: boolean
}) => {
  const { t } = useTranslation()
  const amounts = [
    [t('Cooling Down'), props.rebates.cooling],
    [t('Available to Request'), props.rebates.available],
    [t('Awaiting Review'), props.rebates.pendingApproval],
    [t('Approved, Awaiting Issue'), props.rebates.approved],
    [t('Issue Confirmed'), props.rebates.issued],
    [t('Locked Rebate'), props.rebates.locked],
    [t('Closed Rebate'), props.rebates.closed],
  ] as const
  return (
    <div className='space-y-2'>
      <dl
        className={
          props.compact
            ? 'grid grid-cols-2 gap-x-4 gap-y-2 text-xs'
            : 'grid grid-cols-2 gap-4 border-t pt-4 sm:grid-cols-3 lg:grid-cols-5'
        }
      >
        {amounts.map(([label, amount]) => (
          <div key={label}>
            <dt className='text-muted-foreground'>{label}</dt>
            <dd className='font-semibold tabular-nums'>
              {formatSalesAmount(amount, props.currency)}
            </dd>
          </div>
        ))}
      </dl>
      {props.rebates.pendingCalculationCount > 0 && (
        <p className='text-muted-foreground text-xs'>
          {t('{{count}} recharges awaiting rebate calculation', {
            count: props.rebates.pendingCalculationCount,
          })}
        </p>
      )}
    </div>
  )
}
