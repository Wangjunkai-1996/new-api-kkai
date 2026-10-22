import { useQuery } from '@tanstack/react-query'

import { useSiteConfig } from '@/features/site-config'
import { useAuthStore } from '@/stores/auth-store'

import {
  getRequestNotifications,
  isRequestTicketUnavailable,
  requestQueryKey,
} from './api'

export function useRequestAvailability() {
  const userId = useAuthStore((state) => state.auth.user?.id)
  const { data: site } = useSiteConfig()
  const eligible = Boolean(userId) && site?.tenant_id === 0
  const query = useQuery({
    queryKey: requestQueryKey(userId, 'notifications'),
    queryFn: getRequestNotifications,
    enabled: eligible,
    staleTime: 30_000,
    refetchInterval: 30_000,
    refetchOnWindowFocus: true,
    retry: false,
    meta: { errorToast: false },
  })

  return {
    query,
    eligible,
    available:
      eligible &&
      query.data !== undefined &&
      !isRequestTicketUnavailable(query.error),
  }
}
