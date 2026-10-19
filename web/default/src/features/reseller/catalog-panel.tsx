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
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
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
import { useAuthStore } from '@/stores/auth-store'

import {
  getResellerCatalog,
  getResellerPrices,
  getResellerSupply,
  resellerQueryKey,
  updateResellerCatalog,
  updateResellerPrices,
  type CatalogEntry,
  type PriceVersion,
  type SupplierEntry,
} from './api'
import { formatMinor } from './money'

function CatalogForm(props: {
  entries: CatalogEntry[]
  prices: PriceVersion[]
  supply: SupplierEntry[]
}) {
  const { t, i18n } = useTranslation()
  const client = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const groups = [...new Set(props.supply.map((entry) => entry.sales_group))]
  const [group, setGroup] = useState(
    props.entries[0]?.sales_group ?? groups[0] ?? ''
  )
  const [selected, setSelected] = useState(
    props.entries
      .filter((entry) => entry.status === 'enabled')
      .map((entry) => entry.model)
  )
  const [multiplier, setMultiplier] = useState(
    props.prices.find((price) => price.group === group)?.sales_multiplier ??
      '1.1'
  )
  const available = props.supply.filter((entry) => entry.sales_group === group)
  const selectedSupply = available.filter((entry) =>
    selected.includes(entry.model)
  )
  const minimum = Math.max(
    1,
    ...selectedSupply.map((entry) =>
      Number(entry.minimum_multiplier ?? '1.0162601627')
    )
  )
  const multiplierValue = Number(multiplier)
  const validMultiplier =
    Number.isFinite(multiplierValue) &&
    multiplierValue >= minimum &&
    multiplierValue <= 1_000_000
  const catalogMutation = useMutation({
    mutationFn: updateResellerCatalog,
    onSuccess: async () => {
      await client.invalidateQueries({
        queryKey: resellerQueryKey(userId, 'catalog'),
      })
      await client.invalidateQueries({ queryKey: ['pricing'] })
      toast.success(t('Settings updated successfully'))
    },
    onError: (error) => handleServerError(error),
  })
  const pricesMutation = useMutation({
    mutationFn: updateResellerPrices,
    onSuccess: async () => {
      await Promise.all([
        client.invalidateQueries({
          queryKey: resellerQueryKey(userId, 'catalog'),
        }),
        client.invalidateQueries({ queryKey: ['pricing'] }),
      ])
      toast.success(t('Settings updated successfully'))
    },
    onError: (error) => handleServerError(error),
  })

  return (
    <div className='space-y-6'>
      <FieldGroup className='max-w-2xl'>
        <Field>
          <FieldLabel htmlFor='reseller-sales-group'>
            {t('Default Sales Group')}
          </FieldLabel>
          <NativeSelect
            id='reseller-sales-group'
            value={group}
            onChange={(event) => {
              setGroup(event.target.value)
              setSelected(
                props.entries
                  .filter(
                    (entry) =>
                      entry.sales_group === event.target.value &&
                      entry.status === 'enabled'
                  )
                  .map((entry) => entry.model)
              )
            }}
          >
            {groups.map((name) => (
              <NativeSelectOption value={name} key={name}>
                {name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        <Field>
          <FieldLabel htmlFor='reseller-multiplier'>
            {t('Sales Multiplier')}
          </FieldLabel>
          <Input
            id='reseller-multiplier'
            inputMode='decimal'
            value={multiplier}
            aria-invalid={!validMultiplier}
            onChange={(event) => setMultiplier(event.target.value)}
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              'Minimum multiplier: {{value}}. Includes the 1.6% payment fee.',
              { value: minimum.toFixed(8) }
            )}
          </p>
        </Field>
      </FieldGroup>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Enabled')}</TableHead>
            <TableHead>{t('Model')}</TableHead>
            <TableHead>{t('Supply Price')}</TableHead>
            <TableHead>{t('Price Unit')}</TableHead>
            <TableHead>{t('Current Multiplier')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {available.map((entry) => (
            <TableRow key={entry.model}>
              <TableCell>
                <Checkbox
                  aria-label={t('Select model {{model}}', {
                    model: entry.model,
                  })}
                  checked={selected.includes(entry.model)}
                  onCheckedChange={(checked) =>
                    setSelected((current) =>
                      checked
                        ? [...new Set([...current, entry.model])]
                        : current.filter((name) => name !== entry.model)
                    )
                  }
                />
              </TableCell>
              <TableCell>{entry.model}</TableCell>
              <TableCell>
                {formatMinor(entry.supplier_price_minor, i18n.language)}
              </TableCell>
              <TableCell>
                {entry.price_unit === 'per_call'
                  ? t('Per call')
                  : t('Per million input tokens')}
              </TableCell>
              <TableCell>
                {props.prices.find(
                  (price) =>
                    price.group === group && price.model === entry.model
                )?.sales_multiplier ?? '—'}
              </TableCell>
            </TableRow>
          ))}
          {available.length === 0 && (
            <TableRow>
              <TableCell colSpan={5}>{t('No data available')}</TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
      <div className='flex flex-wrap gap-3'>
        <Button
          disabled={
            selectedSupply.length === 0 ||
            catalogMutation.isPending ||
            pricesMutation.isPending
          }
          onClick={() =>
            catalogMutation.mutate(
              selectedSupply.map((entry, index) => ({
                sales_group: group,
                model: entry.model,
                status: 'enabled',
                is_default: index === 0,
                sort_order: index,
              }))
            )
          }
        >
          {t('Save Model Selection')}
        </Button>
        <Button
          variant='outline'
          disabled={
            !validMultiplier ||
            selectedSupply.length === 0 ||
            pricesMutation.isPending ||
            catalogMutation.isPending
          }
          onClick={() =>
            pricesMutation.mutate(
              selectedSupply.map((entry) => ({
                sales_group: group,
                model: entry.model,
                sale_multiplier: multiplier,
                expected_version:
                  props.prices.find(
                    (price) =>
                      price.group === group && price.model === entry.model
                  )?.version ?? 0,
              }))
            )
          }
        >
          {t('Save Sales Multiplier')}
        </Button>
      </div>
    </div>
  )
}

export function CatalogPanel() {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const query = useQuery({
    queryKey: resellerQueryKey(userId, 'catalog'),
    queryFn: async () => {
      const [entries, prices, supply] = await Promise.all([
        getResellerCatalog(),
        getResellerPrices(),
        getResellerSupply(),
      ])
      return { entries, prices, supply }
    },
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
  return <CatalogForm key={query.dataUpdatedAt} {...query.data} />
}
