import { createContext, useContext } from 'react'

import type {
  InvoiceCreateInput,
  InvoiceOrder,
  InvoicePolicy,
  InvoiceProfile,
} from '../types'
import { emptyInvoiceProfile } from './profile'

export interface InvoiceDraft {
  step: number
  orders: InvoiceOrder[]
  profile: InvoiceProfile
  invoiceType: string
  note: string
  consent: boolean
  key: string
  pendingCreate?: { input: InvoiceCreateInput; key: string }
}
export const DraftContext = createContext<{
  draft: InvoiceDraft
  setDraft: React.Dispatch<React.SetStateAction<InvoiceDraft>>
  policy?: InvoicePolicy
} | null>(null)
export function useInvoiceDraft() {
  const value = useContext(DraftContext)
  if (!value) throw new Error('Invoice workspace is required')
  return value
}
export function freshInvoiceDraft(email = ''): InvoiceDraft {
  return {
    step: 1,
    orders: [],
    profile: { ...emptyInvoiceProfile, email },
    invoiceType: '',
    note: '',
    consent: false,
    key: crypto.randomUUID(),
  }
}
