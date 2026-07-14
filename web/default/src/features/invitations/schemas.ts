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

export const rebateRuleSchema = z.object({
  user_group: z.string().min(1),
  rule_type: z.enum(['subscription', 'topup']),
  rebate_rate: z
    .string()
    .min(1)
    .refine((value) => {
      const percentage = Number(value)
      return Number.isFinite(percentage) && percentage >= 0 && percentage <= 100
    }),
})

export type RebateRuleFormValues = z.infer<typeof rebateRuleSchema>

export const invitationSystemConfigSchema = z.object({
  minRebateRequestAmount: z.number().int().min(0),
  rebateRequestFrequencyDays: z.number().int().min(0),
  userInvitationRebateEnabled: z.boolean(),
  orderRebateEnabled: z.boolean(),
  invitationSignupRewardEnabled: z.boolean(),
  invitationSignupRewardAmount: z.number().int().min(0),
  invitationSignupInviterRewardAmount: z.number().int().min(0),
  invitationSignupInviteeRewardAmount: z.number().int().min(0),
  invitationSignupRewardReviewRequired: z.boolean(),
  invitationSignupInviterRewardRequiresPaidOrder: z.boolean(),
  invitationSignupInviteeRewardRequiresPaidOrder: z.boolean(),
  rebateToBalanceEnabled: z.boolean(),
})
