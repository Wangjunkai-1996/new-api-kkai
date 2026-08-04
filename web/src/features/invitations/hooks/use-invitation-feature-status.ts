/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useQuery } from '@tanstack/react-query'

import { getInvitationFeatureStatus } from '../api/status'
import {
  deriveInvitationAccess,
  DISABLED_INVITATION_STATUS,
  normalizeInvitationStatus,
} from '../status'

export const INVITATION_STATUS_QUERY_KEY = ['kkai', 'invitations', 'status']

export const useInvitationFeatureStatus = () => {
  const query = useQuery({
    queryKey: INVITATION_STATUS_QUERY_KEY,
    queryFn: async () =>
      normalizeInvitationStatus(await getInvitationFeatureStatus()),
    retry: false,
    staleTime: 60_000,
    gcTime: 5 * 60_000,
  })
  const status = query.data ?? DISABLED_INVITATION_STATUS

  return {
    query,
    status,
    ...deriveInvitationAccess(status),
  }
}
