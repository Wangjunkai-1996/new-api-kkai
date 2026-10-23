import { createFileRoute } from '@tanstack/react-router'

import { InvoiceAdminPage } from '@/features/invoice/admin'
import {
  invoiceSearchFilters,
  invoiceSearchSchema,
} from '@/features/invoice/lib/search'

export const Route = createFileRoute('/_authenticated/invoice-admin/')({
  validateSearch: invoiceSearchSchema,
  component: InvoiceAdminRoute,
})
function InvoiceAdminRoute() {
  const search = Route.useSearch()
  return (
    <InvoiceAdminPage
      filters={invoiceSearchFilters(search)}
      selectedId={search.selected}
    />
  )
}
