import type { UseFormReturn } from 'react-hook-form'
import { z } from 'zod'

import { getServerErrorSources } from '@/lib/server-error-message'

import type { InvoiceProfile } from '../types'

export const invoiceProfileSchema = z
  .object({
    kind: z.enum(['person', 'company']),
    title: z.string().trim().min(1).max(200),
    tax_id: z.string().max(128),
    email: z.email().max(254),
    address: z.string().max(500).optional(),
    phone: z.string().max(64).optional(),
    bank_name: z.string().max(200).optional(),
    bank_account: z.string().max(128).optional(),
  })
  .superRefine((value, context) => {
    if (value.kind === 'company' && !value.tax_id.trim()) {
      context.addIssue({
        code: 'custom',
        path: ['tax_id'],
        message: 'Tax ID is required for a company',
      })
    }
  })
export const emptyInvoiceProfile: InvoiceProfile = {
  kind: 'company',
  title: '',
  tax_id: '',
  email: '',
  address: '',
  phone: '',
  bank_name: '',
  bank_account: '',
}
export function applyInvoiceProfileErrors(
  form: UseFormReturn<InvoiceProfile>,
  error: unknown
) {
  let applied = false
  const fields = new Set(Object.keys(emptyInvoiceProfile))
  for (const source of getServerErrorSources(error)) {
    if (!source.field_errors || typeof source.field_errors !== 'object') {
      continue
    }
    for (const field of Object.keys(source.field_errors)) {
      const name = field.replace(/^profile\./, '')
      if (!fields.has(name)) continue
      form.setError(name as keyof InvoiceProfile, { type: 'server' })
      applied = true
    }
  }
  return applied
}
