import { createFileRoute, notFound } from '@tanstack/react-router'

import {
  invoiceSearchFilters,
  invoiceSearchSchema,
} from '@/features/invoice/lib/search'
import { InvoiceDetailPage } from '@/features/invoice/records'

export const Route = createFileRoute('/_authenticated/invoices/$invoiceId')({
  beforeLoad: ({ params }) => {
    if (!/^[A-Za-z0-9_-]{1,128}$/.test(params.invoiceId)) throw notFound()
  },
  validateSearch: invoiceSearchSchema,
  component: InvoiceDetailRoute,
})
function InvoiceDetailRoute() {
  const params = Route.useParams()
  const filters = invoiceSearchFilters(Route.useSearch())
  return (
    <InvoiceDetailPage
      key={params.invoiceId}
      invoiceId={params.invoiceId}
      filters={filters}
    />
  )
}
