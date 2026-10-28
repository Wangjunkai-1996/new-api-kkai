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
import { toast } from 'sonner'
import type { z } from 'zod'

import { Button } from '@/components/ui/button'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  MoneyText,
  ShopErrorNotice,
  ShopHeading,
  ShopLoading,
  ShopTime,
} from '@/features/shop/components/common'
import { useShopAction, useShopQuery } from '@/features/shop/hooks/use-shop'
import { parsePrice, parseShopInteger, priceInput } from '@/features/shop/lib'
import { overviewSchema, settingsSchema } from '@/features/shop/schemas'

export function AdminOverviewPage() {
  const { t } = useTranslation()
  const overview = useShopQuery('/api/admin/shop/overview', overviewSchema)
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
        error={overview.error}
        retry={() => void overview.refetch()}
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
  const currency = data.currency ?? 'CNY'
  const exponent = data.currency_exponent ?? 2
  const [validation, setValidation] = useState('')
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [useCurrentAccount, setUseCurrentAccount] = useState(false)
  const paymentReady = useCurrentAccount
    ? data.payment_config_ready
    : data.payment_ready
  let paymentStatus = t('Payment setup incomplete')
  if (data.payment_ready) {
    paymentStatus = t('Ready to accept payments')
  } else if (useCurrentAccount && data.payment_config_ready) {
    paymentStatus = t('Ready after saving')
  }
  const form = useForm<Record<string, string>>({
    defaultValues: {
      reservation: String(data.reservation_minutes ?? ''),
      pending: String(data.pending_order_limit ?? ''),
      min: priceInput({
        amount_minor: data.min_amount_minor ?? '0',
        currency,
        exponent,
      }),
      max: priceInput({
        amount_minor: data.max_amount_minor ?? '0',
        currency,
        exponent,
      }),
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
    [
      'secret_after_delivery_seconds',
      t('Delivery secret retention in seconds'),
    ],
    [
      'buyer_input_after_closure_seconds',
      t('Buyer information retention in seconds'),
    ],
    [
      'evidence_after_settlement_seconds',
      t('Payment evidence retention in seconds'),
    ],
    ['audit_after_settlement_seconds', t('Audit retention in seconds')],
    [
      'idempotency_after_closure_seconds',
      t('Idempotency retention in seconds'),
    ],
  ] as const
  const reminders = [
    ['payment_unknown_seconds', t('Unknown payment reminder in seconds')],
    ['delivery_exception_seconds', t('Delivery exception reminder in seconds')],
    [
      'refund_verification_seconds',
      t('Refund verification reminder in seconds'),
    ],
  ] as const
  return (
    <>
      <ShopHeading
        eyebrow='KK SHOP'
        title={t('Store settings')}
        description={t(
          'Payments reuse your existing configuration. Operating rules have ready-to-use defaults.'
        )}
      />
      <form
        className='max-w-4xl space-y-5'
        onSubmit={form.handleSubmit(async (values) => {
          setValidation('')
          try {
            const minimum = parsePrice(values.min, currency, exponent)
            const maximum = parsePrice(values.max, currency, exponent)
            if (
              !minimum ||
              !maximum ||
              BigInt(minimum.amount_minor) < 1n ||
              BigInt(minimum.amount_minor) > BigInt(maximum.amount_minor)
            ) {
              throw new Error()
            }
            const updated = await action.run(
              '/api/admin/shop/settings',
              settingsSchema,
              {
                ...(data.version !== '0'
                  ? { expected_version: data.version }
                  : {}),
                new_orders_enabled: values.enabled === 'true',
                provider: data.provider,
                provider_account_ref: useCurrentAccount
                  ? data.current_provider_account_ref
                  : data.provider_account_ref,
                environment: data.environment,
                currency,
                reservation_minutes: parseShopInteger(
                  values.reservation,
                  1,
                  1440
                ),
                pending_order_limit: parseShopInteger(values.pending, 1, 100),
                min_amount_minor: minimum.amount_minor,
                max_amount_minor: maximum.amount_minor,
                retention_policy: Object.fromEntries(
                  retention.map(([key]) => [
                    key,
                    parseShopInteger(values[key], 1, 253402300798),
                  ])
                ),
                reminder_policy: Object.fromEntries(
                  reminders.map(([key]) => [
                    key,
                    parseShopInteger(values[key], 1, 253402300798),
                  ])
                ),
                reason:
                  values.reason.trim() ||
                  'Store settings updated from admin console',
              },
              { method: 'PATCH' }
            )
            if (updated) toast.success(t('Settings updated successfully'))
          } catch {
            setAdvancedOpen(true)
            setValidation(t('Check numeric limits, currency and date values.'))
          }
        })}
      >
        <section className='shop-panel'>
          <FieldGroup>
            <Field orientation='horizontal'>
              <FieldContent>
                <FieldLabel htmlFor='shop-sales'>
                  {t('Enable new purchases')}
                </FieldLabel>
                <FieldDescription>
                  {t(
                    'Pause new purchases without interrupting existing orders or after-sales support.'
                  )}
                </FieldDescription>
              </FieldContent>
              <Switch
                id='shop-sales'
                checked={enabled === 'true'}
                disabled={
                  action.pending ||
                  (paymentReady !== true && enabled !== 'true')
                }
                onCheckedChange={(checked) =>
                  form.setValue('enabled', String(checked))
                }
              />
            </Field>
          </FieldGroup>
        </section>
        <section className='shop-panel space-y-3'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <h2 className='font-semibold'>{t('Payment methods')}</h2>
            <span
              className={
                data.payment_ready
                  ? 'text-sm text-emerald-600 dark:text-emerald-400'
                  : 'text-muted-foreground text-sm'
              }
            >
              {paymentStatus}
            </span>
          </div>
          <p className='shop-muted text-sm'>
            {t(
              'Available payment methods use the existing payment configuration.'
            )}
          </p>
          <p className='text-sm'>
            {data.payment_methods
              .map((method) => {
                if (method === 'alipay') return t('Alipay')
                if (method === 'wxpay') return t('WeChat Pay')
                return method
              })
              .join(' · ') || t('No payment methods configured')}
            {' · '}
            {currency}
          </p>
          {data.payment_account_changed &&
            data.current_provider_account_ref && (
              <div className='flex flex-wrap items-center gap-3 border-t pt-3'>
                <p className='shop-muted min-w-0 flex-1 text-sm'>
                  {t(
                    'The payment account changed. Existing orders keep their original payment records.'
                  )}
                </p>
                <Button
                  type='button'
                  variant='outline'
                  disabled={action.pending || useCurrentAccount}
                  onClick={() => setUseCurrentAccount(true)}
                >
                  {useCurrentAccount
                    ? t('Current payment account selected')
                    : t('Use current payment account')}
                </Button>
              </div>
            )}
          {paymentReady !== true && (
            <p
              role='status'
              className='text-sm text-amber-700 dark:text-amber-400'
            >
              {t(
                'Complete the existing payment setup before enabling purchases.'
              )}
            </p>
          )}
        </section>
        <details
          className='shop-panel'
          open={advancedOpen}
          onToggle={(event) => setAdvancedOpen(event.currentTarget.open)}
        >
          <summary className='cursor-pointer font-semibold'>
            {t('Advanced settings')}
          </summary>
          <p className='shop-muted mt-3 mb-5 text-sm'>
            {t(
              'Defaults are ready to use. Change these limits only when your business needs them.'
            )}
          </p>
          <FieldGroup>
            <FieldSet>
              <FieldLegend>{t('Order limits')}</FieldLegend>
              <FieldGroup className='grid sm:grid-cols-2'>
                {[
                  ['reservation', t('Reservation minutes')],
                  ['pending', t('Unpaid order limit')],
                  [
                    'min',
                    t('Minimum order amount ({{currency}})', { currency }),
                  ],
                  [
                    'max',
                    t('Maximum order amount ({{currency}})', { currency }),
                  ],
                ].map(([name, label]) => (
                  <Field key={name}>
                    <FieldLabel htmlFor={`setting-${name}`}>{label}</FieldLabel>
                    <Input
                      id={`setting-${name}`}
                      inputMode={
                        name === 'min' || name === 'max' ? 'decimal' : 'numeric'
                      }
                      maxLength={24}
                      {...form.register(name)}
                    />
                  </Field>
                ))}
              </FieldGroup>
            </FieldSet>
            <FieldSet>
              <FieldLegend>{t('Data retention and reminders')}</FieldLegend>
              <FieldGroup className='grid sm:grid-cols-2'>
                {[...retention, ...reminders].map(([name, label]) => (
                  <Field key={name}>
                    <FieldLabel htmlFor={`setting-${name}`}>{label}</FieldLabel>
                    <Input
                      id={`setting-${name}`}
                      inputMode='numeric'
                      maxLength={12}
                      {...form.register(name)}
                    />
                  </Field>
                ))}
              </FieldGroup>
            </FieldSet>
            <Field>
              <FieldLabel htmlFor='setting-reason'>
                {t('Change note (optional)')}
              </FieldLabel>
              <Input
                id='setting-reason'
                maxLength={200}
                {...form.register('reason')}
              />
            </Field>
          </FieldGroup>
        </details>
        {validation && (
          <p role='alert' className='text-destructive'>
            {validation}
          </p>
        )}
        <ShopErrorNotice error={action.error} />
        <Button
          type='submit'
          disabled={
            action.pending ||
            !(useCurrentAccount
              ? data.current_provider_account_ref
              : data.provider_account_ref)
          }
        >
          {t('Save settings')}
        </Button>
      </form>
    </>
  )
}
