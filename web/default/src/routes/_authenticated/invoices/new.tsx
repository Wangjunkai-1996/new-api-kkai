import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'

import { NewInvoicePage } from '@/features/invoice/new'

export const Route = createFileRoute('/_authenticated/invoices/new')({
  validateSearch: z.object({
    source_type: z.string().max(32).optional().catch(undefined),
    order_id: z.string().max(255).optional().catch(undefined),
    order_no: z.string().max(255).optional().catch(undefined),
  }),
  component: NewInvoiceRoute,
})
function NewInvoiceRoute() {
  const search = Route.useSearch()
  return (
    <NewInvoicePage
      sourceType={search.source_type}
      orderId={search.order_id}
      orderNo={search.order_no}
    />
  )
}
