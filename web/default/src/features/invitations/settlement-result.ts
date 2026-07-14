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
export type SettlementNotice = {
  level: 'success' | 'warning' | 'error'
  messageKey: string
  values?: Record<string, number>
}

export type ApproveAndPayCounts = {
  paidCount: number
  alreadyPaidCount: number
  pendingCount: number
  failedCount: number
}

const SETTLEMENT_MESSAGE =
  'Settlement: {{paid}} paid, {{alreadyPaid}} already paid, {{pending}} pending, {{failed}} failed'

export const summarizeApproveAndPay = (
  result: ApproveAndPayCounts
): SettlementNotice => {
  const values = {
    paid: result.paidCount,
    alreadyPaid: result.alreadyPaidCount,
    pending: result.pendingCount,
    failed: result.failedCount,
  }
  let level: SettlementNotice['level'] = 'success'
  if (result.failedCount > 0) level = 'error'
  else if (result.pendingCount > 0) level = 'warning'

  return { level, messageKey: SETTLEMENT_MESSAGE, values }
}

export const summarizePayoutAction = (result: {
  outcome?: string
}): SettlementNotice => {
  if (result.outcome === 'pending') {
    return { level: 'warning', messageKey: 'Payout is pending' }
  }
  if (result.outcome === 'failed') {
    return { level: 'error', messageKey: 'Payout failed' }
  }
  if (result.outcome === 'dry_run') {
    return { level: 'warning', messageKey: 'Payout is disabled' }
  }
  if (
    ['paid', 'reversed', 'already_paid', 'already_reversed'].includes(
      result.outcome ?? ''
    )
  ) {
    return { level: 'success', messageKey: 'Payout ledger updated' }
  }
  return { level: 'warning', messageKey: 'Payout result requires review' }
}
