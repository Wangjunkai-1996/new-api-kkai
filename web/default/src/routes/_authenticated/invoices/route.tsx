import { createFileRoute, redirect } from '@tanstack/react-router'

import { InvoiceWorkspace } from '@/features/invoice/workspace'
import { getSiteConfig } from '@/features/site-config/api'

export const Route = createFileRoute('/_authenticated/invoices')({
  beforeLoad: async () => {
    const site = await getSiteConfig()
    if (site.tenant_id !== 0) throw redirect({ to: '/403' })
  },
  component: InvoiceWorkspace,
})
