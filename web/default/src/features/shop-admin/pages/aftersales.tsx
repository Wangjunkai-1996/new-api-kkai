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

import { shopPath } from '@/features/shop/api'
import {
  Pagination,
  ShopErrorNotice,
  ShopHeading,
  ShopLoading,
  ShopTime,
} from '@/features/shop/components/common'
import { useShopQuery } from '@/features/shop/hooks/use-shop'
import { AftersaleConversation } from '@/features/shop/pages/aftersales'
import {
  aftersaleSchema,
  orderSchema,
  pageSchema,
} from '@/features/shop/schemas'

import { AdminActionDialog } from '../components/action-dialog'

export function AdminAftersalesPage() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const query = useShopQuery(
    `/api/admin/shop/aftersales?page=${page}`,
    pageSchema(aftersaleSchema)
  )
  return (
    <>
      <ShopHeading title={t('After-sales support')} />
      <ShopErrorNotice error={query.error} />
      {query.isPending && <ShopLoading />}
      <div className='shop-table-wrap'>
        <table className='shop-table'>
          <thead>
            <tr>
              <th>{t('Support request')}</th>
              <th>{t('Order number')}</th>
              <th>{t('Created at')}</th>
            </tr>
          </thead>
          <tbody>
            {query.data?.items.map((item) => (
              <tr key={item.id}>
                <td>
                  <Link
                    to='/shop-admin/aftersales/$caseId'
                    params={{ caseId: item.id }}
                  >
                    {item.id}
                  </Link>
                </td>
                <td>{item.order_no}</td>
                <td>
                  <ShopTime value={item.created_at} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {query.data && (
        <Pagination
          page={page}
          total={query.data.total}
          pageSize={query.data.page_size}
          onPage={setPage}
        />
      )}
    </>
  )
}

export function AdminAftersalePage(props: { caseId: string }) {
  const { t } = useTranslation()
  const base = `/api/admin/shop/aftersales/${shopPath(props.caseId)}`
  const query = useShopQuery(base, aftersaleSchema)
  const order = useShopQuery(
    `/api/admin/shop/orders/${shopPath(query.data?.order_no ?? '')}`,
    orderSchema,
    { enabled: Boolean(query.data) }
  )
  if (query.isPending) return <ShopLoading />
  if (!query.data) return <ShopErrorNotice error={query.error} />
  const item = query.data
  const fields = order.data?.product_snapshot.delivery_fields ?? []
  return (
    <>
      <ShopHeading title={t('Support request')} description={item.id}>
        <Link
          to='/shop-admin/orders/$orderNo'
          params={{ orderNo: item.order_no }}
        >
          {t('View order')}
        </Link>
      </ShopHeading>
      <div className='shop-columns'>
        <AftersaleConversation item={item} admin />
        <aside className='shop-panel shop-receipt'>
          <h2>{t('Support resolution')}</h2>
          <ShopErrorNotice error={order.error} />
          {item.allowed_actions.includes('decide') && (
            <AdminActionDialog
              label={t('Record decision')}
              description={t('Explain the resolution clearly to the buyer.')}
              path={`${base}/decisions`}
              schema={aftersaleSchema}
              fields={[
                {
                  name: 'decision',
                  label: t('Decision'),
                  options: [
                    { value: 'accept', label: t('Accept') },
                    { value: 'resolve', label: t('Resolve') },
                    { value: 'reject', label: t('Reject') },
                    { value: 'close', label: t('Close') },
                  ],
                },
                {
                  name: 'public_reason',
                  label: t('Reason visible to buyer'),
                  type: 'textarea',
                },
                {
                  name: 'internal_reason',
                  label: t('Internal reason'),
                  type: 'textarea',
                  optional: true,
                },
              ]}
              body={(values) => ({ expected_version: item.version, ...values })}
            />
          )}
          {item.allowed_actions.includes('replace') &&
            order.data?.product_snapshot.template_version && (
              <AdminActionDialog
                label={t('Issue replacement')}
                description={t(
                  'The previous delivery becomes unavailable. The original warranty period does not restart.'
                )}
                path={`${base}/replacements`}
                schema={aftersaleSchema}
                fields={[
                  { name: 'reason', label: t('Reason') },
                  ...(order.data.product_snapshot.delivery_type === 'manual'
                    ? [
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
                          type: 'textarea' as const,
                          optional: true,
                        },
                      ]
                    : []),
                ]}
                body={(values) => ({
                  expected_version: item.version,
                  order_version: order.data?.version,
                  reason: values.reason,
                  ...(order.data?.product_snapshot.delivery_type === 'manual'
                    ? {
                        secret_fields: fields
                          .filter((field) => values[field.key])
                          .map((field) => ({
                            key: field.key,
                            value: values[field.key] ?? '',
                          })),
                        instructions: values.instructions,
                      }
                    : {}),
                })}
                proof={(values, key) => ({
                  scope: 'shop.delivery.create',
                  context: {
                    action: 'replacement',
                    target_id: item.order_no,
                    expected_version: order.data?.version ?? '',
                    idempotency_key: key,
                    details: {
                      reason: values.reason,
                      template_version:
                        order.data?.product_snapshot.template_version,
                      case_id: item.id,
                      case_version: item.version,
                    },
                  },
                })}
              />
            )}
        </aside>
      </div>
    </>
  )
}
