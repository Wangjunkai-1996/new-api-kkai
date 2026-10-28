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

export const idSchema = z.string().min(1).max(191)
export const versionSchema = z.string().regex(/^(0|[1-9]\d*)$/)
export const moneySchema = z.object({
  amount_minor: z
    .string()
    .regex(/^(0|[1-9]\d*)$/)
    .refine(
      (value) =>
        /^\d{1,19}$/.test(value) && BigInt(value) <= 9223372036854775807n
    ),
  currency: z.string().regex(/^[A-Z]{3}$/),
  exponent: z.number().int().min(0).max(3),
})
export const dateSchema = z.string().datetime({ offset: true })
const nullableDate = dateSchema.nullable().optional()
export const fieldSchema = z.object({
  key: idSchema,
  label: z.string(),
  required: z.boolean().optional(),
  type: z.string().optional(),
  max_length: z.number().optional(),
})
export const coverSchema = z.object({
  url: z.string(),
  width: z.number().optional(),
  height: z.number().optional(),
  alt: z.string().optional(),
})
export const policySchema = z.object({
  mode: z.string().optional(),
  public_terms: z.string().default(''),
  duration_seconds: z.number().nullable().optional(),
  start_event: z.string().nullable().optional(),
  fixed_end_at: nullableDate,
  first_start: z.string().optional(),
  replacement_start: z.string().optional(),
  expired_action: z.string().optional(),
  goods_refund_mode: z.string().optional(),
  execution: z.string().optional(),
  sla: z
    .object({
      clock: z.enum(['elapsed', 'business', '']),
      duration_minutes: z.number().int().nonnegative(),
      timezone: z.string(),
      pause_on_missing_input: z.boolean(),
      weekly_windows: z
        .array(
          z.object({
            weekday: z.number().int().min(1).max(7),
            start: z.string(),
            end: z.string(),
          })
        )
        .max(14)
        .nullable()
        .transform((value) => value ?? []),
    })
    .nullable()
    .optional(),
})
export const productSchema = z.object({
  id: idSchema,
  version: versionSchema,
  name: z.string(),
  summary: z.string().default(''),
  category_id: idSchema.nullable().optional(),
  cover: coverSchema.nullable().optional(),
  price: moneySchema.extend({
    currency: z.union([moneySchema.shape.currency, z.literal('')]),
  }),
  delivery_type: z.enum(['automatic', 'manual', '']),
  availability: z.enum([
    'available',
    'sold_out',
    'capacity_full',
    'unavailable',
  ]),
  product_type: z.string().optional(),
  specification: z.string().default(''),
  usage_conditions: z.string().default(''),
  delivery_fields: z.array(fieldSchema).default([]),
  buyer_input_schema: z.array(fieldSchema).default([]),
  delivery_policy: policySchema.nullable().optional(),
  view_policy: policySchema.nullable().optional(),
  warranty_policy: policySchema.nullable().optional(),
  refund_policy: policySchema.nullable().optional(),
  entitlement_policy: policySchema.nullable().optional(),
  terms_version: versionSchema.optional(),
  can_purchase: z.boolean().optional(),
  unavailable_reason_code: z.string().nullable().optional(),
  state: z.string().optional(),
  allowed_actions: z.array(z.string()).default([]),
  template_version: versionSchema.optional(),
  manual_capacity: z.number().int().nullable().optional(),
  inventory_identity_domain: z.string().optional(),
  identity_normalization_version: versionSchema.optional(),
  low_stock_threshold: z.number().int().optional(),
})
export const categorySchema = z.object({
  id: idSchema,
  name: z.string(),
  sort_order: z.number().default(0),
  version: versionSchema.optional(),
})
export const configSchema = z.object({
  new_orders_enabled: z.boolean(),
  currency: z.string().nullable(),
  server_time: dateSchema,
  payment_methods: z.array(z.object({ id: z.string(), name: z.string() })),
})
export const eventSchema = z.object({
  event_id: idSchema,
  event_type: z.string(),
  occurred_at: dateSchema,
  public_summary: z.string(),
})
export const deliverySchema = z.object({
  id: idSchema,
  version: versionSchema,
  delivered_at: nullableDate,
  view_expires_at: nullableDate,
  masked_fields: z
    .array(
      z.object({
        key: z.string(),
        label: z.string(),
        masked_value: z.string().optional(),
      })
    )
    .default([]),
  instructions: z.string().default(''),
  can_reveal: z.boolean().default(false),
  unavailable_reason_code: z.string().nullable().optional(),
})
export const fundSummarySchema = z.object({
  id: idSchema.optional(),
  state: z.string().optional(),
  money: moneySchema.optional(),
  public_summary: z.string().optional(),
})
export const orderSchema = z.object({
  order_no: idSchema,
  version: versionSchema,
  product_snapshot: productSchema
    .partial()
    .extend({ name: z.string(), specification: z.string().default('') }),
  quantity: z.literal(1),
  total: moneySchema,
  created_at: dateSchema,
  purchase_state: z.enum(['open', 'closed']),
  payment_state: z.enum(['unpaid', 'verifying', 'paid']),
  delivery_state: z.enum([
    'not_ready',
    'pending',
    'processing',
    'awaiting_input',
    'delivered',
    'exception',
  ]),
  aftersale_state: z.enum([
    'none',
    'pending',
    'processing',
    'replaced',
    'resolved',
    'rejected',
    'closed',
  ]),
  refund_state: z.enum([
    'none',
    'approved',
    'processing',
    'unknown',
    'failed',
    'succeeded',
    'cancelled',
  ]),
  display_status: z.enum([
    'awaiting_payment',
    'payment_verifying',
    'closed',
    'pending_delivery',
    'processing',
    'awaiting_input',
    'delivery_exception',
    'delivered',
    'aftersale',
    'refund_pending',
    'refund_failed',
    'refunded',
  ]),
  notice_codes: z.array(z.string()).default([]),
  allowed_actions: z.array(z.string()).default([]),
  reservation_expires_at: nullableDate,
  paid_at: nullableDate,
  delivery_due_at: nullableDate,
  delivered_at: nullableDate,
  server_time: dateSchema.optional(),
  current_delivery: deliverySchema.nullable().optional(),
  delivery_history: z.array(deliverySchema).default([]),
  buyer_input_status: z
    .object({
      missing_field_keys: z.array(z.string()).default([]),
      complete: z.boolean().optional(),
    })
    .nullable()
    .optional(),
  risk_hold: z
    .object({ public_reason: z.string(), started_at: dateSchema })
    .nullable()
    .optional(),
  payment_summary: z
    .object({
      expected: moneySchema.optional(),
      received: moneySchema.optional(),
      public_summary: z.string().optional(),
      last_checked_at: nullableDate,
    })
    .nullable()
    .optional(),
  refund_summary: fundSummarySchema.nullable().optional(),
  extra_payment_summary: z.array(fundSummarySchema).default([]),
  timeline: z.array(eventSchema).default([]),
  user_id: idSchema.optional(),
  assignee_id: idSchema.nullable().optional(),
})
export const checkoutSchema = z.object({
  order: orderSchema,
  attempt: z.object({
    id: idSchema,
    state: z.enum(['creating', 'ready', 'unknown', 'closed', 'failed', 'paid']),
    expires_at: nullableDate,
    last_checked_at: nullableDate,
  }),
  checkout: z
    .object({
      kind: z.enum(['redirect', 'qr']),
      url: z.string().optional(),
      qr_payload: z.string().optional(),
      expires_at: nullableDate,
    })
    .nullable(),
  poll_after_seconds: z.number().optional(),
})
export const revealSchema = z.object({
  order_no: idSchema.optional(),
  delivery_id: idSchema.optional(),
  delivery_version: versionSchema.optional(),
  view_expires_at: dateSchema,
  server_time: dateSchema,
  fields: z.array(
    z.object({ key: z.string(), label: z.string(), value: z.string() })
  ),
})
export const buyerInputSchema = z.object({
  fields: z.array(fieldSchema),
  values: z.record(z.string(), z.string()),
})
export const aftersaleSchema = z.object({
  id: idSchema,
  version: versionSchema,
  order_no: idSchema,
  state: z.string(),
  issue_type: z.string(),
  desired_resolution: z.string(),
  description: z.string(),
  created_at: dateSchema,
  allowed_actions: z.array(z.string()).default([]),
  messages: z
    .array(
      z.object({
        id: idSchema,
        body: z.string(),
        visibility: z.enum(['public', 'internal']).optional(),
        created_at: dateSchema,
      })
    )
    .default([]),
  order_version: versionSchema.optional(),
})
export const recoverySchema = z.object({
  status: z.enum(['recorded', 'not_observed']),
  order_no: idSchema.nullable().optional(),
  batch_id: idSchema.nullable().optional(),
  inserted_count: z.number().int().nullable().optional(),
})
export const inventorySchema = z.object({
  id: idSchema,
  version: versionSchema,
  product_id: idSchema,
  state: z.string(),
  masked_identifier: z.string().default(''),
  batch_id: idSchema.optional(),
  order_no: idSchema.nullable().optional(),
  created_at: dateSchema.optional(),
  allowed_actions: z.array(z.string()).default([]),
})
export const importPreviewSchema = z.object({
  preview_digest: z.string(),
  valid_count: z.number(),
  duplicate_count: z.number(),
  invalid_count: z.number(),
  errors: z.array(z.object({ row: z.number(), code: z.string() })).default([]),
})
export const refundSchema = z.object({
  id: idSchema,
  version: versionSchema,
  receipt_id: idSchema,
  order_no: idSchema.nullable().optional(),
  kind: z.string(),
  state: z.string(),
  money: moneySchema,
  allowed_actions: z.array(z.string()).default([]),
  attempts: z
    .array(
      z.object({
        id: idSchema,
        state: z.string(),
        channel_ref: z.string().nullable().optional(),
        operated_at: nullableDate,
      })
    )
    .default([]),
  evidence_ids: z.array(idSchema).default([]),
  public_note: z.string().default(''),
})
export const receiptSchema = z.object({
  refundable_money_by_kind: z
    .object({
      goods: moneySchema.optional(),
      extra: moneySchema.optional(),
      mismatch: moneySchema.optional(),
    })
    .default({}),
  id: idSchema,
  version: versionSchema,
  order_no: idSchema.nullable().optional(),
  provider: z.string(),
  provider_transaction_id: z.string(),
  money: moneySchema,
  state: z.string(),
  allowed_actions: z.array(z.string()).default([]),
  public_summary: z.string().default(''),
  evidence_ids: z.array(idSchema).default([]),
  refunds: z.array(refundSchema).default([]),
  adjustments: z
    .array(
      z.object({
        id: idSchema,
        kind: z.string(),
        state: z.string(),
        money: moneySchema,
      })
    )
    .default([]),
})
export const overviewSchema = z.object({
  currency: z.string(),
  from: dateSchema.optional(),
  to: dateSchema.optional(),
  orders: z.number().default(0),
  pending_delivery: z.number().default(0),
  pending_aftersales: z.number().default(0),
  unresolved_receipts: z.number().default(0),
  unresolved_refunds: z.number().default(0),
  received: moneySchema.nullable().optional(),
  refunded: moneySchema.nullable().optional(),
})
export const retentionSchema = z.object({
  secret_after_delivery_seconds: z.number(),
  buyer_input_after_closure_seconds: z.number(),
  evidence_after_settlement_seconds: z.number(),
  audit_after_settlement_seconds: z.number(),
  idempotency_after_closure_seconds: z.number(),
})
export const reminderSchema = z.object({
  payment_unknown_seconds: z.number(),
  delivery_exception_seconds: z.number(),
  refund_verification_seconds: z.number(),
})
export const settingsSchema = z.object({
  provider_account_ref: z.string().optional(),
  environment: z.string().optional(),
  version: versionSchema,
  new_orders_enabled: z.boolean(),
  provider: z.string().optional(),
  currency: z.string().nullable(),
  currency_exponent: z.number().int().optional(),
  payment_methods: z.array(z.string()).default([]),
  reservation_minutes: z.number().int().optional(),
  pending_order_limit: z.number().int().optional(),
  min_amount_minor: z.string().optional(),
  max_amount_minor: z.string().optional(),
  retention_policy: retentionSchema.nullable().optional(),
  reminder_policy: reminderSchema.nullable().optional(),
})
export function pageSchema<T extends z.ZodType>(item: T) {
  return z.object({
    items: z.array(item),
    page: z.number().int(),
    page_size: z.number().int(),
    total: z.number().int().nonnegative(),
  })
}
export const listSearchSchema = z.object({
  q: z.string().max(100).optional().catch(undefined),
  category_id: z.string().optional(),
  status_group: z
    .enum(['all', 'unpaid', 'delivery', 'delivered', 'aftersale'])
    .optional(),
  page: z.coerce.number().int().min(1).catch(1),
})
export type Money = z.infer<typeof moneySchema>
export type Product = z.infer<typeof productSchema>
export type Order = z.infer<typeof orderSchema>
export type Aftersale = z.infer<typeof aftersaleSchema>
export type Receipt = z.infer<typeof receiptSchema>
export type Refund = z.infer<typeof refundSchema>
export type Reveal = z.infer<typeof revealSchema>
