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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { WalletTopupReconcileRun } from '../components/wallet-topup-reconcile-run'
import type { ReconcileRun } from '../wallet-topup-reconcile-api'

const api = vi.hoisted(() => ({
  reportWalletReconciliation: vi.fn(),
  startWalletReconciliation: vi.fn(),
  getWalletReconciliation: vi.fn(),
  controlWalletReconciliation: vi.fn(),
}))
vi.mock('../wallet-topup-reconcile-api', () => api)
vi.mock('@/features/site-config', () => ({
  useSiteConfig: () => ({ data: { tenant_id: 0 } }),
}))
let client: QueryClient
function renderRun(role = 100) {
  useAuthStore.getState().auth.setBundle({
    access_token: 'test',
    token_type: 'Bearer',
    access_expires_at: 4_000_000_000,
    user: { id: 1, role, username: 'operator' },
    session: {
      sid: 'session',
      current: true,
      login_method: 'password',
      ip: '',
      user_agent: '',
      created_at: 1,
      last_active_at: 1,
      expires_at: 4_000_000_000,
    },
  })
  return render(
    <QueryClientProvider client={client}>
      <WalletTopupReconcileRun />
    </QueryClientProvider>
  )
}
const run: ReconcileRun = {
  run_id: 'RUN-1',
  scope_digest: 'scope-1',
  control_version: '1',
  status: 'running',
  desired_state: 'running',
  cursor: '0',
  counts: {
    reviewed: 2,
    credited: 1,
    closed: 0,
    review_required: 1,
    retry_failed: 0,
  },
  last_task_id: 'task-1',
  owner_id: 1,
  updated_at: 1_790_000_000,
}
beforeEach(() => {
  vi.resetAllMocks()
  client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity },
      mutations: { retry: false },
    },
  })
})
afterEach(() => {
  client.clear()
  useAuthStore.getState().auth.reset()
})
it('does not expose historical business-data controls to ordinary administrators', () => {
  renderRun(10)
  expect(
    screen.queryByText('Historical top-up reconciliation')
  ).not.toBeInTheDocument()
  expect(api.getWalletReconciliation).not.toHaveBeenCalled()
})
it('creates only a scoped read-only report until separate authorization is entered', async () => {
  api.reportWalletReconciliation.mockResolvedValue({
    report_id: 'REPORT-1',
    scope_digest: 'scope-1',
    max_order_id: '20',
    counts: { total: 20, missing_snapshot: 2 },
    capability_gaps: [],
    expires_at: 4_000_000_000,
  })
  renderRun()
  fireEvent.change(screen.getByLabelText('Payment provider'), {
    target: { value: 'epay' },
  })
  fireEvent.change(screen.getByLabelText('Original merchant account'), {
    target: { value: 'account-1' },
  })
  fireEvent.change(screen.getByLabelText('Created before'), {
    target: { value: '2026-10-01T00:00' },
  })
  fireEvent.click(
    screen.getByRole('button', { name: 'Generate read-only report' })
  )
  await screen.findByText('REPORT-1')
  expect(screen.getByText('Orders in scope')).toBeVisible()
  expect(
    screen.getByText('Orders missing original payment details')
  ).toBeVisible()
  expect(api.reportWalletReconciliation).toHaveBeenCalledWith(
    expect.objectContaining({
      payment_provider: 'epay',
      provider_account_ref: 'account-1',
      provider_environment: 'prod',
      tenant_id: 0,
    }),
    100,
    1
  )
  expect(api.startWalletReconciliation).not.toHaveBeenCalled()
  expect(
    screen.getByRole('button', { name: 'Start authorized batch' })
  ).toBeDisabled()
  fireEvent.change(screen.getByLabelText('Original merchant account'), {
    target: { value: 'account-2' },
  })
  expect(screen.queryByText('REPORT-1')).not.toBeInTheDocument()
})
it('shows pausing until the server confirms a paused run', async () => {
  api.getWalletReconciliation.mockResolvedValue(run)
  api.controlWalletReconciliation.mockResolvedValue({
    ...run,
    control_version: '2',
    status: 'pausing',
    desired_state: 'paused',
  })
  renderRun()
  fireEvent.change(screen.getByLabelText('Batch run ID'), {
    target: { value: 'RUN-1' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Load batch' }))
  await screen.findByText('Running')
  expect(api.getWalletReconciliation).toHaveBeenCalledWith(
    'RUN-1',
    expect.any(AbortSignal)
  )
  expect(api.getWalletReconciliation).toHaveBeenCalledTimes(1)
  fireEvent.change(screen.getByLabelText('Operation reason'), {
    target: { value: 'Pause for merchant verification' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Pause batch' }))
  await screen.findByText('Pausing')
  expect(
    screen.queryByRole('button', { name: 'Resume authorized batch' })
  ).not.toBeInTheDocument()
  expect(api.controlWalletReconciliation).toHaveBeenCalledWith(
    run,
    'pause',
    'Pause for merchant verification',
    ''
  )
  api.getWalletReconciliation.mockResolvedValue({
    ...run,
    control_version: '3',
    status: 'paused',
    desired_state: 'paused',
  })
  fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'Resume authorized batch' })
    ).toBeDisabled()
  )
})
