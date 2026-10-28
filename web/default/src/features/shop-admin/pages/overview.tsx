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
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import type { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  MoneyText,
  ShopErrorNotice,
  ShopHeading,
  ShopLoading,
  ShopTime,
} from '@/features/shop/components/common'
import { useShopAction, useShopQuery } from '@/features/shop/hooks/use-shop'
import { parseShopInteger } from '@/features/shop/lib'
import { overviewSchema, settingsSchema } from '@/features/shop/schemas'

export function AdminOverviewPage() {
  const { t } = useTranslation()
  const overview = useShopQuery('/api/admin/shop/overview', overviewSchema)
  const settings = useShopQuery('/api/admin/shop/settings', settingsSchema)
  return (
    <>
      <ShopHeading
        eyebrow='KK SHOP'
        title={t('Shop operations')}
        description={t(
          'Orders, stock and unresolved responsibilities in one workspace.'
        )}
      />
      <ShopErrorNotice
        error={overview.error ?? settings.error}
        retry={() => {
          void overview.refetch()
          void settings.refetch()
        }}
      />
      {overview.isPending && <ShopLoading />}
      {overview.data && (
        <div className='shop-stats'>
          {[
            [t('Orders'), overview.data.orders],
            [t('Awaiting delivery'), overview.data.pending_delivery],
            [t('After-sales support'), overview.data.pending_aftersales],
            [t('Payment exceptions'), overview.data.unresolved_receipts],
            [t('Unresolved refunds'), overview.data.unresolved_refunds],
          ].map(([label, value]) => (
            <div key={label} className='shop-panel'>
              <span className='shop-muted'>{label}</span>
              <strong>{value}</strong>
            </div>
          ))}
          <div className='shop-panel'>
            <p className='shop-muted text-sm'>
              <ShopTime value={overview.data.from} /> –{' '}
              <ShopTime value={overview.data.to} />
            </p>
            <span>{t('Received')}</span>
            <MoneyText money={overview.data.received} />
            <span>{t('Refunded')}</span>
            <MoneyText money={overview.data.refunded} />
          </div>
        </div>
      )}
      <div className='shop-actions my-6'>
        <Link to='/shop-admin/orders'>{t('Open order queue')}</Link>
        <Link to='/shop-admin/receipts'>{t('Review payment exceptions')}</Link>
        <Link to='/shop-admin/refunds'>{t('Review refunds')}</Link>
      </div>
      {settings.data && (
        <SettingsForm key={settings.data.version} settings={settings.data} />
      )}
    </>
  )
}

export function AdminSettingsPage() {
  const settings = useShopQuery('/api/admin/shop/settings', settingsSchema)
  if (settings.isPending) return <ShopLoading />
  if (!settings.data) {
    return (
      <ShopErrorNotice
        error={settings.error}
        retry={() => void settings.refetch()}
      />
    )
  }
  return <SettingsForm key={settings.data.version} settings={settings.data} />
}

function SettingsForm(props: { settings: z.infer<typeof settingsSchema> }) {
  const { t } = useTranslation()
  const action = useShopAction()
  const data = props.settings
  const [validation, setValidation] = useState('')
  const form = useForm<Record<string, string>>({
    defaultValues: {
      provider: data.provider ?? '',
      provider_account_ref: data.provider_account_ref ?? '',
      environment: data.environment ?? '',
      currency: data.currency ?? '',
      reservation: String(data.reservation_minutes ?? ''),
      pending: String(data.pending_order_limit ?? ''),
      min: data.min_amount_minor ?? '',
      max: data.max_amount_minor ?? '',
      enabled: String(data.new_orders_enabled),
      reason: '',
      ...Object.fromEntries(
        Object.entries(data.retention_policy ?? {}).map(([key, value]) => [
          key,
          String(value),
        ])
      ),
      ...Object.fromEntries(
        Object.entries(data.reminder_policy ?? {}).map(([key, value]) => [
          key,
          String(value),
        ])
      ),
    },
  })
  const enabled = useWatch({ control: form.control, name: 'enabled' })
  const retention = [
    'secret_after_delivery_seconds',
    'buyer_input_after_closure_seconds',
    'evidence_after_settlement_seconds',
    'audit_after_settlement_seconds',
    'idempotency_after_closure_seconds',
  ] as const
  const reminders = [
    'payment_unknown_seconds',
    'delivery_exception_seconds',
    'refund_verification_seconds',
  ] as const
  const fields = [
    ['provider', t('Payment provider')],
    ['provider_account_ref', t('Provider account reference')],
    ['environment', t('Payment environment')],
    ['currency', t('Currency')],
    ['reservation', t('Reservation minutes')],
    ['pending', t('Unpaid order limit')],
    ['min', t('Minimum amount in minor units')],
    ['max', t('Maximum amount in minor units')],
    [retention[0], t('Delivery secret retention in seconds')],
    [retention[1], t('Buyer information retention in seconds')],
    [retention[2], t('Payment evidence retention in seconds')],
    [retention[3], t('Audit retention in seconds')],
    [retention[4], t('Idempotency retention in seconds')],
    [reminders[0], t('Unknown payment reminder in seconds')],
    [reminders[1], t('Delivery exception reminder in seconds')],
    [reminders[2], t('Refund verification reminder in seconds')],
    ['reason', t('Reason')],
  ]
  return (
    <section className='shop-panel'>
      <h2>{t('Store settings')}</h2>
      <p className='shop-muted'>
        {t(
          'Complete the operating rules before enabling new purchases. Existing orders remain accessible when sales are paused.'
        )}
      </p>
      <p className='shop-muted'>
        {t('Available payment methods use the existing payment configuration.')}{' '}
        {data.payment_methods.join(', ') || t('Not configured')}
      </p>
      <form
        onSubmit={form.handleSubmit(async (values) => {
          setValidation('')
          try {
            const minimum = BigInt(values.min)
            const maximum = BigInt(values.max)
            if (
              !/^(0|[1-9]\d*)$/.test(values.min) ||
              !/^(0|[1-9]\d*)$/.test(values.max) ||
              maximum > 9223372036854775807n ||
              minimum > maximum ||
              !/^[A-Z]{3}$/.test(values.currency)
            ) {
              throw new Error()
            }
            await action.run(
              '/api/admin/shop/settings',
              settingsSchema,
              {
                ...(data.version !== '0'
                  ? { expected_version: data.version }
                  : {}),
                new_orders_enabled: values.enabled === 'true',
                provider: values.provider,
                provider_account_ref: values.provider_account_ref,
                environment: values.environment,
                currency: values.currency,
                reservation_minutes: parseShopInteger(values.reservation, 1),
                pending_order_limit: parseShopInteger(values.pending, 1),
                min_amount_minor: values.min,
                max_amount_minor: values.max,
                retention_policy: Object.fromEntries(
                  retention.map((key) => [
                    key,
                    parseShopInteger(values[key], 1),
                  ])
                ),
                reminder_policy: Object.fromEntries(
                  reminders.map((key) => [
                    key,
                    parseShopInteger(values[key], 1),
                  ])
                ),
                reason: values.reason,
              },
              { method: 'PATCH' }
            )
          } catch {
            setValidation(t('Check numeric limits, currency and date values.'))
          }
        })}
      >
        <FieldGroup className='grid sm:grid-cols-2'>
          {fields.map(([name, label]) => (
            <Field key={name}>
              <FieldLabel htmlFor={`setting-${name}`}>{label}</FieldLabel>
              <Input
                id={`setting-${name}`}
                required
                maxLength={500}
                {...form.register(name)}
              />
            </Field>
          ))}
          <Field orientation='horizontal'>
            <input
              id='shop-sales'
              type='checkbox'
              checked={enabled === 'true'}
              onChange={(event) =>
                form.setValue('enabled', String(event.target.checked))
              }
              className='size-5'
            />
            <FieldLabel htmlFor='shop-sales'>
              {t('Enable new purchases')}
            </FieldLabel>
          </Field>
          <Button type='submit' disabled={action.pending}>
            {t('Save settings')}
          </Button>
        </FieldGroup>
        {validation && (
          <p role='alert' className='text-destructive'>
            {validation}
          </p>
        )}
        <ShopErrorNotice error={action.error} />
      </form>
    </section>
  )
}
