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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useFieldArray, useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { getCurrencyLabel } from '@/lib/currency'
import { parseQuotaFromDollars, quotaUnitsToDollars } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import { publishMembershipCatalog } from '../api'
import { membershipCatalogSchema } from '../form'
import type { MembershipCatalog } from '../types'
import { MembershipDiscountEditor } from './discount-editor'
import { MembershipModelLimitEditor } from './model-limit-editor'

export function MembershipCatalogEditor(props: { catalog: MembershipCatalog }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [confirmation, setConfirmation] = useState<MembershipCatalog | null>(
    null
  )
  const schema = membershipCatalogSchema(t)
  const form = useForm<MembershipCatalog>({
    resolver: zodResolver(schema),
    defaultValues: props.catalog,
  })
  const tierFields = useFieldArray({
    control: form.control,
    name: 'tiers',
    keyName: 'fieldKey',
  })
  // Field-array changes can render before useWatch receives the updated values.
  const catalog = {
    ...useWatch({ control: form.control }),
    ...form.getValues(),
  } as MembershipCatalog
  const publish = useMutation({
    mutationFn: (next: MembershipCatalog) =>
      publishMembershipCatalog(next, props.catalog.release_id),
    onSuccess: async (next) => {
      setConfirmation(null)
      client.setQueryData(['membership-admin', 'config'], next)
      await client.invalidateQueries({ queryKey: ['membership'] })
      toast.success(t('Membership configuration published'))
    },
    onError: (error) =>
      handleServerError(
        error,
        t(
          'Could not publish membership configuration. Reload before retrying if it changed elsewhere.'
        )
      ),
  })
  const errors = form.formState.errors

  return (
    <form onSubmit={form.handleSubmit(setConfirmation)} className='space-y-6'>
      <div className='flex flex-wrap items-center justify-between gap-4 rounded-xl border p-4'>
        <label className='flex items-center gap-2 font-medium'>
          <Checkbox
            checked={catalog.sales_enabled}
            onCheckedChange={(checked) =>
              form.setValue('sales_enabled', checked === true, {
                shouldDirty: true,
              })
            }
          />
          {t('Enable membership sales')}
        </label>
        <p className='text-muted-foreground text-xs'>
          {t('Published version')}: {props.catalog.release_id}
        </p>
      </div>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Memberships last 30 days, never reset quota, and accept one-time external payments only.'
        )}
      </p>
      <div className='grid gap-5 xl:grid-cols-3'>
        {tierFields.fields.map((field, index) => {
          const tier = catalog.tiers[index]
          const existing = props.catalog.tiers.some(
            (item) => item.id === field.id
          )
          const error = errors.tiers?.[index]
          return (
            <section
              key={field.fieldKey}
              className='bg-card space-y-4 rounded-2xl border p-5'
            >
              <div className='flex items-center justify-between gap-2'>
                <h3 className='font-semibold'>
                  {tier?.name || t('New membership tier')}
                </h3>
                <label className='flex items-center gap-2 text-xs'>
                  <Checkbox
                    checked={tier?.enabled}
                    onCheckedChange={(checked) =>
                      form.setValue(
                        `tiers.${index}.enabled`,
                        checked === true,
                        { shouldDirty: true }
                      )
                    }
                  />
                  {t('On sale')}
                </label>
              </div>
              <div className='space-y-1.5'>
                <label
                  className='text-sm'
                  htmlFor={`tier-name-${field.fieldKey}`}
                >
                  {t('Plan Title')}
                </label>
                <Input
                  id={`tier-name-${field.fieldKey}`}
                  {...form.register(`tiers.${index}.name`)}
                  aria-invalid={!!error?.name}
                />
                {error?.name && (
                  <p className='text-destructive text-xs'>
                    {error.name.message}
                  </p>
                )}
              </div>
              <div className='space-y-1.5'>
                <label
                  className='text-sm'
                  htmlFor={`tier-id-${field.fieldKey}`}
                >
                  {t('Tier identifier')}
                </label>
                <Input
                  id={`tier-id-${field.fieldKey}`}
                  {...form.register(`tiers.${index}.id`)}
                  readOnly={existing}
                  aria-invalid={!!error?.id}
                />
                {error?.id && (
                  <p className='text-destructive text-xs'>{error.id.message}</p>
                )}
              </div>
              <div className='grid grid-cols-2 gap-3'>
                <div className='space-y-1.5'>
                  <label
                    className='text-sm'
                    htmlFor={`tier-rank-${field.fieldKey}`}
                  >
                    {t('Membership rank')}
                  </label>
                  <Input
                    id={`tier-rank-${field.fieldKey}`}
                    type='number'
                    min='1'
                    {...form.register(`tiers.${index}.rank`, {
                      valueAsNumber: true,
                    })}
                    aria-invalid={!!error?.rank}
                  />
                  {error?.rank && (
                    <p className='text-destructive text-xs'>
                      {error.rank.message}
                    </p>
                  )}
                </div>
                <div className='space-y-1.5'>
                  <label
                    className='text-sm'
                    htmlFor={`tier-style-${field.fieldKey}`}
                  >
                    {t('Visual style')}
                  </label>
                  <select
                    id={`tier-style-${field.fieldKey}`}
                    {...form.register(`tiers.${index}.style`)}
                    className='bg-background h-9 w-full rounded-md border px-2 text-sm'
                  >
                    <option value='gold'>{t('Champagne gold')}</option>
                    <option value='diamond'>{t('Ice blue')}</option>
                    <option value='king'>{t('Purple and gold')}</option>
                  </select>
                </div>
              </div>
              <div className='grid grid-cols-2 gap-3'>
                <div className='space-y-1.5'>
                  <label
                    className='text-sm'
                    htmlFor={`tier-price-${field.fieldKey}`}
                  >
                    {t('Price')}
                  </label>
                  <Input
                    id={`tier-price-${field.fieldKey}`}
                    type='number'
                    min='0'
                    step='0.01'
                    value={
                      Number.isFinite(tier.price_minor)
                        ? tier.price_minor / 100
                        : ''
                    }
                    onChange={(event) =>
                      form.setValue(
                        `tiers.${index}.price_minor`,
                        Math.round(Number(event.target.value) * 100),
                        { shouldDirty: true }
                      )
                    }
                    aria-invalid={!!error?.price_minor}
                  />
                  {error?.price_minor && (
                    <p className='text-destructive text-xs'>
                      {error.price_minor.message}
                    </p>
                  )}
                </div>
                <div className='space-y-1.5'>
                  <label
                    className='text-sm'
                    htmlFor={`tier-currency-${field.fieldKey}`}
                  >
                    {t('Currency')}
                  </label>
                  <select
                    id={`tier-currency-${field.fieldKey}`}
                    {...form.register(`tiers.${index}.currency`)}
                    className='bg-background h-9 w-full rounded-md border px-2 text-sm'
                  >
                    <option value='USD'>USD</option>
                    <option value='CNY'>CNY</option>
                  </select>
                  {error?.currency && (
                    <p className='text-destructive text-xs'>
                      {error.currency.message}
                    </p>
                  )}
                </div>
              </div>
              <div className='space-y-1.5'>
                <label
                  className='text-sm'
                  htmlFor={`tier-quota-${field.fieldKey}`}
                >
                  {t('Included quota')} ({getCurrencyLabel()})
                </label>
                <Input
                  id={`tier-quota-${field.fieldKey}`}
                  type='number'
                  min='0'
                  step='0.01'
                  value={
                    Number.isFinite(tier.quota)
                      ? quotaUnitsToDollars(tier.quota)
                      : ''
                  }
                  onChange={(event) =>
                    form.setValue(
                      `tiers.${index}.quota`,
                      parseQuotaFromDollars(Number(event.target.value)),
                      { shouldDirty: true }
                    )
                  }
                  aria-invalid={!!error?.quota}
                />
                {error?.quota && (
                  <p className='text-destructive text-xs'>
                    {error.quota.message}
                  </p>
                )}
              </div>
              <div className='space-y-1.5'>
                <label
                  className='text-sm'
                  htmlFor={`tier-creem-${field.fieldKey}`}
                >
                  {t('Creem one-time product ID')}
                </label>
                <Input
                  id={`tier-creem-${field.fieldKey}`}
                  {...form.register(`tiers.${index}.payment_products.creem`)}
                />
              </div>
              <div className='space-y-1.5'>
                <label
                  className='text-sm'
                  htmlFor={`tier-pancake-${field.fieldKey}`}
                >
                  {t('Pancake one-time product ID')}
                </label>
                <Input
                  id={`tier-pancake-${field.fieldKey}`}
                  {...form.register(
                    `tiers.${index}.payment_products.waffo_pancake`
                  )}
                />
              </div>
              {!existing && (
                <Button
                  variant='ghost'
                  type='button'
                  onClick={() => tierFields.remove(index)}
                >
                  {t('Remove')}
                </Button>
              )}
            </section>
          )
        })}
      </div>
      <Button
        type='button'
        variant='outline'
        onClick={() =>
          tierFields.append({
            id: '',
            name: '',
            rank: Math.max(0, ...catalog.tiers.map((tier) => tier.rank)) + 1,
            style: 'gold',
            price_minor: 0,
            currency: 'USD',
            quota: 0,
            enabled: false,
            discounts: {},
            payment_products: {},
          })
        }
      >
        {t('Add membership tier')}
      </Button>
      <MembershipModelLimitEditor
        value={catalog.model_limits ?? {}}
        onChange={(limits) =>
          form.setValue('model_limits', limits, { shouldDirty: true })
        }
        disabled={publish.isPending}
      />
      <MembershipDiscountEditor
        catalog={catalog}
        publishedGroups={props.catalog.groups
          .filter((group) => group.published)
          .map((group) => group.key)}
        onDiscountChange={(index, group, value) =>
          form.setValue(
            `tiers.${index}.discounts`,
            { ...catalog.tiers[index].discounts, [group]: value },
            { shouldDirty: true }
          )
        }
        onPublishedChange={(index, value) =>
          form.setValue(`groups.${index}.published`, value, {
            shouldDirty: true,
          })
        }
        disabled={publish.isPending}
      />
      {Object.keys(errors).length > 0 && (
        <Alert variant='destructive'>
          <AlertDescription>
            {t('Check the tier fields and group discounts before publishing.')}
          </AlertDescription>
        </Alert>
      )}
      {publish.isError && (
        <Alert variant='destructive'>
          <AlertDescription>
            {t(
              'Could not publish membership configuration. Reload before retrying if it changed elsewhere.'
            )}
          </AlertDescription>
        </Alert>
      )}
      <Button
        type='submit'
        disabled={publish.isPending || !form.formState.isDirty}
      >
        {t('Publish membership configuration')}
      </Button>
      {confirmation && (
        <Dialog
          open
          onOpenChange={(open) => {
            if (!open && !publish.isPending) setConfirmation(null)
          }}
          title={t('Publish membership configuration')}
          description={t('Review how this change affects membership benefits.')}
          footer={
            <Button
              onClick={() => publish.mutate(confirmation)}
              disabled={publish.isPending}
            >
              {publish.isPending
                ? t('Publishing...')
                : t('Confirm publication')}
            </Button>
          }
        >
          <div className='space-y-3 text-sm leading-relaxed'>
            <p>
              {t(
                'Changes to existing group discounts apply only to new purchases and upgrades. Existing locked discounts remain unchanged.'
              )}
            </p>
            <p>
              {t(
                'First publication of a group grants its published benefits to eligible existing members as well.'
              )}
            </p>
            <p>{t('Disabling sales does not cancel existing memberships.')}</p>
          </div>
        </Dialog>
      )}
    </form>
  )
}
