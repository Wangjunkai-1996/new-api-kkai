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
import type { RebateRequestStatus, RebateStatus, RebateRequest } from './types'

export const rebateStatusLabel = (status: RebateStatus): string => {
  const labels: Record<RebateStatus, string> = {
    initializing: 'Confirming',
    pending: 'Pending',
    requested: 'Requested',
    approved: 'Approved',
    completed: 'Completed',
    rejected: 'Rejected',
  }
  return labels[status]
}

export const rebateRequestStatusLabel = (
  status: RebateRequestStatus,
  request?: Pick<RebateRequest, 'requestKind' | 'issuedAt'>
): string => {
  if (request?.requestKind !== 'sales') return rebateStatusLabel(status)
  if (request.issuedAt) return 'Issue Confirmed'
  if (status === 'approved') return 'Approved, Awaiting Issue'
  if (status === 'pending') return 'Awaiting Review'
  return rebateStatusLabel(status)
}

export const salesRebateStatusLabel = (status: string | null): string => {
  const labels: Record<string, string> = {
    confirming: 'Confirming',
    cooling: 'Cooling Down',
    initializing: 'Cooling Down',
    estimated: 'Cooling Down',
    available: 'Available to Request',
    claimable: 'Available to Request',
    pendingApproval: 'Awaiting Review',
    requested: 'Awaiting Review',
    approved: 'Approved, Awaiting Issue',
    issued: 'Issue Confirmed',
    completed: 'Issue Confirmed',
    paid: 'Issue Confirmed',
    locked: 'Locked Rebate',
    waiting_unlock: 'Locked Rebate',
    pending_approval: 'Awaiting Review',
    pending_calculation: 'Awaiting Rebate Calculation',
    not_generated: 'Awaiting Rebate Calculation',
    closed: 'Closed Rebate',
    rejected: 'Rejected',
  }
  if (status === null) return 'Awaiting Rebate Calculation'
  return labels[status] ?? 'Unknown Rebate Status'
}

export const rebateStatusVariant = (
  status: RebateStatus | RebateRequestStatus
): 'default' | 'secondary' | 'destructive' | 'outline' => {
  if (status === 'completed') return 'default'
  if (status === 'rejected') return 'destructive'
  if (
    status === 'initializing' ||
    status === 'pending' ||
    status === 'requested'
  ) {
    return 'secondary'
  }
  return 'outline'
}
