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
import type { RebateRequestStatus, RebateRecord } from './types'

export type PayoutRecordMode = 'order' | 'signup'

const payoutStatus = (record: RebateRecord): string =>
  record.payout?.status || record.payoutStatus || 'unknown'

export const derivePayoutActions = (
  record: RebateRecord,
  mode: PayoutRecordMode
) => {
  const status = payoutStatus(record)
  const serviceCanPay = record.payout?.canPay ?? record.canPay
  const serviceCanReverse = record.payout?.canReverse ?? record.canReverse
  const pay =
    mode === 'order' &&
    (serviceCanPay ??
      ['pending', 'unpaid', 'failed', 'reversed', 'reverted'].includes(status))
  const reverse =
    mode === 'order' &&
    (serviceCanReverse ?? ['paid', 'completed', 'success'].includes(status))
  const revokeSignup =
    mode === 'signup' &&
    ['invite_inviter', 'invite_invitee'].includes(record.orderType) &&
    record.status !== 'completed'

  return { pay, reverse, revokeSignup }
}

export const registrationRewardAction = (
  generated: boolean,
  status?: string | null
): 'generate' | 'revoke' | null => {
  if (!generated) return 'generate'
  if (status === 'completed') return null
  return 'revoke'
}

export const isApproveAndPayEligible = (status: RebateRequestStatus): boolean =>
  status === 'pending' || status === 'approved'
