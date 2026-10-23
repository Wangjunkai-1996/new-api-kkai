import { createFileRoute, notFound } from '@tanstack/react-router'

import { InvoiceAdminPage } from '@/features/invoice/admin'
import {
  invoiceSearchFilters,
  invoiceSearchSchema,
} from '@/features/invoice/lib/search'

export const Route = createFileRoute(
  '/_authenticated/invoice-admin/$invoiceId'
)({
  beforeLoad: ({ params }) => {
    if (!/^[A-Za-z0-9_-]{1,128}$/.test(params.invoiceId)) throw notFound()
  },
  validateSearch: invoiceSearchSchema,
  component: InvoiceAdminDetailRoute,
})
function InvoiceAdminDetailRoute() {
  const params = Route.useParams()
  const filters = invoiceSearchFilters(Route.useSearch())
  return (
    <InvoiceAdminPage
      key={params.invoiceId}
      invoiceId={params.invoiceId}
      filters={filters}
    />
  )
}
