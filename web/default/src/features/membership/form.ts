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
import type { TFunction } from 'i18next'
import { z } from 'zod'

import { findMembershipDiscountConflicts } from './lib'

export function membershipCatalogSchema(t: TFunction) {
  return z
    .object({
      release_id: z.number().int().nonnegative(),
      runtime_enabled: z.boolean().optional(),
      sales_enabled: z.boolean(),
      topup_discounts_disabled: z.boolean().optional(),
      model_limits: z
        .record(
          z.string().trim().min(1),
          z
            .object({
              max_input_tokens: z.number().int().min(0).max(2147483647),
              max_output_tokens: z.number().int().min(0).max(2147483647),
              max_tool_calls: z.number().int().min(0).max(2147483647),
              task_usage_limits: z
                .record(
                  z.string().trim().min(1).max(256),
                  z.number().finite().min(0).max(2147483647)
                )
                .optional(),
              evidence: z.string().trim().min(1).max(2048),
            })
            .refine(
              (limit) =>
                limit.max_input_tokens > 0 ||
                limit.max_output_tokens > 0 ||
                limit.max_tool_calls > 0 ||
                Object.keys(limit.task_usage_limits ?? {}).length > 0,
              t('At least one authorization limit must be positive.')
            )
        )
        .nullish(),
      tiers: z.array(
        z.object({
          id: z
            .string()
            .trim()
            .min(1, t('Tier identifier is required'))
            .regex(
              /^[a-zA-Z0-9_-]+$/,
              t('Use letters, numbers, underscores or hyphens')
            ),
          name: z.string().trim().min(1, t('Please enter plan title')),
          rank: z.number().int().positive(t('Rank must be a positive integer')),
          style: z.enum(['gold', 'diamond', 'king']),
          price_minor: z
            .number()
            .int()
            .nonnegative()
            .max(Number.MAX_SAFE_INTEGER),
          currency: z.enum(['USD', 'CNY']),
          // A tier can contain more than one request's int32 reservation cap.
          // Keep the catalog value within JavaScript's exact integer range; each
          // individual reservation is still bounded by the backend billing guard.
          quota: z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER),
          enabled: z.boolean(),
          discounts: z.record(
            z.string(),
            z.number().finite().positive().max(1)
          ),
          payment_products: z.record(z.string(), z.string()).optional(),
        })
      ),
      groups: z.array(
        z.object({
          key: z.string(),
          display_name: z.string(),
          enabled: z.boolean(),
          published: z.boolean(),
        })
      ),
    })
    .superRefine((catalog, ctx) => {
      const ids = new Set<string>()
      const ranks = new Set<number>()
      for (const [index, tier] of catalog.tiers.entries()) {
        if (ids.has(tier.id)) {
          ctx.addIssue({
            code: 'custom',
            path: ['tiers', index, 'id'],
            message: t('Tier identifiers must be unique'),
          })
        }
        if (ranks.has(tier.rank)) {
          ctx.addIssue({
            code: 'custom',
            path: ['tiers', index, 'rank'],
            message: t('Tier ranks must be unique'),
          })
        }
        ids.add(tier.id)
        ranks.add(tier.rank)
        if (catalog.sales_enabled && tier.enabled) {
          if (tier.price_minor <= 0) {
            ctx.addIssue({
              code: 'custom',
              path: ['tiers', index, 'price_minor'],
              message: t('Set a positive price before opening sales'),
            })
          }
          if (tier.quota <= 0) {
            ctx.addIssue({
              code: 'custom',
              path: ['tiers', index, 'quota'],
              message: t('Set a positive quota before opening sales'),
            })
          }
        }
      }
      for (const conflict of findMembershipDiscountConflicts(catalog)) {
        const index = catalog.tiers.findIndex(
          (tier) => tier.id === conflict.tier
        )
        ctx.addIssue({
          code: 'custom',
          path: ['tiers', index, 'discounts', conflict.group],
          message: t('A higher tier cannot have a higher coefficient'),
        })
      }
    })
}
