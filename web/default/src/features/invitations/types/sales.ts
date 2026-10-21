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
import type { PaginatedResponse } from './common'

export type SalesAccount = {
  userId: number
  userName: string
  active: boolean
  createdAt: string
  updatedAt: string
  note?: string | null
  performance?: SalesPerformance
}

export type SalesRebates = {
  cooling: number
  available: number
  pendingApproval: number
  approved: number
  issued: number
  locked: number
  closed: number
  pendingCalculationCount: number
}

export type SalesCustomer = {
  userId: number
  userName: string
  invitedAt: string | null
  firstRechargeAt: string | null
  effective: boolean
  rechargeAmount: number
  unverifiedRechargeCount: number
  rebates: SalesRebates
}

export type SalesCreditCurrency = 'legacy_075' | 'usd_credit_v1'

export type SalesPerformance = {
  invitedCount: number
  effectiveInvitedCount: number
  rechargeAmount: number
  unverifiedRechargeCount: number
  rebates: SalesRebates
}

export type SalesAccountsResponse = PaginatedResponse<SalesAccount> & {
  // Optional while frontend and Invitations service roll out independently.
  creditCurrency?: SalesCreditCurrency
  summary?: SalesPerformance
}

export type SalesCustomers = PaginatedResponse<SalesCustomer> & {
  creditCurrency: SalesCreditCurrency
  summary: SalesPerformance
}

export type SalesRecharge = {
  id: number
  completedAt: string
  amount: number | null
  originalAmount: string
  paymentProvider: string
  creditEpoch: 'legacy_075' | 'usd_credit_v1'
  rebateAmount: number | null
  rebateStatus: string | null
  effectiveAt: string | null
}
