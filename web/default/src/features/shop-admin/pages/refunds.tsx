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
  MoneyText,
  Pagination,
  ShopErrorNotice,
  ShopHeading,
  ShopLoading,
  ShopState,
  ShopTime,
} from '@/features/shop/components/common'
import { useShopQuery } from '@/features/shop/hooks/use-shop'
import { pageSchema, refundSchema } from '@/features/shop/schemas'

import { evidenceIds, utcTime } from '../action-values'
import { AdminActionDialog } from '../components/action-dialog'

export function AdminRefundsPage() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const query = useShopQuery(
    `/api/admin/shop/refunds?page=${page}`,
    pageSchema(refundSchema)
  )
  return (
    <>
      <ShopHeading
        title={t('Refunds')}
        description={t(
          'Unknown transfer results must be verified before another attempt.'
        )}
      />
      <ShopErrorNotice error={query.error} />
      {query.isPending && <ShopLoading />}
      <div className='shop-table-wrap'>
        <table className='shop-table'>
          <thead>
            <tr>
              <th>{t('Refund')}</th>
              <th>{t('Order number')}</th>
              <th>{t('Amount')}</th>
              <th>{t('Status')}</th>
            </tr>
          </thead>
          <tbody>
            {query.data?.items.map((refund) => (
              <tr key={refund.id}>
                <td>
                  <Link
                    to='/shop-admin/refunds/$refundId'
                    params={{ refundId: refund.id }}
                  >
                    {refund.id}
                  </Link>
                </td>
                <td>{refund.order_no ?? '—'}</td>
                <td>
                  <MoneyText money={refund.money} />
                </td>
                <td>
                  <ShopState state={refund.state} />
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

export function AdminRefundPage(props: { refundId: string }) {
  const { t } = useTranslation()
  const base = `/api/admin/shop/refunds/${shopPath(props.refundId)}`
  const query = useShopQuery(base, refundSchema)
  if (query.isPending) return <ShopLoading />
  if (!query.data) return <ShopErrorNotice error={query.error} />
  const refund = query.data
  const reason = {
    name: 'reason',
    label: t('Reason'),
    type: 'textarea' as const,
  }
  const refs = { name: 'evidence_refs', label: t('Evidence IDs') }
  return (
    <>
      <ShopHeading title={t('Refund details')} description={refund.id}>
        <Link
          to='/shop-admin/receipts/$receiptId'
          params={{ receiptId: refund.receipt_id }}
        >
          {t('View payment receipt')}
        </Link>
      </ShopHeading>
      <section className='shop-panel'>
        <MoneyText money={refund.money} />
        <p>{refund.public_note}</p>
        <p className='shop-muted'>
          {t(
            'Each attempt represents one transfer in the original payment channel. This page does not move funds automatically.'
          )}
        </p>
        <div className='shop-actions'>
          {refund.allowed_actions.includes('register_attempt') && (
            <AdminActionDialog
              label={t('Register refund attempt')}
              description={t(
                'Confirm there is no unresolved attempt before operating the original payment channel.'
              )}
              path={`${base}/attempts`}
              schema={refundSchema}
              fields={[reason, refs]}
              body={(values) => ({
                expected_version: refund.version,
                reason: values.reason,
                preflight_evidence_refs: evidenceIds(values.evidence_refs),
              })}
              proof={(values, key) => ({
                scope: 'shop.refund.manage',
                context: {
                  action: 'register_attempt',
                  target_id: refund.id,
                  expected_version: refund.version,
                  idempotency_key: key,
                  details: {
                    reason: values.reason,
                    evidence_ids: evidenceIds(values.evidence_refs),
                  },
                },
              })}
            />
          )}
          {refund.allowed_actions.includes('cancel') && (
            <AdminActionDialog
              label={t('Cancel refund approval')}
              description={t(
                'Cancellation is only allowed when no transfer may have occurred.'
              )}
              path={`${base}/cancel`}
              schema={refundSchema}
              fields={[reason, refs]}
              body={(values) => ({
                expected_version: refund.version,
                reason: values.reason,
                evidence_refs: evidenceIds(values.evidence_refs),
              })}
              proof={(values, key) => ({
                scope: 'shop.refund.manage',
                context: {
                  action: 'cancel',
                  target_id: refund.id,
                  expected_version: refund.version,
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
        {refund.attempts.map((attempt) => (
          <section key={attempt.id} className='shop-panel'>
            <h2>{attempt.id}</h2>
            <p>
              <ShopState state={attempt.state} /> · {attempt.channel_ref ?? '—'}{' '}
              · <ShopTime value={attempt.operated_at} />
            </p>
            <div className='shop-actions'>
              {refund.allowed_actions.includes('record_attempt') && (
                <AdminActionDialog
                  label={t('Record transfer operation')}
                  description={t(
                    'Record exactly what was submitted to the payment provider. An operation record does not confirm success.'
                  )}
                  path={`${base}/attempts/${shopPath(attempt.id)}/record`}
                  schema={refundSchema}
                  fields={[
                    {
                      name: 'channel_ref',
                      label: t('Provider refund reference'),
                      optional: true,
                    },
                    {
                      name: 'operated_at',
                      label: t('Operated at'),
                      type: 'datetime-local',
                    },
                    refs,
                    {
                      name: 'public_note',
                      label: t('Note visible to buyer'),
                      type: 'textarea',
                      optional: true,
                    },
                  ]}
                  body={(values) => ({
                    expected_version: refund.version,
                    ...(values.channel_ref
                      ? { channel_ref: values.channel_ref }
                      : {}),
                    operated_at: utcTime(values.operated_at),
                    evidence_refs: evidenceIds(values.evidence_refs),
                    public_note: values.public_note,
                  })}
                  proof={(values, key) => ({
                    scope: 'shop.refund.manage',
                    context: {
                      action: 'record_attempt',
                      target_id: refund.id,
                      expected_version: refund.version,
                      idempotency_key: key,
                      details: {
                        attempt_id: attempt.id,
                        operated_at: utcTime(values.operated_at),
                        evidence_ids: evidenceIds(values.evidence_refs),
                        public_note: values.public_note,
                        ...(values.channel_ref
                          ? { channel_ref: values.channel_ref }
                          : {}),
                      },
                    },
                  })}
                />
              )}
              {refund.allowed_actions.includes('verify_attempt') && (
                <AdminActionDialog
                  label={t('Verify transfer result')}
                  description={t(
                    'Use provider evidence. Unknown results keep retrying blocked.'
                  )}
                  path={`${base}/attempts/${shopPath(attempt.id)}/verify`}
                  schema={refundSchema}
                  fields={[
                    {
                      name: 'conclusion',
                      label: t('Conclusion'),
                      options: [
                        { value: 'unknown', label: t('Unknown') },
                        { value: 'succeeded', label: t('Succeeded') },
                        { value: 'failed', label: t('Failed') },
                      ],
                    },
                    refs,
                  ]}
                  body={(values) => ({
                    expected_version: refund.version,
                    conclusion: values.conclusion,
                    evidence_refs: evidenceIds(values.evidence_refs),
                  })}
                  proof={(values, key) => ({
                    scope: 'shop.refund.manage',
                    context: {
                      action: 'verify_attempt',
                      target_id: refund.id,
                      expected_version: refund.version,
                      idempotency_key: key,
                      details: {
                        attempt_id: attempt.id,
                        conclusion: values.conclusion,
                        evidence_ids: evidenceIds(values.evidence_refs),
                      },
                    },
                  })}
                />
              )}
            </div>
          </section>
        ))}
      </section>
    </>
  )
}
