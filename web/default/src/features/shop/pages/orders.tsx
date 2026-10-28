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
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'

import { queryString, shopPath } from '../api'
import { productImage } from '../assets'
import { BuyerInput } from '../components/buyer-input'
import {
  MoneyText,
  OrderStatus,
  Pagination,
  ShopEmpty,
  ShopErrorNotice,
  ShopEventSummary,
  ShopHeading,
  ShopLoading,
  ShopState,
  ShopTime,
} from '../components/common'
import { DeliveryDetails } from '../components/delivery'
import { useShopQuery } from '../hooks/use-shop'
import { eventSchema, orderSchema, pageSchema } from '../schemas'

export function OrdersPage(props: {
  page: number
  q?: string
  status_group?: 'all' | 'unpaid' | 'delivery' | 'delivered' | 'aftersale'
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [search, setSearch] = useState(props.q ?? '')
  const query = useShopQuery(
    `/api/shop/orders${queryString(props)}`,
    pageSchema(orderSchema)
  )
  const change = (value: Partial<typeof props>) =>
    void navigate({
      to: '/shop/orders',
      search: { ...props, page: 1, ...value },
    })
  return (
    <>
      <ShopHeading
        eyebrow={t('Your collection')}
        title={t('My orders')}
        description={t(
          'Payments, delivery details and support, kept together.'
        )}
      />
      <form
        className='shop-filters'
        onSubmit={(event) => {
          event.preventDefault()
          change({ q: search })
        }}
      >
        <Field className='w-auto flex-1'>
          <FieldLabel htmlFor='shop-order-search'>
            {t('Order number')}
          </FieldLabel>
          <Input
            id='shop-order-search'
            maxLength={100}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </Field>
        <Field className='w-auto'>
          <FieldLabel htmlFor='shop-order-filter'>{t('Status')}</FieldLabel>
          <NativeSelect
            id='shop-order-filter'
            value={props.status_group ?? 'all'}
            onChange={(event) =>
              change({
                status_group: event.target.value as typeof props.status_group,
              })
            }
          >
            {[
              ['all', t('All')],
              ['unpaid', t('Awaiting payment')],
              ['delivery', t('Awaiting delivery')],
              ['delivered', t('Delivered')],
              ['aftersale', t('After-sales support')],
            ].map(([value, label]) => (
              <NativeSelectOption key={value} value={value}>
                {label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        <Button type='submit'>{t('Search')}</Button>
      </form>
      <ShopErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending && <ShopLoading />}
      {query.data?.items.length === 0 && <ShopEmpty />}
      <div className='shop-list mt-5'>
        {query.data?.items.map((order) => (
          <Link
            key={order.order_no}
            className='shop-order-card'
            to='/shop/orders/$orderNo'
            params={{ orderNo: order.order_no }}
          >
            {productImage(order.product_snapshot.cover?.url) && (
              <img
                src={productImage(order.product_snapshot.cover?.url)}
                alt=''
                width={72}
                height={72}
                loading='lazy'
              />
            )}
            <div className='grow'>
              <small>{order.order_no}</small>
              <h2>{order.product_snapshot.name}</h2>
              <small>
                <ShopTime value={order.created_at} />
              </small>
            </div>
            <OrderStatus order={order} />
            <MoneyText money={order.total} />
          </Link>
        ))}
      </div>
      {query.data && (
        <Pagination
          page={props.page}
          total={query.data.total}
          pageSize={query.data.page_size}
          onPage={(page) => change({ page })}
        />
      )}
    </>
  )
}

export function OrderPage(props: { orderNo: string }) {
  const { t } = useTranslation()
  const query = useShopQuery(
    `/api/shop/orders/${shopPath(props.orderNo)}`,
    orderSchema,
    { poll: 30_000 }
  )
  const [eventsPage, setEventsPage] = useState(0)
  const events = useShopQuery(
    `/api/shop/orders/${shopPath(props.orderNo)}/events?page=${Math.max(1, eventsPage)}`,
    pageSchema(eventSchema),
    { enabled: eventsPage > 0 }
  )
  if (query.isPending) return <ShopLoading />
  if (!query.data) {
    return (
      <ShopErrorNotice error={query.error} retry={() => void query.refetch()} />
    )
  }
  const order = query.data
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
          {order.risk_hold && (
            <Alert>
              <AlertDescription>
                {order.risk_hold.public_reason}
              </AlertDescription>
            </Alert>
          )}
          {order.extra_payment_summary.length > 0 && (
            <section className='shop-panel'>
              <h2>{t('Additional payments')}</h2>
              <p className='shop-muted'>
                {t(
                  'Additional payments are handled separately from your product delivery.'
                )}
              </p>
              {order.extra_payment_summary.map((item, index) => (
                <div key={item.id ?? index}>
                  <MoneyText money={item.money} />
                  <p>
                    <ShopState state={item.state} />
                  </p>
                </div>
              ))}
            </section>
          )}
          <DeliveryDetails order={order} />
          {order.allowed_actions.includes('provide_input') ||
          order.product_snapshot.buyer_input_schema?.length ? (
            <BuyerInput order={order} />
          ) : null}
          <section className='shop-panel'>
            <h2>{t('Order progress')}</h2>
            <ol className='shop-timeline'>
              {(events.data?.items ?? order.timeline).map((event) => (
                <li key={event.event_id}>
                  <p>
                    <ShopEventSummary event={event} />
                  </p>
                  <small className='shop-muted'>
                    <ShopTime value={event.occurred_at} />
                  </small>
                </li>
              ))}
            </ol>
            <ShopErrorNotice error={events.error} />
            {eventsPage === 0 ? (
              <Button variant='outline' onClick={() => setEventsPage(1)}>
                {t('View all events')}
              </Button>
            ) : (
              events.data && (
                <Pagination
                  page={eventsPage}
                  total={events.data.total}
                  pageSize={events.data.page_size}
                  onPage={setEventsPage}
                />
              )
            )}
          </section>
        </div>
        <aside className='shop-panel shop-receipt'>
          <h2>{t('Order summary')}</h2>
          <dl className='shop-detail-list'>
            <div>
              <dt>{t('Total')}</dt>
              <dd>
                <MoneyText money={order.total} />
              </dd>
            </div>
            <div>
              <dt>{t('Created at')}</dt>
              <dd>
                <ShopTime value={order.created_at} />
              </dd>
            </div>
            <div>
              <dt>{t('Paid at')}</dt>
              <dd>
                <ShopTime value={order.paid_at} />
              </dd>
            </div>
            <div>
              <dt>{t('Delivery due at')}</dt>
              <dd>
                <ShopTime value={order.delivery_due_at} />
              </dd>
            </div>
          </dl>
          {!order.delivery_due_at &&
            order.delivery_state === 'awaiting_input' && (
              <p className='shop-muted'>
                {t(
                  'The delivery deadline is calculated after payment and the required information are complete.'
                )}
              </p>
            )}
          {order.refund_summary && (
            <Alert>
              <AlertDescription>
                <ShopState state={order.refund_summary.state} />
                <MoneyText money={order.refund_summary.money} />
              </AlertDescription>
            </Alert>
          )}
          {order.allowed_actions.includes('pay') && (
            <Button
              nativeButton={false}
              render={
                <Link
                  to='/shop/orders/$orderNo/pay'
                  params={{ orderNo: order.order_no }}
                />
              }
            >
              {t('Continue payment')}
            </Button>
          )}
          {(order.allowed_actions.includes('create_aftersale') ||
            order.allowed_actions.includes('view_aftersale')) && (
            <Button
              variant='outline'
              nativeButton={false}
              render={
                <Link
                  to='/shop/orders/$orderNo/aftersales'
                  params={{ orderNo: order.order_no }}
                />
              }
            >
              {t('After-sales support')}
            </Button>
          )}
          <Button variant='outline' onClick={() => void query.refetch()}>
            {t('Refresh')}
          </Button>
          <Link to='/shop/orders' search={{ page: 1 }}>
            {t('Back to orders')}
          </Link>
        </aside>
      </div>
    </>
  )
}
