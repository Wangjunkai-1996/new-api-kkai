import { createFileRoute, Outlet, redirect } from '@tanstack/react-router'

import { getSiteConfig } from '@/features/site-config/api'

export const Route = createFileRoute('/_authenticated/requests')({
  beforeLoad: async () => {
    const site = await getSiteConfig()
    if (site.tenant_id !== 0) throw redirect({ to: '/403' })
  },
  component: Outlet,
})
