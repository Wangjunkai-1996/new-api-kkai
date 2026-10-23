import { api } from '@/lib/api'
import { getCurrentHost } from '@/lib/host-scope'
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  InvoiceAdminSummary,
  InvoiceAttachment,
  InvoiceCreateInput,
  InvoiceEvent,
  InvoiceFilters,
  InvoiceOrder,
  InvoiceOrderRef,
  InvoicePage,
  InvoicePolicy,
  InvoiceProfile,
  InvoiceQuote,
  InvoiceRequest,
  InvoiceSummary,
} from './types'

export function invoiceKey(userId: number | undefined, admin = false) {
  return [
    'invoices',
    getCurrentHost(),
    userId,
    admin ? 'admin' : 'user',
  ] as const
}
export function invoiceBase(admin = false) {
  return admin ? '/api/admin/invoices' : '/api/invoices'
}
async function get<T>(url: string, params?: object): Promise<T> {
  const response = await api.get(url, { params, skipErrorHandler: true })
  return requireServerSuccess(response.data).data as T
}
async function write<T>(
  method: 'post' | 'patch' | 'delete',
  url: string,
  input: unknown,
  key: string
): Promise<T> {
  const response = await api.request({
    method,
    url,
    data: input,
    headers: { 'Idempotency-Key': key },
    skipErrorHandler: true,
  })
  return requireServerSuccess(response.data).data as T
}
export const getInvoicePolicy = () => get<InvoicePolicy>('/api/invoices/policy')
export const getInvoiceSummary = () =>
  get<InvoiceSummary>('/api/invoices/summary')
export const getInvoiceOrders = (filters: InvoiceFilters) =>
  get<InvoicePage<InvoiceOrder>>('/api/invoices/eligible-orders', filters)
export const verifyInvoicePayment = (order: InvoiceOrderRef, key: string) =>
  write<{ verified: boolean }>(
    'post',
    '/api/invoices/payments/verify',
    order,
    key
  )
export const getInvoices = (filters: InvoiceFilters, admin = false) =>
  get<InvoicePage<InvoiceRequest>>(invoiceBase(admin), filters)
export const getInvoice = (id: string, admin = false) =>
  get<InvoiceRequest>(`${invoiceBase(admin)}/${encodeURIComponent(id)}`)
export const getInvoiceAdminSummary = (filters: InvoiceFilters) =>
  get<InvoiceAdminSummary>('/api/admin/invoices/summary', filters)
export const quoteInvoice = (
  items: InvoiceOrderRef[],
  invoice_type?: string,
  policy_version?: string
) =>
  write<InvoiceQuote>(
    'post',
    '/api/invoices/quote',
    { items, invoice_type, policy_version },
    crypto.randomUUID()
  )
export const createInvoice = (input: InvoiceCreateInput, key: string) =>
  write<InvoiceRequest>('post', '/api/invoices', input, key)
export const invoiceAction = (
  id: string,
  action: string,
  input: object,
  key: string,
  admin = false
) =>
  write<InvoiceRequest>(
    'post',
    `${invoiceBase(admin)}/${encodeURIComponent(id)}/${action}`,
    input,
    key
  )
export const getInvoiceProfiles = () =>
  get<InvoicePage<InvoiceProfile>>('/api/invoice-profiles', {
    p: 1,
    page_size: 100,
  })
export const getInvoiceProfile = (id: string) =>
  get<InvoiceProfile>(`/api/invoice-profiles/${encodeURIComponent(id)}`)
export const saveInvoiceProfile = (profile: InvoiceProfile, key: string) => {
  const id = profile.public_id || profile.id
  return write<InvoiceProfile>(
    id ? 'patch' : 'post',
    `/api/invoice-profiles${id ? `/${encodeURIComponent(id)}` : ''}`,
    profile,
    key
  )
}
export const deleteInvoiceProfile = (profile: InvoiceProfile, key: string) =>
  write<void>(
    'delete',
    `/api/invoice-profiles/${encodeURIComponent(profile.public_id || profile.id || '')}`,
    { version: profile.version },
    key
  )
export async function uploadInvoiceAttachment(
  id: string,
  file: File,
  admin: boolean,
  key: string,
  onProgress?: (percent: number) => void
): Promise<InvoiceAttachment> {
  const form = new FormData()
  form.append('file', file)
  form.append('purpose', admin ? 'invoice' : 'dispute_evidence')
  const response = await api.post(
    `${invoiceBase(admin)}/${encodeURIComponent(id)}/attachments`,
    form,
    {
      headers: { 'Idempotency-Key': key },
      skipErrorHandler: true,
      onUploadProgress: (event) => {
        if (event.total) {
          onProgress?.(Math.round((event.loaded / event.total) * 100))
        }
      },
    }
  )
  return requireServerSuccess(response.data).data as InvoiceAttachment
}
export async function fetchInvoiceFile(
  id: string,
  fileId: string,
  admin: boolean,
  download: boolean
): Promise<Blob> {
  const response = await api.get(
    `${invoiceBase(admin)}/${encodeURIComponent(id)}/files/${encodeURIComponent(fileId)}`,
    {
      params: { disposition: download ? 'attachment' : 'inline' },
      responseType: 'blob',
      skipErrorHandler: true,
    }
  )
  return response.data as Blob
}
export const getInvoiceNotifications = (admin: boolean, p: number) =>
  get<InvoicePage<InvoiceEvent> & { unread_count: number }>(
    `${invoiceBase(admin)}/notifications`,
    { p, page_size: 20 }
  )
export async function readInvoiceNotification(admin: boolean, id?: string) {
  const response = await api.post(
    `${invoiceBase(admin)}/notifications/${id ? `${encodeURIComponent(id)}/read` : 'read-all'}`
  )
  requireServerSuccess(response.data)
}
