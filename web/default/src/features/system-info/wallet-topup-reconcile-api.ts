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
import { api } from '@/lib/api'
import { createServerError } from '@/lib/server-error-message'

export interface ReconcileScope {
  payment_provider: string
  provider_account_ref: string
  provider_environment: string
  tenant_id: number
  created_before: number
  created_after?: number
}
export type ReconcileCounts = Record<string, number>
export interface ReconcileReport {
  report_id: string
  scope_digest: string
  max_order_id: string
  counts: ReconcileCounts
  capability_gaps: string[]
  expires_at: number
}
export interface ReconcileRun {
  run_id: string
  scope_digest: string
  control_version: string
  status:
    | 'report_only'
    | 'queued'
    | 'running'
    | 'pausing'
    | 'paused'
    | 'completed'
    | 'failed'
  desired_state: string
  cursor: string
  counts: ReconcileCounts
  last_task_id: string
  owner_id: number
  updated_at: number
}
const endpoint = '/api/user/topup/reconcile-runs'
async function request<T>(
  method: 'get' | 'post',
  path: string,
  data?: unknown,
  key?: string,
  signal?: AbortSignal
): Promise<T> {
  const response = await api.request({
    method,
    url: path,
    data,
    signal,
    headers: key ? { 'Idempotency-Key': key } : undefined,
  })
  if (!response.data?.success || !response.data.data) {
    throw createServerError(
      response.data,
      'Payment reconciliation is unavailable.'
    )
  }
  return response.data.data
}
export function reportWalletReconciliation(
  scope: ReconcileScope,
  batchSize: number,
  maxQueriesPerSecond: number
) {
  return request<ReconcileReport>('post', `${endpoint}/report`, {
    scope,
    batch_size: batchSize,
    max_queries_per_second: maxQueriesPerSecond,
  })
}
export function startWalletReconciliation(
  report: ReconcileReport,
  authorizationRef: string,
  reason: string,
  key: string
) {
  return request<{ run: ReconcileRun; task_id: string }>(
    'post',
    endpoint,
    {
      report_id: report.report_id,
      scope_digest: report.scope_digest,
      authorization_ref: authorizationRef,
      reason,
    },
    key
  )
}
export function getWalletReconciliation(runId: string, signal?: AbortSignal) {
  return request<ReconcileRun>(
    'get',
    `${endpoint}/${encodeURIComponent(runId)}`,
    undefined,
    undefined,
    signal
  )
}
export function controlWalletReconciliation(
  run: ReconcileRun,
  action: 'pause' | 'resume',
  reason: string,
  authorizationRef: string
) {
  const data =
    action === 'pause'
      ? { expected_control_version: run.control_version, reason }
      : {
          expected_control_version: run.control_version,
          scope_digest: run.scope_digest,
          authorization_ref: authorizationRef,
        }
  return request<ReconcileRun>(
    'post',
    `${endpoint}/${encodeURIComponent(run.run_id)}/${action}`,
    data
  )
}
