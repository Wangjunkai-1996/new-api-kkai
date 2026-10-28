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
import { Link, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { shopPath } from '@/features/shop/api'
import { productImage } from '@/features/shop/assets'
import {
  MoneyText,
  Pagination,
  ShopEmpty,
  ShopErrorNotice,
  ShopHeading,
  ShopLoading,
} from '@/features/shop/components/common'
import { useShopAction, useShopQuery } from '@/features/shop/hooks/use-shop'
import { parsePrice, parseShopInteger, priceInput } from '@/features/shop/lib'
import {
  categorySchema,
  pageSchema,
  productSchema,
  settingsSchema,
  type Product,
} from '@/features/shop/schemas'

import { AdminActionDialog } from '../components/action-dialog'

export function AdminProductsPage() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const products = useShopQuery(
    `/api/admin/shop/products?page=${page}`,
    pageSchema(productSchema)
  )
  const categories = useShopQuery(
    '/api/admin/shop/categories',
    z.array(categorySchema)
  )
  return (
    <>
      <ShopHeading
        title={t('Products')}
        description={t(
          'Publish clear terms and maintain the original rules on existing orders.'
        )}
      >
        <Button
          nativeButton={false}
          render={<Link to='/shop-admin/products/new' />}
        >
          {t('New product')}
        </Button>
      </ShopHeading>
      <ShopErrorNotice
        error={products.error ?? categories.error}
        retry={() => {
          void products.refetch()
          void categories.refetch()
        }}
      />
      {products.isPending && <ShopLoading />}
      {products.data?.items.length === 0 && <ShopEmpty />}
      <div className='shop-table-wrap'>
        <table className='shop-table'>
          <thead>
            <tr>
              <th>{t('Product')}</th>
              <th>{t('Price')}</th>
              <th>{t('Status')}</th>
              <th>{t('Actions')}</th>
            </tr>
          </thead>
          <tbody>
            {products.data?.items.map((product) => (
              <tr key={product.id}>
                <td>
                  <Link
                    to='/shop-admin/products/$productId'
                    params={{ productId: product.id }}
                  >
                    {product.name}
                  </Link>
                  <p className='shop-muted'>{product.specification}</p>
                </td>
                <td>
                  <MoneyText money={product.price} />
                </td>
                <td>
                  {(
                    {
                      published: t('Published'),
                      archived: t('Archived'),
                    } as Record<string, string>
                  )[product.state ?? ''] ?? t('Draft')}
                </td>
                <td>
                  <div className='shop-actions'>
                    <Link
                      to='/shop-admin/products/$productId'
                      params={{ productId: product.id }}
                    >
                      {t('Edit')}
                    </Link>
                    {['publish', 'unpublish', 'archive'].map(
                      (verb) =>
                        product.allowed_actions.includes(verb) && (
                          <AdminActionDialog
                            key={verb}
                            label={
                              (
                                {
                                  publish: t('Publish'),
                                  unpublish: t('Unpublish'),
                                  archive: t('Archive'),
                                } as Record<string, string>
                              )[verb]
                            }
                            description={product.name}
                            path={`/api/admin/shop/products/${shopPath(product.id)}/${verb}`}
                            schema={productSchema}
                            fields={
                              verb === 'publish'
                                ? []
                                : [{ name: 'reason', label: t('Reason') }]
                            }
                            body={(values) => ({
                              expected_version: product.version,
                              ...(verb !== 'publish'
                                ? { reason: values.reason }
                                : {}),
                            })}
                          />
                        )
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {products.data && (
        <Pagination
          page={page}
          total={products.data.total}
          pageSize={products.data.page_size}
          onPage={setPage}
        />
      )}
      <section className='shop-panel mt-6'>
        <h2>{t('Categories')}</h2>
        <div className='shop-actions'>
          {categories.data?.map((category) => (
            <AdminActionDialog
              key={category.id}
              label={category.name}
              description={t('Edit category')}
              path={`/api/admin/shop/categories/${shopPath(category.id)}`}
              method='PATCH'
              schema={categorySchema}
              fields={[
                { name: 'name', label: t('Name'), defaultValue: category.name },
                {
                  name: 'sort_order',
                  label: t('Sort order'),
                  defaultValue: String(category.sort_order),
                },
              ]}
              body={(values) => ({
                name: values.name,
                sort_order: parseShopInteger(values.sort_order),
                expected_version: category.version,
              })}
            />
          ))}
          <AdminActionDialog
            label={t('New category')}
            description={t('Add a single-level product category.')}
            path='/api/admin/shop/categories'
            schema={categorySchema}
            fields={[
              { name: 'name', label: t('Name'), maxLength: 120 },
              { name: 'sort_order', label: t('Sort order'), defaultValue: '0' },
            ]}
            body={(values) => ({
              name: values.name,
              sort_order: parseShopInteger(values.sort_order),
            })}
          />
        </div>
      </section>
    </>
  )
}

export function ProductEditorPage(props: { productId?: string }) {
  const product = useShopQuery(
    `/api/admin/shop/products/${shopPath(props.productId ?? '')}`,
    productSchema,
    { enabled: Boolean(props.productId) }
  )
  const settings = useShopQuery('/api/admin/shop/settings', settingsSchema)
  const categories = useShopQuery(
    '/api/admin/shop/categories',
    z.array(categorySchema)
  )
  if (settings.isPending || (props.productId && product.isPending)) {
    return <ShopLoading />
  }
  if (!settings.data || (props.productId && !product.data)) {
    return <ShopErrorNotice error={settings.error ?? product.error} />
  }
  return (
    <ProductEditor
      key={product.data?.version ?? 'new'}
      product={product.data}
      currency={settings.data.currency ?? ''}
      exponent={settings.data.currency_exponent ?? -1}
      categories={categories.data ?? []}
    />
  )
}

function ProductEditor(props: {
  product?: Product
  currency: string
  exponent: number
  categories: z.infer<typeof categorySchema>[]
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const action = useShopAction()
  const p = props.product
  const [validation, setValidation] = useState('')
  const form = useForm<Record<string, string>>({
    defaultValues: {
      name: p?.name ?? '',
      summary: p?.summary ?? '',
      specification: p?.specification ?? '',
      product_type: p?.product_type ?? 'account',
      delivery_type: p?.delivery_type ?? '',
      category_id: p?.category_id ?? '',
      price: p ? priceInput(p.price) : '',
      cover: p?.cover?.url ?? '',
      usage_conditions: p?.usage_conditions ?? '',
      delivery_terms: p?.delivery_policy?.public_terms ?? '',
      view_terms: p?.view_policy?.public_terms ?? '',
      warranty_terms: p?.warranty_policy?.public_terms ?? '',
      refund_terms: p?.refund_policy?.public_terms ?? '',
      entitlement_terms: p?.entitlement_policy?.public_terms ?? '',
      view_seconds: String(p?.view_policy?.duration_seconds ?? ''),
      warranty_mode: p?.warranty_policy?.mode ?? 'none',
      warranty_start: p?.warranty_policy?.start_event ?? 'first_delivered_at',
      warranty_seconds: String(p?.warranty_policy?.duration_seconds ?? ''),
      warranty_end: p?.warranty_policy?.fixed_end_at ?? '',
      entitlement_mode: p?.entitlement_policy?.mode ?? 'described',
      entitlement_start:
        p?.entitlement_policy?.start_event ?? 'first_delivered_at',
      entitlement_seconds: String(
        p?.entitlement_policy?.duration_seconds ?? ''
      ),
      entitlement_end: p?.entitlement_policy?.fixed_end_at ?? '',
      sla_clock: p?.delivery_policy?.sla?.clock ?? 'elapsed',
      sla_minutes: String(p?.delivery_policy?.sla?.duration_minutes ?? ''),
      timezone: p?.delivery_policy?.sla?.timezone ?? '',
      pause: String(p?.delivery_policy?.sla?.pause_on_missing_input ?? false),
      manual_capacity: String(p?.manual_capacity ?? ''),
      inventory_identity_domain: p?.inventory_identity_domain ?? '',
      identity_normalization_version: p?.identity_normalization_version ?? '1',
      template_version: p?.template_version ?? '1',
      low_stock_threshold: String(p?.low_stock_threshold ?? 0),
      account_identifier: String(
        Boolean(
          p?.buyer_input_schema.some((f) => f.key === 'account_identifier')
        )
      ),
      contact_email: String(
        Boolean(p?.buyer_input_schema.some((f) => f.key === 'contact_email'))
      ),
      order_note: String(
        Boolean(p?.buyer_input_schema.some((f) => f.key === 'order_note'))
      ),
      windows:
        p?.delivery_policy?.sla?.weekly_windows
          .map((w) => `${w.weekday} ${w.start} ${w.end}`)
          .join('\n') ?? '',
    },
  })
  const cover = useWatch({ control: form.control, name: 'cover' })
  const input = (name: string, label: string, area = false) => (
    <Field key={name}>
      <FieldLabel htmlFor={`product-${name}`}>{label}</FieldLabel>
      {area ? (
        <Textarea
          id={`product-${name}`}
          maxLength={8000}
          {...form.register(name)}
        />
      ) : (
        <Input
          id={`product-${name}`}
          maxLength={500}
          {...form.register(name)}
        />
      )}
    </Field>
  )
  const select = (name: string, label: string, options: [string, string][]) => (
    <Field key={name}>
      <FieldLabel htmlFor={`product-${name}`}>{label}</FieldLabel>
      <NativeSelect id={`product-${name}`} {...form.register(name)}>
        {options.map(([value, text]) => (
          <NativeSelectOption key={value} value={value}>
            {text}
          </NativeSelectOption>
        ))}
      </NativeSelect>
    </Field>
  )
  const period = (prefix: string, v: Record<string, string>) => ({
    mode: v[`${prefix}_mode`],
    start_event: v[`${prefix}_mode`] === 'duration' ? v[`${prefix}_start`] : '',
    duration_seconds:
      v[`${prefix}_mode`] === 'duration'
        ? parseShopInteger(v[`${prefix}_seconds`] || '0')
        : 0,
    fixed_end_at:
      v[`${prefix}_mode`] === 'fixed_end' ? v[`${prefix}_end`] || null : null,
    public_terms: v[`${prefix}_terms`],
  })
  return (
    <>
      <ShopHeading
        title={p ? t('Edit product') : t('New product')}
        description={t(
          'Drafts may be incomplete. Publishing checks every price, policy and delivery requirement.'
        )}
      />
      <form
        className='shop-stack'
        onSubmit={form.handleSubmit(async (v) => {
          setValidation('')
          try {
            const price = v.price
              ? parsePrice(v.price, props.currency, props.exponent)
              : undefined
            if (v.price && !price) {
              setValidation(
                t('Configure a supported currency and enter a valid price.')
              )
              return
            }
            const windows = v.windows.trim()
              ? v.windows
                  .trim()
                  .split('\n')
                  .map((line) => {
                    const [weekday, start, end] = line.trim().split(/\s+/)
                    if (
                      !/^([01]\d|2[0-3]):[0-5]\d$/.test(start ?? '') ||
                      !/^([01]\d|2[0-3]):[0-5]\d$/.test(end ?? '') ||
                      start >= end
                    ) {
                      throw new Error()
                    }
                    return {
                      weekday: parseShopInteger(weekday, 1, 7),
                      start,
                      end,
                    }
                  })
              : []
            if (windows.length > 14) throw new Error()
            if (
              v.delivery_type === 'manual' &&
              v.sla_clock === 'business' &&
              v.timezone
            ) {
              new Intl.DateTimeFormat('en', { timeZone: v.timezone }).format()
            }
            for (const prefix of ['warranty', 'entitlement']) {
              const end = v[`${prefix}_end`]
              if (
                v[`${prefix}_mode`] === 'fixed_end' &&
                end &&
                (!Number.isFinite(Date.parse(end)) || !end.endsWith('Z'))
              ) {
                throw new Error()
              }
            }
            const fields =
              v.product_type === 'card'
                ? [{ key: 'card_code', label: t('Card code'), required: true }]
                : [
                    {
                      key: 'account_identifier',
                      label: t('Account'),
                      required: true,
                    },
                    { key: 'password', label: t('Password'), required: true },
                  ]
            const body = {
              ...(p ? { expected_version: p.version } : {}),
              name: v.name,
              summary: v.summary,
              specification: v.specification,
              product_type: v.product_type,
              delivery_type: v.delivery_type,
              category_id: v.category_id,
              cover: { url: v.cover, width: 1024, height: 1024, alt: '' },
              ...(price ? { price } : {}),
              usage_conditions: v.usage_conditions,
              template_version: v.template_version,
              delivery_fields: fields,
              buyer_input_schema: [
                ['account_identifier', t('Account')],
                ['contact_email', t('Contact email')],
                ['order_note', t('Order note')],
              ]
                .filter(([key]) => v[key] === 'true')
                .map(([key, label]) => ({ key, label, required: true })),
              delivery_policy: {
                mode: v.delivery_type,
                public_terms: v.delivery_terms,
                sla:
                  v.delivery_type === 'manual'
                    ? {
                        clock: v.sla_clock,
                        duration_minutes: parseShopInteger(
                          v.sla_minutes || '0'
                        ),
                        timezone: v.timezone,
                        pause_on_missing_input: v.pause === 'true',
                        weekly_windows:
                          v.sla_clock === 'business' ? windows : [],
                      }
                    : null,
              },
              view_policy: {
                duration_seconds: parseShopInteger(v.view_seconds || '0'),
                first_start: 'delivered_at',
                replacement_start: 'delivered_at',
                expired_action: 'deny_reveal',
                public_terms: v.view_terms,
              },
              warranty_policy: period('warranty', v),
              entitlement_policy: period('entitlement', v),
              refund_policy: {
                goods_refund_mode: 'full_only',
                execution: 'manual_original_channel',
                public_terms: v.refund_terms,
              },
              manual_capacity: parseShopInteger(v.manual_capacity || '0'),
              inventory_identity_domain: v.inventory_identity_domain,
              identity_normalization_version: v.identity_normalization_version,
              low_stock_threshold: parseShopInteger(
                v.low_stock_threshold || '0'
              ),
            }
            const result = await action.run(
              p
                ? `/api/admin/shop/products/${shopPath(p.id)}`
                : '/api/admin/shop/products',
              productSchema,
              body,
              { method: p ? 'PATCH' : 'POST' }
            )
            if (result) {
              void navigate({
                to: '/shop-admin/products/$productId',
                params: { productId: result.id },
              })
            }
          } catch {
            setValidation(t('Check numeric limits, currency and date values.'))
          }
        })}
      >
        <section className='shop-panel'>
          <h2>{t('Product information')}</h2>
          <FieldGroup className='grid sm:grid-cols-2'>
            {input('name', t('Name'))}
            {input('summary', t('Summary'))}
            {input('specification', t('Specification'))}
            {input('price', `${t('Price')} · ${props.currency}`)}
            {select('category_id', t('Category'), [
              ['', t('Uncategorized')],
              ...props.categories.map((c): [string, string] => [c.id, c.name]),
            ])}
            {select('product_type', t('Product type'), [
              ['account', t('Account')],
              ['card', t('Card code')],
            ])}
            {select('delivery_type', t('Delivery type'), [
              ['', t('Not configured')],
              ['automatic', t('Automatic delivery')],
              ['manual', t('Manual delivery')],
            ])}
            {select('cover', t('Cover image'), [
              ['', t('No image')],
              ...[1, 2, 3, 4, 5, 6].map((n): [string, string] => [
                `/shop-assets/product-${n}.webp`,
                t('Illustration {{number}}', { number: n }),
              ]),
            ])}
          </FieldGroup>
          {productImage(cover) && (
            <img
              src={productImage(cover)}
              alt=''
              width={160}
              height={160}
              className='rounded-xl'
            />
          )}
        </section>
        <section className='shop-panel'>
          <h2>{t('Delivery requirements')}</h2>
          <FieldGroup className='grid sm:grid-cols-2'>
            {input('template_version', t('Template version'))}
            {input('inventory_identity_domain', t('Inventory identity domain'))}
            {input(
              'identity_normalization_version',
              t('Identity normalization version')
            )}
            {input('low_stock_threshold', t('Low stock threshold'))}
            {input('manual_capacity', t('Manual delivery capacity'))}
            {select('sla_clock', t('Delivery clock'), [
              ['elapsed', t('Elapsed time')],
              ['business', t('Business hours')],
            ])}
            {input('sla_minutes', t('Delivery duration in minutes'))}
            {input('timezone', t('IANA time zone'))}
            {input(
              'windows',
              t(
                'Weekly windows: weekday 1–7, start and end time, one per line'
              ),
              true
            )}
            {select('pause', t('Pause for requested missing information'), [
              ['false', t('No')],
              ['true', t('Yes')],
            ])}
            {[
              ['account_identifier', t('Require account identifier')],
              ['contact_email', t('Require contact email')],
              ['order_note', t('Require order note')],
            ].map(([key, label]) =>
              select(key, label, [
                ['false', t('No')],
                ['true', t('Yes')],
              ])
            )}
          </FieldGroup>
        </section>
        <section className='shop-panel'>
          <h2>{t('Product terms')}</h2>
          <FieldGroup>
            {input('usage_conditions', t('Usage conditions'), true)}
            {input('delivery_terms', t('Delivery terms'), true)}
            {input('view_seconds', t('Viewing period in seconds'))}
            {input('view_terms', t('Viewing period'), true)}
            {['warranty', 'entitlement'].map((prefix) => (
              <FieldGroup key={prefix} className='grid sm:grid-cols-2'>
                {select(
                  `${prefix}_mode`,
                  prefix === 'warranty' ? t('Warranty') : t('Product validity'),
                  [
                    ...(prefix === 'warranty'
                      ? [
                          ['none', t('No additional warranty')] as [
                            string,
                            string,
                          ],
                        ]
                      : [
                          ['described', t('Described in terms')] as [
                            string,
                            string,
                          ],
                        ]),
                    ['duration', t('Duration')],
                    ['fixed_end', t('Fixed end date')],
                  ]
                )}
                {select(`${prefix}_start`, t('Starts from'), [
                  ['paid_at', t('Payment confirmed')],
                  ['first_delivered_at', t('First delivery')],
                ])}
                {input(`${prefix}_seconds`, t('Duration in seconds'))}
                {input(`${prefix}_end`, t('End date in UTC (RFC3339)'))}
                {input(`${prefix}_terms`, t('Public terms'), true)}
              </FieldGroup>
            ))}
            {input('refund_terms', t('Refund policy'), true)}
          </FieldGroup>
        </section>
        {validation && (
          <p role='alert' className='text-destructive'>
            {validation}
          </p>
        )}
        <ShopErrorNotice error={action.error} />
        <div className='shop-actions'>
          <Button type='submit' disabled={action.pending}>
            {t('Save draft')}
          </Button>
          <Link to='/shop-admin/products'>{t('Back to products')}</Link>
        </div>
      </form>
    </>
  )
}
