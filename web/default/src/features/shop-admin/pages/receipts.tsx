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
import { z } from 'zod'

import { shopPath, shopRequest } from '@/features/shop/api'
import {
  MoneyText,
  Pagination,
  ShopErrorNotice,
  ShopHeading,
  ShopLoading,
  ShopState,
} from '@/features/shop/components/common'
import { usePrivateResult, useShopQuery } from '@/features/shop/hooks/use-shop'
import { parsePrice } from '@/features/shop/lib'
import {
  pageSchema,
  receiptSchema,
  refundSchema,
  type Money,
} from '@/features/shop/schemas'

import { evidenceIds, utcTime } from '../action-values'
import { AdminActionDialog } from '../components/action-dialog'

async function digest(value: string) {
  const bytes = await crypto.subtle.digest(
    'SHA-256',
    new TextEncoder().encode(value)
  )
  return Array.from(new Uint8Array(bytes), (byte) =>
    byte.toString(16).padStart(2, '0')
  ).join('')
}
const evidenceSchema = z.object({ evidence_id: z.string() })
const evidenceDetailSchema = z.object({
  id: z.string().optional(),
  evidence_id: z.string().optional(),
  reference: z.string(),
  summary: z.string(),
  observed_at: z.string(),
  observed_status: z.string(),
})

export function AdminReceiptsPage() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const query = useShopQuery(
    `/api/admin/shop/receipts?page=${page}`,
    pageSchema(receiptSchema)
  )
  return (
    <>
      <ShopHeading
        title={t('Payment receipts')}
        description={t(
          'Review provider receipts separately from goods and additional payments.'
        )}
      />
      <ShopErrorNotice error={query.error} />
      {query.isPending && <ShopLoading />}
      <div className='shop-table-wrap'>
        <table className='shop-table'>
          <thead>
            <tr>
              <th>{t('Receipt')}</th>
              <th>{t('Provider transaction ID')}</th>
              <th>{t('Amount')}</th>
              <th>{t('Status')}</th>
            </tr>
          </thead>
          <tbody>
            {query.data?.items.map((receipt) => (
              <tr key={receipt.id}>
                <td>
                  <Link
                    to='/shop-admin/receipts/$receiptId'
                    params={{ receiptId: receipt.id }}
                  >
                    {receipt.id}
                  </Link>
                </td>
                <td>{receipt.provider_transaction_id}</td>
                <td>
                  <MoneyText money={receipt.money} />
                </td>
                <td>
                  <ShopState state={receipt.state} />
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

export function AdminReceiptPage(props: { receiptId: string }) {
  const { t } = useTranslation()
  const base = `/api/admin/shop/receipts/${shopPath(props.receiptId)}`
  const query = useShopQuery(base, receiptSchema)
  const evidence = usePrivateResult<z.infer<typeof evidenceDetailSchema>>(
    props.receiptId
  )
  if (query.isPending) return <ShopLoading />
  if (!query.data) return <ShopErrorNotice error={query.error} />
  const receipt = query.data
  const can = (action: string) => receipt.allowed_actions.includes(action)
  const reason = {
    name: 'reason',
    label: t('Reason'),
    type: 'textarea' as const,
  }
  const refs = { name: 'evidence_refs', label: t('Evidence IDs') }
  const refundKinds = [
    { value: 'goods' as const, label: t('Goods refund') },
    { value: 'extra' as const, label: t('Additional payment refund') },
    { value: 'mismatch' as const, label: t('Mismatched payment refund') },
  ].filter((kind) => receipt.refundable_money_by_kind[kind.value])
  const adjustmentMoney = (values: Record<string, string>): Money => {
    const money = parsePrice(
      values.amount,
      receipt.money.currency,
      receipt.money.exponent
    )
    if (!money || money.amount_minor === '0') throw new Error()
    return money
  }
  const evidenceBody = (values: Record<string, string>) => ({
    receipt_id: receipt.id,
    receipt_version: receipt.version,
    source: 'merchant_console',
    provider_transaction_id: receipt.provider_transaction_id,
    ...(values.provider_refund_or_adjustment_id
      ? {
          provider_refund_or_adjustment_id:
            values.provider_refund_or_adjustment_id,
        }
      : {}),
    money: adjustmentMoney(values),
    observed_status: values.observed_status,
    observed_at: utcTime(values.observed_at),
    reference: values.reference,
    summary: values.summary,
    ...(values.correction_of ? { correction_of: values.correction_of } : {}),
  })
  return (
    <>
      <ShopHeading title={t('Payment receipt')} description={receipt.id}>
        {receipt.order_no && (
          <Link
            to='/shop-admin/orders/$orderNo'
            params={{ orderNo: receipt.order_no }}
          >
            {t('View order')}
          </Link>
        )}
      </ShopHeading>
      <div className='shop-columns'>
        <div className='shop-stack'>
          <section className='shop-panel'>
            <h2>{t('Receipt details')}</h2>
            <MoneyText money={receipt.money} />
            <p>
              {receipt.provider} · {receipt.provider_transaction_id}
            </p>
            <p>{receipt.public_summary}</p>
            <div className='shop-actions'>
              {can('assign') && (
                <AdminActionDialog
                  label={t('Assign review')}
                  description={t(
                    'Assign an authorized operator to review this receipt.'
                  )}
                  path={`${base}/assign`}
                  schema={receiptSchema}
                  fields={[
                    { name: 'assignee_id', label: t('Operator ID') },
                    reason,
                  ]}
                  body={(values) => ({
                    expected_version: receipt.version,
                    ...values,
                  })}
                />
              )}
              {can('reconcile') && (
                <AdminActionDialog
                  label={t('Reconcile receipt')}
                  description={t(
                    'The server verifies this exact provider transaction. It cannot be reassigned to another order.'
                  )}
                  path={`${base}/reconcile`}
                  schema={receiptSchema}
                  fields={[reason]}
                  body={(values) => ({
                    expected_version: receipt.version,
                    reason: values.reason,
                  })}
                />
              )}
              {can('refund') && refundKinds.length > 0 && (
                <AdminActionDialog
                  label={t('Approve refund')}
                  description={t(
                    'The server derives the refundable amount. Approval does not execute a transfer.'
                  )}
                  path={`${base}/refunds`}
                  schema={refundSchema}
                  fields={[
                    {
                      name: 'kind',
                      label: t('Refund type'),
                      options: refundKinds,
                      defaultValue: refundKinds[0]?.value,
                    },
                    reason,
                    refs,
                  ]}
                  body={(values) => ({
                    expected_version: receipt.version,
                    kind: values.kind,
                    reason: values.reason,
                    evidence_refs: evidenceIds(values.evidence_refs),
                  })}
                  proof={(values, key) => ({
                    scope: 'shop.refund.manage',
                    context: {
                      action: 'approve',
                      target_id: receipt.id,
                      expected_version: receipt.version,
                      idempotency_key: key,
                      details: {
                        kind: values.kind,
                        money:
                          receipt.refundable_money_by_kind[
                            values.kind as 'goods' | 'extra' | 'mismatch'
                          ],
                        reason: values.reason,
                        evidence_ids: evidenceIds(values.evidence_refs),
                      },
                    },
                  })}
                />
              )}
            </div>
          </section>
          <section className='shop-panel'>
            <h2>{t('Payment evidence')}</h2>
            <p className='shop-muted'>
              {t(
                'Enter evidence from the merchant console. References are never fetched by the server.'
              )}{' '}
              {t('Corrections create a new evidence record.')}
            </p>
            {can('create_evidence') && (
              <AdminActionDialog
                label={t('Register payment evidence')}
                description={receipt.provider_transaction_id}
                path='/api/admin/shop/evidence'
                schema={evidenceSchema}
                fields={[
                  { name: 'amount', label: t('Observed amount') },
                  {
                    name: 'observed_status',
                    label: t('Observed status'),
                    options: [
                      { value: 'pending', label: t('Pending') },
                      { value: 'succeeded', label: t('Succeeded') },
                      { value: 'failed', label: t('Failed') },
                      { value: 'disputed', label: t('Disputed') },
                      { value: 'unknown', label: t('Unknown') },
                      { value: 'paid', label: t('Paid') },
                      { value: 'unpaid', label: t('Unpaid') },
                      { value: 'not_refunded', label: t('Not refunded') },
                      { value: 'refund_failed', label: t('Refund failed') },
                      { value: 'not_executed', label: t('Not executed') },
                      {
                        value: 'refund_succeeded',
                        label: t('Refund succeeded'),
                      },
                    ],
                    defaultValue: 'unknown',
                  },
                  {
                    name: 'observed_at',
                    label: t('Observed at'),
                    type: 'datetime-local',
                  },
                  {
                    name: 'provider_refund_or_adjustment_id',
                    label: t('Provider refund or adjustment ID'),
                    optional: true,
                  },
                  {
                    name: 'reference',
                    label: t('Evidence reference'),
                    type: 'textarea',
                    maxLength: 8000,
                  },
                  {
                    name: 'summary',
                    label: t('Evidence summary'),
                    type: 'textarea',
                  },
                  {
                    name: 'correction_of',
                    label: t('Corrected evidence ID'),
                    optional: true,
                  },
                ]}
                body={evidenceBody}
                proof={async (values, key) => {
                  const body = evidenceBody(values)
                  return {
                    scope: 'shop.payment.evidence.create',
                    context: {
                      action: 'create',
                      target_id: receipt.id,
                      expected_version: receipt.version,
                      idempotency_key: key,
                      details: {
                        source: body.source,
                        provider_transaction_id: body.provider_transaction_id,
                        money: body.money,
                        observed_status: body.observed_status,
                        observed_at: body.observed_at,
                        reference_digest: await digest(body.reference),
                        summary_digest: await digest(body.summary),
                        ...(body.provider_refund_or_adjustment_id
                          ? {
                              provider_refund_or_adjustment_id:
                                body.provider_refund_or_adjustment_id,
                            }
                          : {}),
                        ...(body.correction_of
                          ? { correction_of: body.correction_of }
                          : {}),
                      },
                    },
                  }
                }}
              />
            )}
            {receipt.evidence_ids.map((id) => (
              <button
                type='button'
                key={id}
                className='block text-left break-all underline'
                onClick={() =>
                  evidence.read((signal) =>
                    shopRequest(
                      `/api/admin/shop/evidence/${shopPath(id)}`,
                      evidenceDetailSchema,
                      { signal }
                    )
                  )
                }
              >
                {id}
              </button>
            ))}
            {evidence.value && (
              <div className='shop-secret'>
                <p>
                  {evidence.value.observed_status} ·{' '}
                  {evidence.value.observed_at}
                </p>
                <p>{evidence.value.reference}</p>
                <p>{evidence.value.summary}</p>
              </div>
            )}
            <ShopErrorNotice error={evidence.error} />
          </section>
          <section className='shop-panel'>
            <h2>{t('External adjustments')}</h2>
            {can('record_adjustment') && (
              <AdminActionDialog
                label={t('Register adjustment')}
                description={t(
                  'Record a provider refund or chargeback with independent evidence.'
                )}
                path={`${base}/adjustments`}
                schema={receiptSchema}
                fields={[
                  {
                    name: 'kind',
                    label: t('Type'),
                    options: [
                      { value: 'external_refund', label: t('External refund') },
                      { value: 'chargeback', label: t('Chargeback') },
                    ],
                  },
                  {
                    name: 'provider_adjustment_ref',
                    label: t('Provider adjustment reference'),
                  },
                  { name: 'amount', label: t('Amount') },
                  {
                    name: 'state',
                    label: t('Status'),
                    options: [
                      { value: 'pending', label: t('Pending') },
                      { value: 'succeeded', label: t('Succeeded') },
                      { value: 'failed', label: t('Failed') },
                      { value: 'disputed', label: t('Disputed') },
                    ],
                  },
                  reason,
                  refs,
                ]}
                body={(values) => ({
                  expected_version: receipt.version,
                  kind: values.kind,
                  provider_adjustment_ref: values.provider_adjustment_ref,
                  money: adjustmentMoney(values),
                  state: values.state,
                  evidence_refs: evidenceIds(values.evidence_refs),
                  reason: values.reason,
                })}
                proof={(values, key) => ({
                  scope: 'shop.payment.adjustment.manage',
                  context: {
                    action: 'record',
                    target_id: receipt.id,
                    expected_version: receipt.version,
                    idempotency_key: key,
                    details: {
                      kind: values.kind,
                      money: adjustmentMoney(values),
                      provider_adjustment_ref: values.provider_adjustment_ref,
                      state: values.state,
                      reason: values.reason,
                      evidence_ids: evidenceIds(values.evidence_refs),
                    },
                  },
                })}
              />
            )}
            {receipt.adjustments.map((adjustment) => (
              <div key={adjustment.id}>
                <p>
                  {adjustment.id} · <ShopState state={adjustment.state} /> ·{' '}
                  <MoneyText money={adjustment.money} />
                </p>
                {can('review_adjustment') && (
                  <AdminActionDialog
                    label={t('Review adjustment')}
                    description={adjustment.id}
                    path={`${base}/adjustments/${shopPath(
                      adjustment.id
                    )}/review`}
                    schema={receiptSchema}
                    fields={[
                      {
                        name: 'state',
                        label: t('Status'),
                        options: [
                          { value: 'succeeded', label: t('Succeeded') },
                          { value: 'failed', label: t('Failed') },
                          { value: 'disputed', label: t('Disputed') },
                        ],
                      },
                      reason,
                      refs,
                    ]}
                    body={(values) => ({
                      expected_version: receipt.version,
                      state: values.state,
                      reason: values.reason,
                      evidence_refs: evidenceIds(values.evidence_refs),
                    })}
                    proof={(values, key) => ({
                      scope: 'shop.payment.adjustment.manage',
                      context: {
                        action: 'review',
                        target_id: receipt.id,
                        expected_version: receipt.version,
                        idempotency_key: key,
                        details: {
                          adjustment_id: adjustment.id,
                          state: values.state,
                          reason: values.reason,
                          evidence_ids: evidenceIds(values.evidence_refs),
                        },
                      },
                    })}
                  />
                )}
              </div>
            ))}
          </section>
        </div>
        <aside className='shop-panel shop-receipt'>
          <h2>{t('Refunds')}</h2>
          {receipt.refunds.map((refund) => (
            <Link
              key={refund.id}
              to='/shop-admin/refunds/$refundId'
              params={{ refundId: refund.id }}
            >
              {refund.id} · <MoneyText money={refund.money} />
            </Link>
          ))}
        </aside>
      </div>
    </>
  )
}
