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
import { useQuery } from '@tanstack/react-query'

import { getGroups as getAdminGroups } from '@/features/users/api'
import { getUserGroups } from '@/lib/api'
import { getUserGroupDisplayName } from '@/lib/group-display'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export const GROUP_DISPLAY_NAMES_QUERY_KEY = 'group-display-names'

/**
 * Resolve labels from the authenticated user's group metadata endpoint.
 *
 * The returned object is deliberately keyed by the canonical group id. It is
 * safe to use for labels in admin and user-facing views without ever feeding a
 * renamed label back into a request or filter.
 */
export function useGroupDisplayNames(): Record<string, string> {
  const user = useAuthStore((state) => state.auth.user)
  const isAdmin = (user?.role ?? ROLE.GUEST) >= ROLE.ADMIN
  const { data } = useQuery({
    // Group labels are permission-scoped. Keep each account's response in a
    // separate cache entry so a logout/login cannot briefly show stale labels
    // from the previous account during the query's stale window.
    queryKey: [
      GROUP_DISPLAY_NAMES_QUERY_KEY,
      user?.id ?? null,
      user?.role ?? null,
    ],
    queryFn: async () => {
      const names: Record<string, string> = {}

      if (isAdmin) {
        const response = await getAdminGroups()
        if (response.success) {
          for (const group of response.data ?? []) {
            names[group] = response.display_names?.[group]?.trim() || group
          }
          // Keep labels for a configured key even if an older server omits it
          // from the group list while settings are being refreshed.
          for (const [group, displayName] of Object.entries(
            response.display_names ?? {}
          )) {
            names[group] = displayName?.trim() || group
          }
        }
        return names
      }

      const response = await getUserGroups()
      if (response.success && response.data) {
        for (const [group, info] of Object.entries(response.data)) {
          names[group] = getUserGroupDisplayName(group, info)
        }
      }

      // A freshly renamed or custom user group can briefly be absent from the
      // usable-groups map. The self response still gives us its current label.
      if (user?.group && user.group_display_name?.trim()) {
        names[user.group] = user.group_display_name.trim()
      }
      return names
    },
    enabled: Boolean(user),
    staleTime: 60_000,
  })

  return data ?? {}
}
