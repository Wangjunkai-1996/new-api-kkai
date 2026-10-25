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
import { z } from 'zod'

import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

const fundingSummarySchema = z.object({
  cash_payments: z.array(
    z.object({
      amount_minor: z.string().regex(/^\d+$/),
      currency: z.string().regex(/^[A-Z]{3}$/),
      exponent: z.number().int().min(0).max(6),
    })
  ),
  confirmed_payment_count: z.number().int().nonnegative(),
  unknown_payment_count: z.number().int().nonnegative(),
  rebate_credited_quota: z.string().regex(/^-?\d+$/),
  rebate_credit_display: z.object({
    amount: z.string().regex(/^-?\d+(?:\.\d+)?$/),
    unit: z.enum(['USD', 'TOKENS']),
  }),
  quota_epoch: z.enum(['usd_credit_v1', 'legacy_075']),
  refund_adjusted: z.literal(false),
  server_time: z.number(),
})

export type WalletFundingSummary = z.infer<typeof fundingSummarySchema>

export async function getWalletFundingSummary(signal?: AbortSignal) {
  const response = await api.get('/api/user/topup/summary', {
    signal,
    timeout: 20_000,
    disableDuplicate: true,
  })
  return fundingSummarySchema.parse(requireServerSuccess(response.data).data)
}
