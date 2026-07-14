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
export type RebatePayoutAction = 'pay' | 'reverse'

export type RebatePayoutCommand = {
  recordId: number
  action: RebatePayoutAction
  idempotencyKey: string
}

export type ApproveAndPayCommand = {
  scope: string
  idempotencyKey: string
}

export type RebatePayoutActionResponse = {
  outcome: string
  payoutId?: number | null
  recordId: number
  requestId: string
  amountCents: number
  quotaDelta: number
  balanceAfter?: number | null
  newapiLogId?: number | null
  newapiMarkId?: number | null
  message?: string | null
  payoutEnabled: boolean
}

export type ApproveAndPayItemResult = {
  recordId: number
  outcome: string
  amountCents?: number | null
  quotaDelta?: number | null
  payoutId?: number | null
  error?: string | null
}

export type ApproveAndPayResponse = {
  requestId: number
  status: string
  totalAmount: number
  paidCount: number
  alreadyPaidCount: number
  pendingCount: number
  failedCount: number
  items: ApproveAndPayItemResult[]
}

export type BatchApproveAndPayResponse = {
  totalRequests: number
  succeededRequests: number
  failedRequests: number
  paidCount: number
  alreadyPaidCount: number
  pendingCount: number
  failedCount: number
  items: ApproveAndPayResponse[]
  errors: { requestId: number; error: string }[]
}
