export type InvoiceStatus =
  | 'pending_review'
  | 'approved_for_issue'
  | 'delivered'
  | 'confirmed'
  | 'disputed'
  | 'awaiting_reconfirmation'
  | 'finance_hold'
  | 'rejected'

export interface InvoiceProfile {
  id?: string
  public_id?: string
  version?: number
  kind: 'person' | 'company'
  title: string
  tax_id: string
  email: string
  address?: string
  phone?: string
  bank_name?: string
  bank_account?: string
}
export interface InvoiceType {
  id: string
  label: string
  required_fields: string[]
}
export interface InvoicePolicy {
  enabled: boolean
  disabled_reason?: string
  policy_version: string
  minimum_amount_minor?: string
  seller_id: string
  seller_name: string
  currencies: { currency: string; exponent: number }[]
  invoice_types: InvoiceType[]
  timezone: string
  limits: { max_orders: number; max_file_bytes: number }
  sla?: string | null
}
export interface InvoiceOrderRef {
  source_type: string
  order_id: string
}
export interface InvoiceOrder extends InvoiceOrderRef {
  order_no: string
  paid_at: string
  payment_label: string
  amount_minor: string | null
  currency: string
  currency_exponent: number
  eligible: boolean
  reason_codes: string[]
  merge_group_id: string
  claimed_request_public_id?: string
  allowed_invoice_types: string[]
  allowed_actions?: string[]
}
export interface InvoicePage<T> {
  items: T[]
  total: number
  p: number
  page_size: number
}
export interface InvoiceSummary {
  eligible_count: number
  totals: {
    currency: string
    currency_exponent: number
    amount_minor: string
  }[]
  processing_count: number
  awaiting_confirmation_count: number
  unread_count: number
}
export interface InvoiceAttachment {
  id: string
  file_name: string
  mime_type: string
  size: number
  expires_at?: string
}
export interface InvoiceDelivery {
  delivery_version: number
  file: InvoiceAttachment
  invoice_no: string
  issued_on: string
  amount_minor: string
  currency: string
  created_at: string
}
export interface InvoiceMessage {
  id: string
  description: string
  actor_id?: string
  created_at: string
  evidence?: InvoiceAttachment
}
export interface InvoiceDispute {
  id: string
  delivery_version: number
  round: number
  category: string
  description: string
  reply?: string
  status: string
  created_at: string
  evidence?: InvoiceAttachment
  messages?: InvoiceMessage[]
}
export interface InvoiceEvent {
  id: string
  event_id?: string
  request_public_id?: string
  display_no?: string
  kind: string
  attachment?: InvoiceAttachment
  actor_admin?: boolean
  status: InvoiceStatus
  description?: string
  created_at: string
  read_at?: string | null
}
export interface InvoiceRequest {
  public_id: string
  display_no: string
  status: InvoiceStatus
  version: number
  profile: InvoiceProfile
  seller_id: string
  seller_name: string
  invoice_type: string
  amount_minor: string
  currency: string
  currency_exponent: number
  items_count: number
  source_summary: string[]
  items?: InvoiceOrder[]
  created_at: string
  updated_at: string
  current_delivery_version: number
  current_delivery?: InvoiceDelivery | null
  delivered_versions?: InvoiceDelivery[]
  active_dispute?: InvoiceDispute | null
  disputes?: InvoiceDispute[]
  events?: InvoiceEvent[]
  allowed_actions: string[]
  reason?: string
  hold_reason?: string
  hold_from_status?: InvoiceStatus
  username?: string
  user_id?: number
  note?: string
  notification_delivery_state?: string
}
export interface InvoiceQuote {
  items: InvoiceOrder[]
  amount_minor: string
  currency: string
  currency_exponent?: number
  merge_group_id: string
  policy_version: string
  eligible: boolean
  reason_codes?: string[]
}
export interface InvoiceAdminSummary {
  pending_review: number
  approved_for_issue: number
  disputed: number
  finance_hold: number
  all: number
  unread_count: number
  counts?: Record<string, number>
}
export interface InvoiceFilters {
  p?: number
  page_size?: number
  q?: string
  source_type?: string
  currency?: string
  from?: string
  to?: string
  status?: string
  group?: string
  queue?: string
  eligibility?: 'eligible' | 'unavailable'
  order_id?: string
}
export interface InvoiceCreateInput {
  items: InvoiceOrderRef[]
  profile?: InvoiceProfile
  profile_id?: string
  invoice_type: string
  policy_version: string
  note: string
  user_confirmed: boolean
}
