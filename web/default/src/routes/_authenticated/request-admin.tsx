import { createFileRoute, redirect } from '@tanstack/react-router'

import { RequestAdmin } from '@/features/requests/admin'
import { getSiteConfig } from '@/features/site-config/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/request-admin')({
  beforeLoad: async () => {
    const user = useAuthStore.getState().auth.user
    if (!user || user.role < ROLE.ADMIN) throw redirect({ to: '/403' })
    const site = await getSiteConfig()
    if (site.tenant_id !== 0) throw redirect({ to: '/403' })
  },
  component: RequestAdmin,
})
