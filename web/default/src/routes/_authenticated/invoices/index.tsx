import { createFileRoute, redirect } from '@tanstack/react-router'
import { z } from 'zod'

import { InvoiceCenterPage } from '@/features/invoice'
import {
  invoiceSearchFilters,
  invoiceSearchSchema,
} from '@/features/invoice/lib/search'

export const Route = createFileRoute('/_authenticated/invoices/')({
  validateSearch: invoiceSearchSchema.extend({
    tab: z.enum(['records', 'profiles']).optional().catch(undefined),
  }),
  beforeLoad: ({ search }) => {
    if (!search.tab) throw redirect({ to: '/invoices/new', replace: true })
  },
  component: InvoiceCenterRoute,
})
function InvoiceCenterRoute() {
  const search = Route.useSearch()
  return (
    <InvoiceCenterPage
      tab={search.tab}
      filters={invoiceSearchFilters(search)}
      selectedId={search.selected}
    />
  )
}
