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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useWalletPaymentIdentity } from '@/features/wallet/hooks/use-wallet-payment-flow'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import {
  controlWalletReconciliation,
  getWalletReconciliation,
  reportWalletReconciliation,
  startWalletReconciliation,
  type ReconcileReport,
  type ReconcileRun,
  type ReconcileScope,
} from '../wallet-topup-reconcile-api'

const RUN_LABELS = {
  report_only: 'Report only',
  queued: 'Queued',
  running: 'Running',
  pausing: 'Pausing',
  paused: 'Paused',
  completed: 'Completed',
  failed: 'Failed',
} as const
const COUNT_LABELS: Record<string, string> = {
  total: 'Orders in scope',
  missing_snapshot: 'Orders missing original payment details',
  reviewed: 'Reviewed orders',
  credited: 'Credited orders',
  closed: 'Closed orders',
  review_required: 'Orders requiring review',
  retry_failed: 'Failed checks',
}
export function WalletTopupReconcileRun() {
  const user = useAuthStore((state) => state.auth.user)
  const session = useAuthStore((state) => state.auth.session?.sid)
  if (user?.role !== ROLE.SUPER_ADMIN || !session) return null
  return <WalletTopupReconcileControls key={`${user.id}:${session}`} />
}
function WalletTopupReconcileControls() {
  const { t } = useTranslation()
  const identity = useWalletPaymentIdentity()
  const client = useQueryClient()
  const [provider, setProvider] = useState('')
  const [account, setAccount] = useState('')
  const [environment, setEnvironment] = useState('prod')
  const [tenant, setTenant] = useState('0')
  const [before, setBefore] = useState('')
  const [after, setAfter] = useState('')
  const [batch, setBatch] = useState('100')
  const [rate, setRate] = useState('1')
  const [authorization, setAuthorization] = useState('')
  const [reason, setReason] = useState('')
  const [report, setReport] = useState<ReconcileReport | null>(null)
  const [runId, setRunId] = useState('')
  const [runDraft, setRunDraft] = useState('')
  const startKey = useRef<string | null>(null)
  const operationBusy = useRef(false)
  const queryKey = ['wallet-payment', ...identity, 'history-run', runId]
  const runQuery = useQuery({
    queryKey,
    queryFn: ({ signal }) => getWalletReconciliation(runId, signal),
    enabled: !!runId,
    retry: false,
    refetchOnWindowFocus: false,
    refetchInterval: (query) => {
      if (
        document.hidden ||
        !query.state.data ||
        !['queued', 'running', 'pausing'].includes(query.state.data.status)
      ) {
        return false
      }
      return 8000
    },
    refetchIntervalInBackground: false,
  })
  const mutation = useMutation({
    mutationFn: async (action: 'report' | 'start' | 'pause' | 'resume') => {
      if (operationBusy.current) return
      operationBusy.current = true
      try {
        if (action === 'report') {
          const scope: ReconcileScope = {
            payment_provider: provider.trim(),
            provider_account_ref: account.trim(),
            provider_environment: environment,
            tenant_id: Number(tenant),
            created_before: Date.parse(before) / 1000,
          }
          if (after) scope.created_after = Date.parse(after) / 1000
          if (
            !scope.payment_provider ||
            !scope.provider_account_ref ||
            scope.provider_account_ref.length > 128 ||
            !Number.isSafeInteger(scope.tenant_id) ||
            scope.tenant_id < 0 ||
            !Number.isFinite(scope.created_before) ||
            scope.created_before <= 0 ||
            scope.created_before > Date.now() / 1000 ||
            (scope.created_after !== undefined &&
              (!Number.isFinite(scope.created_after) ||
                scope.created_after >= scope.created_before)) ||
            !Number.isSafeInteger(Number(batch)) ||
            Number(batch) < 1 ||
            Number(batch) > 100 ||
            !Number.isSafeInteger(Number(rate)) ||
            Number(rate) < 1 ||
            Number(rate) > 10
          ) {
            throw new Error('Invalid report scope')
          }
          const next = await reportWalletReconciliation(
            scope,
            Number(batch),
            Number(rate)
          )
          setReport(next)
          startKey.current = null
          return
        }
        if (action === 'start') {
          if (
            !report ||
            report.expires_at <= Date.now() / 1000 ||
            !authorization.trim() ||
            !reason.trim()
          ) {
            throw new Error('Missing report or authorization')
          }
          startKey.current ??= crypto.randomUUID()
          const result = await startWalletReconciliation(
            report,
            authorization.trim(),
            reason.trim(),
            startKey.current
          )
          setRunId(result.run.run_id)
          setRunDraft(result.run.run_id)
          client.setQueryData(
            ['wallet-payment', ...identity, 'history-run', result.run.run_id],
            result.run
          )
          return
        }
        const run = runQuery.data
        if (
          !run ||
          (action === 'pause' && !reason.trim()) ||
          (action === 'resume' && !authorization.trim())
        ) {
          throw new Error('Missing run control input')
        }
        const next = await controlWalletReconciliation(
          run,
          action,
          reason.trim(),
          authorization.trim()
        )
        client.setQueryData<ReconcileRun>(queryKey, (previous) =>
          previous &&
          BigInt(previous.control_version) > BigInt(next.control_version)
            ? previous
            : next
        )
      } finally {
        operationBusy.current = false
      }
    },
    retry: false,
    onError: () => {
      if (runId) void runQuery.refetch()
    },
  })
  const invalidateReport = () => {
    setReport(null)
    startKey.current = null
  }
  const scopeInputs = [
    {
      key: 'provider',
      label: 'Payment provider',
      value: provider,
      set: setProvider,
    },
    {
      key: 'account',
      label: 'Original merchant account',
      value: account,
      set: setAccount,
    },
    { key: 'tenant', label: 'Tenant ID', value: tenant, set: setTenant },
    {
      key: 'before',
      label: 'Created before',
      value: before,
      set: setBefore,
      type: 'datetime-local',
    },
    {
      key: 'after',
      label: 'Created after (optional)',
      value: after,
      set: setAfter,
      type: 'datetime-local',
    },
    {
      key: 'batch',
      label: 'Batch size (1–100)',
      value: batch,
      set: setBatch,
      type: 'number',
    },
    {
      key: 'rate',
      label: 'Queries per second (up to 10)',
      value: rate,
      set: setRate,
      type: 'number',
    },
  ]
  const run = runQuery.data
  return (
    <section
      className='bg-card space-y-4 rounded-lg border p-4 sm:p-5'
      aria-labelledby='wallet-reconcile-title'
    >
      <div>
        <h3 id='wallet-reconcile-title' className='font-semibold'>
          {t('Historical top-up reconciliation')}
        </h3>
        <p className='text-muted-foreground mt-1 text-sm'>
          {t(
            'Generate a read-only report first. Processing historical orders requires separate authorization for the frozen scope.'
          )}
        </p>
      </div>
      <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
        {scopeInputs.map((input) => (
          <label key={input.key} className='space-y-1 text-sm'>
            {t(input.label)}
            <Input
              type={input.type ?? 'text'}
              maxLength={input.key === 'account' ? 128 : undefined}
              value={input.value}
              onChange={(event) => {
                input.set(event.target.value)
                invalidateReport()
              }}
              disabled={mutation.isPending}
            />
          </label>
        ))}
        <label className='space-y-1 text-sm'>
          {t('Payment environment')}
          <select
            className='bg-background block h-10 w-full rounded-md border px-3'
            value={environment}
            disabled={mutation.isPending}
            onChange={(event) => {
              setEnvironment(event.target.value)
              invalidateReport()
            }}
          >
            <option value='prod'>{t('Production')}</option>
            <option value='test'>{t('Test')}</option>
            <option value='unknown'>{t('Unknown original environment')}</option>
          </select>
        </label>
      </div>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Use unknown only for records missing the original merchant identity. These records require review; current merchant settings do not prove their payment state.'
        )}
      </p>
      <Button
        variant='outline'
        disabled={mutation.isPending}
        onClick={() => mutation.mutate('report')}
      >
        {t('Generate read-only report')}
      </Button>
      {report && (
        <div className='bg-muted/30 space-y-2 rounded-lg border p-4 text-sm'>
          <p>
            {t('Report ID')}:{' '}
            <span className='break-all'>{report.report_id}</span>
          </p>
          <p>
            {t('Frozen scope')}:{' '}
            <span className='break-all'>{report.scope_digest}</span>
          </p>
          <p>
            {t('Order boundary')}: {report.max_order_id}
          </p>
          <p>
            {t('Report expires')}:{' '}
            {new Date(report.expires_at * 1000).toLocaleString()}
          </p>
          <ReconcileCountList counts={report.counts} />
          {report.capability_gaps.length > 0 && (
            <p>
              {t(
                'Some orders lack verified provider capabilities and will require review.'
              )}
            </p>
          )}
        </div>
      )}
      <div className='grid gap-3 sm:grid-cols-2'>
        <label className='space-y-1 text-sm'>
          {t('Data-processing authorization reference')}
          <Input
            value={authorization}
            maxLength={200}
            onChange={(event) => setAuthorization(event.target.value)}
          />
        </label>
        <label className='space-y-1 text-sm'>
          {t('Operation reason')}
          <Input
            value={reason}
            maxLength={500}
            onChange={(event) => setReason(event.target.value)}
          />
        </label>
      </div>
      <p className='text-muted-foreground text-sm'>
        {t(
          'An authorization reference must be checked by an authorized operator. A release approval does not authorize historical data processing.'
        )}
      </p>
      <Button
        disabled={
          !report ||
          mutation.isPending ||
          !authorization.trim() ||
          !reason.trim()
        }
        onClick={() => mutation.mutate('start')}
      >
        {t('Start authorized batch')}
      </Button>
      <div className='flex flex-wrap items-end gap-2 border-t pt-4'>
        <label className='min-w-0 flex-1 space-y-1 text-sm'>
          {t('Batch run ID')}
          <Input
            value={runDraft}
            onChange={(event) => setRunDraft(event.target.value)}
            maxLength={128}
          />
        </label>
        <Button
          variant='outline'
          disabled={!runDraft.trim() || mutation.isPending}
          onClick={() => {
            if (runDraft.trim() === runId) void runQuery.refetch()
            else setRunId(runDraft.trim())
          }}
        >
          {t('Load batch')}
        </Button>
      </div>
      {runQuery.isFetching && <p role='status'>{t('Refreshing...')}</p>}
      {(mutation.isError || runQuery.isError) && (
        <p role='alert' className='text-destructive text-sm'>
          {t(
            'The batch request could not be confirmed. Refresh its status before retrying.'
          )}
        </p>
      )}
      {run && (
        <div className='space-y-3 rounded-lg border p-4' aria-live='polite'>
          <p className='font-medium'>
            {t(RUN_LABELS[run.status] ?? 'Status unavailable')}
          </p>
          <p className='text-muted-foreground text-xs break-all'>
            {run.run_id}
          </p>
          <p className='text-muted-foreground text-xs break-all'>
            {t('Frozen scope')}: {run.scope_digest}
          </p>
          <ReconcileCountList counts={run.counts} />
          <div className='flex flex-wrap gap-2'>
            <Button
              variant='outline'
              onClick={() => void runQuery.refetch()}
              disabled={runQuery.isFetching}
            >
              {t('Refresh')}
            </Button>
            {['queued', 'running'].includes(run.status) && (
              <Button
                variant='outline'
                disabled={mutation.isPending || !reason.trim()}
                onClick={() => mutation.mutate('pause')}
              >
                {t('Pause batch')}
              </Button>
            )}
            {run.status === 'paused' && (
              <Button
                disabled={mutation.isPending || !authorization.trim()}
                onClick={() => mutation.mutate('resume')}
              >
                {t('Resume authorized batch')}
              </Button>
            )}
          </div>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Pausing stops new historical checks after in-flight work settles. Payment receipts and normal orders continue processing.'
            )}
          </p>
        </div>
      )}
    </section>
  )
}
function ReconcileCountList(props: { counts: Record<string, number> }) {
  const { t } = useTranslation()
  return (
    <dl className='flex flex-wrap gap-x-5 gap-y-2 text-sm'>
      {Object.entries(props.counts).map(([key, value]) => (
        <div key={key}>
          <dt className='text-muted-foreground'>
            {t(COUNT_LABELS[key] ?? 'Orders')}
          </dt>
          <dd className='font-medium tabular-nums'>{value}</dd>
        </div>
      ))}
    </dl>
  )
}
