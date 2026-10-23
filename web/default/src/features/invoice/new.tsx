import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useEffect, useRef, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { NativeSelect } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { useAuthStore } from '@/stores/auth-store'

import {
  createInvoice,
  getInvoiceOrders,
  getInvoiceProfile,
  getInvoiceProfiles,
  invoiceKey,
  quoteInvoice,
  saveInvoiceProfile,
} from './api'
import { InvoiceOrderSelector } from './components/order-selector'
import { InvoiceProfileFields } from './components/profile-form'
import { InvoiceError } from './components/shared'
import { freshInvoiceDraft, useInvoiceDraft } from './lib/draft'
import {
  formatInvoiceMoney,
  invoiceOrderKey,
  sumInvoiceOrders,
  invoiceMoneyTotals,
} from './lib/format'
import { applyInvoiceProfileErrors, invoiceProfileSchema } from './lib/profile'
import { useInvoiceOperation } from './lib/use-invoice-operation'
import type { InvoiceProfile } from './types'

export function NewInvoicePage(props: {
  sourceType?: string
  orderId?: string
  orderNo?: string
}) {
  const { t, i18n } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const { draft, setDraft, policy } = useInvoiceDraft()
  const navigate = useNavigate()
  const op = useInvoiceOperation()
  const setOperationError = op.setError
  const preselected = useRef('')
  const [profileId, setProfileId] = useState('')
  const [saved, setSaved] = useState(false)
  const form = useForm<InvoiceProfile>({
    resolver: zodResolver(invoiceProfileSchema),
    defaultValues: draft.profile,
  })
  const profiles = useQuery({
    queryKey: [...invoiceKey(user?.id), 'profiles'],
    queryFn: getInvoiceProfiles,
    retry: false,
    gcTime: 0,
  })
  const availableTypes =
    policy?.invoice_types.filter((option) =>
      draft.orders.every((order) =>
        order.allowed_invoice_types.includes(option.id)
      )
    ) || []
  const type = availableTypes.some((option) => option.id === draft.invoiceType)
    ? draft.invoiceType
    : availableTypes[0]?.id || ''
  const incompatible =
    new Set(draft.orders.map((order) => order.merge_group_id)).size > 1 ||
    new Set(
      draft.orders.map(
        (order) => `${order.currency}:${order.currency_exponent}`
      )
    ).size > 1
  const invalid =
    draft.orders.length === 0 ||
    draft.orders.length > (policy?.limits.max_orders || 50) ||
    incompatible ||
    !type ||
    draft.orders.some((order) => !order.eligible)
  const totals = invoiceMoneyTotals(draft.orders)
  const profileValues = useWatch({ control: form.control })
  useEffect(() => {
    return form.subscribe({
      formState: { values: true },
      callback: ({ values }) => {
        setDraft((previous) => ({
          ...previous,
          profile: values,
          consent: false,
        }))
        setSaved(false)
      },
    })
  }, [form, setDraft])
  useEffect(() => {
    const key = `${props.sourceType || ''}:${props.orderId || ''}:${props.orderNo || ''}`
    if (
      (!props.orderId && !props.orderNo) ||
      !policy?.enabled ||
      preselected.current === key
    ) {
      return
    }
    preselected.current = key
    let cancelled = false
    void getInvoiceOrders({
      source_type: props.sourceType,
      order_id: props.orderId,
      q: props.orderNo,
      eligibility: 'eligible',
      page_size: 20,
    })
      .then(async (page) => {
        if (cancelled) return
        const order = page.items.find(
          (item) =>
            item.order_id === props.orderId || item.order_no === props.orderNo
        )
        if (!order) {
          setOperationError(new Error('ORDER_UNAVAILABLE'))
          return
        }
        const quote = await quoteInvoice([
          { source_type: order.source_type, order_id: order.order_id },
        ])
        if (cancelled) return
        if (quote.eligible) {
          setDraft((previous) => ({
            ...previous,
            orders: [
              ...previous.orders.filter(
                (item) => invoiceOrderKey(item) !== invoiceOrderKey(order)
              ),
              order,
            ],
          }))
        }
      })
      .catch((error) => {
        if (!cancelled) setOperationError(error)
      })
    return () => {
      cancelled = true
    }
  }, [
    props.sourceType,
    props.orderId,
    props.orderNo,
    policy?.enabled,
    setDraft,
    setOperationError,
  ])
  async function next() {
    if (draft.step === 2 && !(await form.trigger())) return
    if (draft.step === 2) {
      const required =
        policy?.invoice_types.find((option) => option.id === type)
          ?.required_fields || []
      let missing = false
      for (const field of required) {
        const key = field as keyof InvoiceProfile
        if (!String(form.getValues(key) || '').trim()) {
          form.setError(key, { type: 'required' })
          missing = true
        }
      }
      if (missing) return
    }
    const quote = await op.run('quote', { orders: draft.orders, type }, () =>
      quoteInvoice(draft.orders, type, policy?.policy_version)
    )
    if (!quote) return
    if (!quote.eligible) {
      setDraft((previous) => ({
        ...previous,
        orders: quote.items,
        step: 1,
        consent: false,
      }))
      op.setError(new Error('ORDER_UNAVAILABLE'))
      return
    }
    setDraft((previous) => ({
      ...previous,
      orders: quote.items,
      invoiceType: type,
      step: Math.min(3, previous.step + 1),
      profile: form.getValues(),
    }))
  }
  async function submit() {
    if (!policy || !draft.consent || invalid) return
    const quote = await op.run('quote', { orders: draft.orders, type }, () =>
      quoteInvoice(draft.orders, type, policy.policy_version)
    )
    if (!quote) return
    if (!quote.eligible || quote.policy_version !== policy.policy_version) {
      setDraft((previous) => ({
        ...previous,
        orders: quote.items,
        step: 1,
        consent: false,
      }))
      op.setError(new Error('ORDER_UNAVAILABLE'))
      return
    }
    if (
      quote.amount_minor !== sumInvoiceOrders(draft.orders) ||
      quote.currency !== draft.orders[0]?.currency
    ) {
      setDraft((previous) => ({
        ...previous,
        orders: quote.items,
        consent: false,
      }))
      op.setError(new Error('PAYMENT_CHANGED'))
      return
    }
    const input = {
      items: draft.orders.map((order) => ({
        source_type: order.source_type,
        order_id: order.order_id,
      })),
      profile: {
        ...form.getValues(),
        tax_id:
          profileValues.kind === 'person' ? '' : profileValues.tax_id || '',
      },
      invoice_type: type,
      policy_version: policy.policy_version,
      note: draft.note,
      user_confirmed: true,
    }
    const result = await op.run(
      'create',
      input,
      () => createInvoice(input, draft.key),
      (error) => {
        if (applyInvoiceProfileErrors(form, error)) {
          setDraft((previous) => ({ ...previous, step: 2, consent: false }))
        }
      }
    )
    if (result) {
      setDraft(freshInvoiceDraft(user?.email))
      void navigate({
        to: '/invoices/$invoiceId',
        params: { invoiceId: result.public_id },
      })
    }
  }
  let actionLabel = t('Next: invoice information')
  if (draft.step === 2) actionLabel = t('Next: review request')
  if (draft.step === 3) actionLabel = t('Submit invoice request')
  if (op.busy) actionLabel = t('Processing...')
  const action = (
    <Button
      className='invoice-primary-action min-h-11 w-full'
      disabled={
        !policy?.enabled ||
        invalid ||
        op.busy ||
        (draft.step === 3 && !draft.consent)
      }
      onClick={() => void (draft.step === 3 ? submit() : next())}
    >
      {actionLabel}
    </Button>
  )
  if (policy && !policy.enabled) {
    return (
      <InvoiceError
        message={t(
          'Invoicing is not available yet. Existing requests remain accessible.'
        )}
      />
    )
  }
  return (
    <div className='space-y-4'>
      <div
        className='invoice-stepper'
        aria-label={t('Invoice application steps')}
      >
        {['Select orders', 'Invoice information', 'Review and submit'].map(
          (label, index) => (
            <button
              type='button'
              key={label}
              disabled={index + 1 > draft.step}
              onClick={() => setDraft({ ...draft, step: index + 1 })}
              aria-current={draft.step === index + 1 ? 'step' : undefined}
            >
              <span className='invoice-step-circle'>{index + 1}</span>
              {t(label)}
            </button>
          )
        )}
      </div>
      <InvoiceError error={op.error} />
      <div className='invoice-workspace'>
        <div className='min-w-0'>
          {draft.step === 1 && (
            <InvoiceOrderSelector
              selected={draft.orders}
              onChange={(orders) =>
                setDraft({ ...draft, orders, consent: false })
              }
            />
          )}
          {draft.step === 2 && (
            <section className='invoice-panel'>
              <div className='invoice-panel-head'>
                <h2 className='font-medium'>{t('Invoice information')}</h2>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t(
                    'Use a saved title or enter information for this request.'
                  )}
                </p>
              </div>
              <div className='invoice-panel-body space-y-5'>
                <label className='grid gap-2 text-sm'>
                  {t('Saved invoice titles')}
                  <NativeSelect
                    value={profileId}
                    disabled={op.busy}
                    onChange={async (event) => {
                      const id = event.target.value
                      setProfileId(id)
                      if (id) {
                        const profile = await op.run('load-profile', id, () =>
                          getInvoiceProfile(id)
                        )
                        if (profile) form.reset(profile)
                      }
                    }}
                  >
                    <option value=''>{t('Enter manually')}</option>
                    {profiles.data?.items.map((profile) => (
                      <option key={profile.id} value={profile.id}>
                        {profile.title}
                      </option>
                    ))}
                  </NativeSelect>
                </label>
                <label className='grid gap-2 text-sm'>
                  {t('Invoice type')}
                  <NativeSelect
                    value={type}
                    onChange={(event) =>
                      setDraft({ ...draft, invoiceType: event.target.value })
                    }
                  >
                    {availableTypes.map((option) => (
                      <option key={option.id} value={option.id}>
                        {option.id === 'manual' ? t('Invoice') : option.label}
                      </option>
                    ))}
                  </NativeSelect>
                </label>
                <fieldset disabled={op.busy}>
                  <InvoiceProfileFields
                    form={form}
                    policy={policy}
                    invoiceType={type}
                    error={op.error}
                  />
                </fieldset>
                <label className='grid gap-2 text-sm'>
                  {t('Additional note')}
                  <Textarea
                    maxLength={500}
                    value={draft.note}
                    onChange={(event) =>
                      setDraft({ ...draft, note: event.target.value })
                    }
                  />
                </label>
                <div className='flex flex-wrap gap-2'>
                  <Button
                    type='button'
                    variant='outline'
                    disabled={op.busy}
                    onClick={async () => {
                      if (!(await form.trigger())) return
                      const profile = {
                        ...form.getValues(),
                        id: undefined,
                        version: undefined,
                      }
                      const result = await op.run(
                        'save-profile',
                        profile,
                        (key) => saveInvoiceProfile(profile, key),
                        (error) => {
                          applyInvoiceProfileErrors(form, error)
                        }
                      )
                      if (result) {
                        setProfileId(result.id || '')
                        form.reset(result)
                        setSaved(true)
                      }
                    }}
                  >
                    {t('Save as new title')}
                  </Button>
                  {profileId && (
                    <Button
                      type='button'
                      variant='outline'
                      disabled={op.busy}
                      onClick={async () => {
                        if (!(await form.trigger())) return
                        const result = await op.run(
                          'update-profile',
                          form.getValues(),
                          (key) =>
                            saveInvoiceProfile(
                              { ...form.getValues(), id: profileId },
                              key
                            ),
                          (error) => {
                            applyInvoiceProfileErrors(form, error)
                          }
                        )
                        if (result) {
                          form.reset(result)
                          setSaved(true)
                        }
                      }}
                    >
                      {t('Update saved title')}
                    </Button>
                  )}
                  {saved && (
                    <span role='status' className='text-sm text-emerald-700'>
                      {t('Saved')}
                    </span>
                  )}
                </div>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Editing a saved title does not change submitted requests.'
                  )}
                </p>
              </div>
            </section>
          )}
          {draft.step === 3 && (
            <section className='invoice-panel'>
              <div className='invoice-panel-head flex items-center justify-between gap-2'>
                <h2 className='font-medium'>{t('Review and submit')}</h2>
                <Button
                  variant='ghost'
                  size='sm'
                  onClick={() => setDraft({ ...draft, step: 2 })}
                >
                  {t('Edit information')}
                </Button>
              </div>
              <div className='invoice-panel-body'>
                {policy?.seller_name && (
                  <div className='invoice-data-row'>
                    <span>{t('Seller')}</span>
                    <span>{policy.seller_name}</span>
                  </div>
                )}
                <div className='invoice-data-row'>
                  <span>{t('Invoice title')}</span>
                  <span>{profileValues.title}</span>
                </div>
                {profileValues.kind === 'company' && (
                  <div className='invoice-data-row'>
                    <span>{t('Tax ID')}</span>
                    <span>
                      {(profileValues.tax_id || '').slice(0, 3)}••••
                      {(profileValues.tax_id || '').slice(-4)}
                    </span>
                  </div>
                )}
                <div className='invoice-data-row'>
                  <span>{t('Invoice email')}</span>
                  <span>{profileValues.email}</span>
                </div>
                <div className='invoice-data-row'>
                  <span>{t('Invoice type')}</span>
                  <span>
                    {type === 'manual'
                      ? t('Invoice')
                      : policy?.invoice_types.find(
                          (option) => option.id === type
                        )?.label}
                  </span>
                </div>
                <div className='mt-5 flex items-center justify-between'>
                  <h3 className='text-sm font-medium'>
                    {t('Selected orders')}
                  </h3>
                  <Button
                    variant='ghost'
                    size='sm'
                    onClick={() => setDraft({ ...draft, step: 1 })}
                  >
                    {t('Edit orders')}
                  </Button>
                </div>
                {draft.orders.map((order) => (
                  <div
                    className='invoice-data-row'
                    key={invoiceOrderKey(order)}
                  >
                    <span className='min-w-0! shrink! wrap-anywhere'>
                      {order.order_no}
                    </span>
                    <span className='shrink-0 tabular-nums'>
                      {formatInvoiceMoney(
                        order.amount_minor,
                        order.currency,
                        order.currency_exponent,
                        i18n.language
                      )}
                    </span>
                  </div>
                ))}
                <label className='mt-6 flex items-start gap-3 text-sm leading-relaxed'>
                  <Checkbox
                    className='mt-1'
                    checked={draft.consent}
                    onCheckedChange={(consent) =>
                      setDraft({ ...draft, consent })
                    }
                  />
                  {t(
                    'I have checked the orders and invoice information and confirm they are accurate.'
                  )}
                </label>
              </div>
            </section>
          )}
        </div>
        <aside className='invoice-panel invoice-summary'>
          <div className='invoice-panel-body'>
            <h2 className='text-sm font-medium'>{t('Request summary')}</h2>
            <p className='text-muted-foreground mt-5 text-xs'>
              {t('Invoice amount')}
            </p>
            <p className='mt-1 text-[28px] font-medium tracking-tight tabular-nums'>
              {totals.length
                ? totals.map((total) => (
                    <span className='block' key={total.currency}>
                      {formatInvoiceMoney(
                        total.amount_minor,
                        total.currency,
                        total.currency_exponent,
                        i18n.language
                      )}
                    </span>
                  ))
                : '—'}
            </p>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('{{count}} selected', { count: draft.orders.length })}
            </p>
            <div className='text-muted-foreground mt-5 border-t pt-4 text-xs leading-relaxed'>
              <p>{t('Each selected payment is invoiced in full.')}</p>
              <p className='mt-2'>
                {t(
                  'Approval starts invoice preparation. Delivery follows after issuance.'
                )}
              </p>
            </div>
            {incompatible && (
              <p role='alert' className='text-destructive mt-3 text-xs'>
                {t(
                  'These orders cannot be combined. Remove incompatible orders and apply separately.'
                )}
              </p>
            )}
            <div className='mt-5'>{action}</div>
            {draft.step > 1 && (
              <Button
                variant='ghost'
                className='mt-2 w-full'
                onClick={() => setDraft({ ...draft, step: draft.step - 1 })}
              >
                {t('Previous step')}
              </Button>
            )}
            <details className='mt-4 text-xs'>
              <summary className='text-muted-foreground cursor-pointer'>
                {t('View selected orders')}
              </summary>
              <div className='mt-2 space-y-2'>
                {draft.orders.map((order) => (
                  <div
                    key={invoiceOrderKey(order)}
                    className='flex items-start justify-between gap-2'
                  >
                    <span className='wrap-anywhere'>{order.order_no}</span>
                    <button
                      type='button'
                      className='text-primary shrink-0 underline'
                      onClick={() =>
                        setDraft({
                          ...draft,
                          orders: draft.orders.filter(
                            (item) =>
                              invoiceOrderKey(item) !== invoiceOrderKey(order)
                          ),
                        })
                      }
                    >
                      {t('Remove')}
                    </button>
                  </div>
                ))}
              </div>
            </details>
          </div>
        </aside>
      </div>
      <div className='invoice-bottom'>
        <div className='invoice-bottom-summary text-xs'>
          <p className='text-muted-foreground'>
            {t('{{count}} selected', { count: draft.orders.length })}
          </p>
          <p className='mt-1 font-medium tabular-nums'>
            {totals.length
              ? totals.map((total) => (
                  <span className='block' key={total.currency}>
                    {formatInvoiceMoney(
                      total.amount_minor,
                      total.currency,
                      total.currency_exponent,
                      i18n.language
                    )}
                  </span>
                ))
              : '—'}
          </p>
        </div>
        <div className='invoice-bottom-actions'>
          {draft.step > 1 && (
            <Button
              variant='ghost'
              size='sm'
              onClick={() => setDraft({ ...draft, step: draft.step - 1 })}
            >
              {t('Previous step')}
            </Button>
          )}
          {action}
        </div>
      </div>
    </div>
  )
}
