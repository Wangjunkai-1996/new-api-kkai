import { z } from 'zod'

import type { InvoiceFilters } from '../types'

export const invoiceSearchSchema = z.object({
  q: z.string().max(128).optional().catch(undefined),
  source_type: z.string().max(32).optional().catch(undefined),
  currency: z.string().max(3).optional().catch(undefined),
  from: z.string().max(40).optional().catch(undefined),
  to: z.string().max(40).optional().catch(undefined),
  status: z.string().max(40).optional().catch(undefined),
  group: z.string().max(40).optional().catch(undefined),
  queue: z.string().max(40).optional().catch(undefined),
  p: z.coerce.number().int().min(1).optional().catch(undefined),
  page_size: z.coerce.number().int().min(1).max(50).optional().catch(undefined),
  selected: z.string().max(128).optional().catch(undefined),
})

export function invoiceSearchFilters(
  search: z.infer<typeof invoiceSearchSchema>
): InvoiceFilters {
  const { selected: _selected, ...filters } = search
  return filters
}
