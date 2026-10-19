/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/
import { createFileRoute, redirect, Outlet } from '@tanstack/react-router'
import axios from 'axios'

import { getResellerMe } from '@/features/reseller/api'
import { siteConfigQueryOptions } from '@/features/site-config'
import { resolveAuthentication } from '@/lib/auth-session'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/reseller')({
  beforeLoad: async ({ location, context }) => {
    const [, site] = await Promise.all([
      resolveAuthentication(),
      context.queryClient.fetchQuery(siteConfigQueryOptions()),
    ])
    if (site.tenant_id === 0) throw redirect({ to: '/403' })
    const { auth } = useAuthStore.getState()

    if (!auth.user || !auth.accessToken) {
      throw redirect({
        to: '/sign-in',
        search: { redirect: location.href, owner_login: true },
      })
    }

    try {
      await getResellerMe()
    } catch (error) {
      // The endpoint is intentionally tenant and owner scoped. Any failure to
      // prove that scope fails closed; the page must never render as a generic
      // platform admin surface.
      if (
        axios.isAxiosError(error) &&
        [401, 403].includes(error.response?.status ?? 0)
      ) {
        throw redirect({
          to: '/sign-in',
          search: { redirect: location.href, owner_login: true },
        })
      }
      throw redirect({ to: '/403' })
    }
  },
  component: Outlet,
})
