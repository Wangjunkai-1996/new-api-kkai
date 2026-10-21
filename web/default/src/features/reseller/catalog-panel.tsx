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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { handleServerError } from '@/lib/handle-server-error'
import { getServerErrorSources } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import {
  getResellerPrices,
  resellerQueryKey,
  updateResellerPrices,
  type GroupPrice,
} from './api'

function GroupRatioRow(props: { price: GroupPrice }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [markup, setMarkup] = useState(props.price.markup_percent)
  const value = Number(markup)
  const salesRatio = Number(props.price.platform_multiplier) * (1 + value / 100)
  const unchanged =
    markup === props.price.markup_percent ||
    (props.price.markup_percent.trim() !== '' &&
      value === Number(props.price.markup_percent))
  const legacy = props.price.pricing_mode === 'legacy'
  const valid =
    markup.trim() !== '' &&
    Number.isFinite(value) &&
    value >= 0 &&
    value <= 1_000_000 &&
    Number.isFinite(salesRatio) &&
    salesRatio <= 1_000_000
  let status = props.price.inherited ? t('Inherited') : t('Custom')
  if (legacy) status = t('Legacy fixed ratio')
  const mutation = useMutation({
    mutationFn: (percent: string | null) => {
      const group = {
        sales_group: props.price.group,
        expected_version: props.price.version,
      }
      if (percent === null) {
        return updateResellerPrices([
          {
            ...group,
            convert_legacy: true,
            expected_platform_multiplier: props.price.platform_multiplier,
          },
        ])
      }
      return updateResellerPrices([{ ...group, markup_percent: percent }])
    },
    onSuccess: async () => {
      await Promise.all([
        client.invalidateQueries({
          queryKey: resellerQueryKey(userId, 'catalog'),
        }),
        client.invalidateQueries({ queryKey: ['pricing'] }),
      ])
      toast.success(t('Settings updated successfully'))
    },
    onError: (error) => {
      handleServerError(error)
      if (
        getServerErrorSources(error).some(
          (source) => source.code === 'TENANT_PRICE_VERSION_CONFLICT'
        )
      ) {
        void client.invalidateQueries({
          queryKey: resellerQueryKey(userId, 'catalog'),
        })
      }
    },
  })
  return (
    <TableRow>
      <TableCell className='font-medium'>{props.price.group}</TableCell>
      <TableCell>{props.price.platform_multiplier}</TableCell>
      <TableCell>
        <Input
          className='max-w-40 min-w-24'
          aria-label={t('Markup for {{group}} (%)', {
            group: props.price.group,
          })}
          inputMode='decimal'
          value={markup}
          disabled={mutation.isPending}
          aria-invalid={!valid}
          onChange={(event) => setMarkup(event.target.value)}
        />
        {!valid && (
          <p className='text-destructive mt-1 text-sm'>
            {t(
              'Markup must be between 0 and {{maximum}}%, and the sales ratio must not exceed {{ratioMaximum}}.',
              { maximum: '1000000', ratioMaximum: '1000000' }
            )}
          </p>
        )}
        {legacy && (
          <p className='text-muted-foreground mt-1 max-w-64 text-sm'>
            {t(
              'Save the markup to make this group follow platform price changes.'
            )}
          </p>
        )}
      </TableCell>
      <TableCell>
        {unchanged
          ? props.price.sales_multiplier
          : valid && Number(salesRatio.toPrecision(15)).toString()}
      </TableCell>
      <TableCell>{status}</TableCell>
      <TableCell>
        <div className='flex flex-wrap gap-2'>
          <Button
            disabled={mutation.isPending || !valid || (unchanged && !legacy)}
            onClick={() =>
              mutation.mutate(legacy && unchanged ? null : markup.trim())
            }
          >
            {t('Save')}
          </Button>
          <Button
            variant='outline'
            disabled={mutation.isPending || props.price.inherited}
            onClick={() => mutation.mutate('0')}
          >
            {t('Reset to default')}
          </Button>
        </div>
      </TableCell>
    </TableRow>
  )
}

export function CatalogPanel() {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const query = useQuery({
    queryKey: resellerQueryKey(userId, 'catalog'),
    queryFn: getResellerPrices,
  })
  if (query.isPending) return <Skeleton className='h-80 w-full' />
  if (query.isError) {
    return (
      <ErrorState
        title={t('Failed to load data')}
        description={query.error.message}
        onRetry={() => void query.refetch()}
      />
    )
  }
  return (
    <div className='space-y-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Models follow the platform. Set a markup percentage for each group; sales ratios automatically follow platform price changes.'
        )}
      </p>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Group')}</TableHead>
            <TableHead>{t('Platform group ratio')}</TableHead>
            <TableHead>{t('Markup (%)')}</TableHead>
            <TableHead>{t('Effective sales ratio')}</TableHead>
            <TableHead>{t('Status')}</TableHead>
            <TableHead>{t('Actions')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {query.data.map((price) => (
            <GroupRatioRow
              key={`${price.group}:${price.version}:${price.markup_percent}:${price.sales_multiplier}:${price.platform_multiplier}`}
              price={price}
            />
          ))}
          {query.data.length === 0 && (
            <TableRow>
              <TableCell colSpan={6}>{t('No data available')}</TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  )
}
