import { InvoiceProfiles } from './components/profiles'
import { useInvoiceDraft } from './lib/draft'
import { InvoiceRecords } from './records'
import type { InvoiceFilters } from './types'

export function InvoiceCenterPage(props: {
  tab?: string
  filters?: InvoiceFilters
  selectedId?: string
}) {
  const { policy } = useInvoiceDraft()
  if (props.tab === 'profiles') return <InvoiceProfiles policy={policy} />
  return (
    <InvoiceRecords filters={props.filters} selectedId={props.selectedId} />
  )
}
