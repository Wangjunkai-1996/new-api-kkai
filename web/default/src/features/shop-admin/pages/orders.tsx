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
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { shopPath, shopRequest } from '@/features/shop/api'
import {
  MoneyText,
  OrderStatus,
  Pagination,
  ShopErrorNotice,
  ShopEventSummary,
  ShopHeading,
  ShopLoading,
  ShopTime,
} from '@/features/shop/components/common'
import { usePrivateResult, useShopQuery } from '@/features/shop/hooks/use-shop'
import {
  buyerInputSchema,
  orderSchema,
  pageSchema,
  type Order,
} from '@/features/shop/schemas'

import { evidenceIds } from '../action-values'
import { AdminActionDialog } from '../components/action-dialog'
import { AdminPrivateReveal } from '../components/private-reveal'

export function AdminOrdersPage() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState('')
  const [query, setQuery] = useState('')
  const orders = useShopQuery(
    `/api/admin/shop/orders?page=${page}&q=${encodeURIComponent(query)}`,
    pageSchema(orderSchema)
  )
  return (
    <>
      <ShopHeading
        title={t('Orders')}
        description={t(
          'Handle delivery and exceptions from the current order state.'
        )}
      />
      <form
        className='shop-actions'
        onSubmit={(event) => {
          event.preventDefault()
          setQuery(search)
          setPage(1)
        }}
      >
        <Input
          aria-label={t('Order number')}
          maxLength={100}
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
        <Button>{t('Search')}</Button>
      </form>
      <ShopErrorNotice error={orders.error} />
      {orders.isPending && <ShopLoading />}
      <div className='shop-table-wrap'>
        <table className='shop-table'>
          <thead>
            <tr>
              <th>{t('Order number')}</th>
              <th>{t('Product')}</th>
              <th>{t('Status')}</th>
              <th>{t('Total')}</th>
              <th>{t('Created at')}</th>
            </tr>
          </thead>
          <tbody>
            {orders.data?.items.map((order) => (
              <tr key={order.order_no}>
                <td>
                  <Link
                    to='/shop-admin/orders/$orderNo'
                    params={{ orderNo: order.order_no }}
                  >
                    {order.order_no}
                  </Link>
                </td>
                <td>{order.product_snapshot.name}</td>
                <td>
                  <OrderStatus order={order} />
                </td>
                <td>
                  <MoneyText money={order.total} />
                </td>
                <td>
                  <ShopTime value={order.created_at} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {orders.data && (
        <Pagination
          page={page}
          total={orders.data.total}
          pageSize={orders.data.page_size}
          onPage={setPage}
        />
      )}
    </>
  )
}

export function AdminOrderPage(props: { orderNo: string }) {
  const { t } = useTranslation()
  const base = `/api/admin/shop/orders/${shopPath(props.orderNo)}`
  const query = useShopQuery(base, orderSchema)
  const buyerInput = usePrivateResult<{
    fields: { key: string; label: string }[]
    values: Record<string, string>
  }>(props.orderNo)
  if (query.isPending) return <ShopLoading />
  if (!query.data) return <ShopErrorNotice error={query.error} />
  const order = query.data
  const fields = order.product_snapshot.delivery_fields ?? []
  const templateVersion = order.product_snapshot.template_version
  const can = (action: string) => order.allowed_actions.includes(action)
  return (
    <>
      <ShopHeading
        eyebrow={t('Order details')}
        title={order.product_snapshot.name}
        description={order.order_no}
      >
        <OrderStatus order={order} />
      </ShopHeading>
      <div className='shop-columns'>
        <div className='shop-stack'>
          <section className='shop-panel'>
            <h2>{t('Delivery operations')}</h2>
            <p>
              {t('Delivery due at')} <ShopTime value={order.delivery_due_at} />
            </p>
            <div className='shop-actions'>
              {can('claim') && (
                <AdminActionDialog
                  label={t('Claim order')}
                  description={order.order_no}
                  path={`${base}/claim`}
                  schema={orderSchema}
                  body={() => ({ expected_version: order.version })}
                />
              )}
              {can('request_input') && (
                <AdminActionDialog
                  label={t('Request missing information')}
                  description={t(
                    'Only request fields defined by the product snapshot.'
                  )}
                  path={`${base}/request-input`}
                  schema={orderSchema}
                  fields={[
                    {
                      name: 'missing_field_keys',
                      label: t('Missing field keys'),
                    },
                    {
                      name: 'public_reason',
                      label: t('Reason visible to buyer'),
                      type: 'textarea',
                    },
                  ]}
                  body={(values) => {
                    const keys = values.missing_field_keys
                      .split(/[\s,]+/)
                      .filter(Boolean)
                    if (
                      !keys.length ||
                      keys.some(
                        (key) =>
                          !order.product_snapshot.buyer_input_schema?.some(
                            (field) => field.key === key
                          )
                      )
                    ) {
                      throw new Error()
                    }
                    return {
                      expected_version: order.version,
                      missing_field_keys: [...new Set(keys)],
                      public_reason: values.public_reason,
                    }
                  }}
                />
              )}
              {can('retry_delivery') && (
                <AdminActionDialog
                  label={t('Retry automatic delivery')}
                  description={t(
                    'The server retries allocation using the existing order.'
                  )}
                  path={`${base}/retry-delivery`}
                  schema={orderSchema}
                  fields={[{ name: 'reason', label: t('Reason') }]}
                  body={(values) => ({
                    expected_version: order.version,
                    reason: values.reason,
                  })}
                />
              )}
              {can('deliver') && templateVersion && (
                <AdminActionDialog
                  label={t('Deliver manually')}
                  description={t(
                    'Delivery values are encrypted by the server and never stored in browser caches.'
                  )}
                  path={`${base}/deliveries`}
                  schema={orderSchema}
                  fields={[
                    ...fields.map((field) => ({
                      name: field.key,
                      label: field.label,
                      type: 'password' as const,
                      optional: !field.required,
                      maxLength: field.max_length ?? 8000,
                    })),
                    {
                      name: 'instructions',
                      label: t('Delivery instructions'),
                      type: 'textarea',
                      optional: true,
                    },
                    { name: 'reason', label: t('Reason') },
                  ]}
                  body={(values) => ({
                    expected_version: order.version,
                    secret_fields: fields
                      .filter((field) => values[field.key])
                      .map((field) => ({
                        key: field.key,
                        value: values[field.key] ?? '',
                      })),
                    instructions: values.instructions,
                    reason: values.reason,
                  })}
                  proof={(values, key) => ({
                    scope: 'shop.delivery.create',
                    context: {
                      action: 'first',
                      target_id: order.order_no,
                      expected_version: order.version,
                      idempotency_key: key,
                      details: {
                        reason: values.reason,
                        template_version: templateVersion,
                      },
                    },
                  })}
                />
              )}
            </div>
            {can('view_buyer_input') && (
              <div className='shop-stack'>
                <Button
                  variant='outline'
                  disabled={buyerInput.pending}
                  onClick={() =>
                    buyerInput.read((signal) =>
                      shopRequest(`${base}/buyer-input`, buyerInputSchema, {
                        signal,
                      })
                    )
                  }
                >
                  {t('View buyer information')}
                </Button>
                {buyerInput.value && (
                  <div>
                    {buyerInput.value.fields.map((field) => (
                      <p key={field.key}>
                        <strong>{field.label}</strong>:{' '}
                        {buyerInput.value?.values[field.key]}
                      </p>
                    ))}
                    <Button variant='outline' onClick={buyerInput.clear}>
                      {t('Hide details')}
                    </Button>
                  </div>
                )}
                <ShopErrorNotice error={buyerInput.error} />
              </div>
            )}
          </section>
          <RiskActions order={order} base={base} />
          <section className='shop-panel'>
            <h2>{t('Delivery history')}</h2>
            {[
              ...(order.current_delivery ? [order.current_delivery] : []),
              ...order.delivery_history,
            ]
              .filter(
                (delivery, index, list) =>
                  list.findIndex((item) => item.id === delivery.id) === index
              )
              .map((delivery) => (
                <div key={delivery.id} className='shop-stack'>
                  <p>
                    {delivery.id} · <ShopTime value={delivery.delivered_at} />
                  </p>
                  {can('reveal_delivery') && (
                    <AdminPrivateReveal
                      path={`${base}/deliveries/${shopPath(
                        delivery.id
                      )}/reveal`}
                      target={order.order_no}
                      version={order.version}
                      scope='shop.delivery.reveal'
                      deliveryId={delivery.id}
                    />
                  )}
                </div>
              ))}
          </section>
          <section className='shop-panel'>
            <h2>{t('Order progress')}</h2>
            <ol className='shop-timeline'>
              {order.timeline.map((event) => (
                <li key={event.event_id}>
                  <p>
                    <ShopEventSummary event={event} />
                  </p>
                  <small>
                    <ShopTime value={event.occurred_at} />
                  </small>
                </li>
              ))}
            </ol>
          </section>
        </div>
        <aside className='shop-panel shop-receipt'>
          <h2>{t('Order summary')}</h2>
          <MoneyText money={order.total} />
          <p>
            {t('Created at')} <ShopTime value={order.created_at} />
          </p>
          <p>
            {t('Paid at')} <ShopTime value={order.paid_at} />
          </p>
          {can('note') && (
            <AdminActionDialog
              label={t('Add note')}
              description={t(
                'Do not include passwords, card codes or verification codes in messages.'
              )}
              path={`${base}/notes`}
              schema={orderSchema}
              fields={[
                {
                  name: 'visibility',
                  label: t('Visibility'),
                  options: [
                    { value: 'internal', label: t('Internal only') },
                    { value: 'public', label: t('Visible to buyer') },
                  ],
                },
                { name: 'body', label: t('Note'), type: 'textarea' },
              ]}
              body={(values) => ({
                expected_version: order.version,
                visibility: values.visibility,
                body: values.body,
              })}
            />
          )}
          <Button variant='outline' onClick={() => void query.refetch()}>
            {t('Refresh')}
          </Button>
          <Link to='/shop-admin/orders'>{t('Back to orders')}</Link>
        </aside>
      </div>
    </>
  )
}

function RiskActions(props: { order: Order; base: string }) {
  const { t } = useTranslation()
  const order = props.order
  return (
    <section className='shop-panel'>
      <h2>{t('Risk review')}</h2>
      {order.risk_hold && <p>{order.risk_hold.public_reason}</p>}
      <div className='shop-actions'>
        {order.allowed_actions.includes('risk_hold') && (
          <AdminActionDialog
            label={t('Place risk hold')}
            description={t(
              'A risk hold blocks delivery and viewing while the issue is reviewed.'
            )}
            path={`${props.base}/risk-hold`}
            schema={orderSchema}
            fields={[
              {
                name: 'internal_reason',
                label: t('Internal reason'),
                type: 'textarea',
              },
              {
                name: 'public_reason',
                label: t('Reason visible to buyer'),
                type: 'textarea',
              },
              {
                name: 'evidence_refs',
                label: t('Evidence IDs'),
                optional: true,
              },
            ]}
            body={(values) => ({
              expected_version: order.version,
              internal_reason: values.internal_reason,
              public_reason: values.public_reason,
              evidence_refs: evidenceIds(values.evidence_refs),
            })}
          />
        )}
        {order.allowed_actions.includes('risk_release') && (
          <AdminActionDialog
            label={t('Release risk hold')}
            description={t(
              'Review the evidence before allowing delivery or viewing to resume.'
            )}
            path={`${props.base}/risk-release`}
            schema={orderSchema}
            fields={[
              { name: 'reason', label: t('Reason') },
              { name: 'evidence_refs', label: t('Evidence IDs') },
            ]}
            body={(values) => ({
              expected_version: order.version,
              reason: values.reason,
              evidence_refs: evidenceIds(values.evidence_refs),
            })}
            proof={(values, key) => ({
              scope: 'shop.risk.release',
              context: {
                action: 'release',
                target_id: order.order_no,
                expected_version: order.version,
                idempotency_key: key,
                details: {
                  reason: values.reason,
                  evidence_ids: evidenceIds(values.evidence_refs),
                },
              },
            })}
          />
        )}
      </div>
    </section>
  )
}
