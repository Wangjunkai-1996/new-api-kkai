import { useQuery } from '@tanstack/react-query'
import { getInvitationFeatureStatus } from '../api'

const DISABLED_STATUS = {
  available: false,
  userInvitationRebateEnabled: false,
  orderRebateEnabled: false,
  invitationSignupRewardEnabled: false,
  rebateToBalanceEnabled: false,
}

export function useInvitationFeatureStatus() {
  const query = useQuery({
    queryKey: ['invitationFeatureStatus'],
    queryFn: async () => {
      const response = await getInvitationFeatureStatus()
      return response.data ?? DISABLED_STATUS
    },
    retry: false,
    staleTime: 60_000,
    gcTime: 5 * 60_000,
  })

  const available = query.isSuccess && query.data?.available === true
  const userVisible =
    available && query.data?.userInvitationRebateEnabled === true
  const rebateRecordsVisible =
    userVisible &&
    (query.data?.orderRebateEnabled === true ||
      query.data?.invitationSignupRewardEnabled === true)
  const rebateManagementVisible =
    rebateRecordsVisible && query.data?.rebateToBalanceEnabled === true

  return {
    query,
    available,
    userVisible,
    rebateRecordsVisible,
    rebateManagementVisible,
    hasAnyRebateFeature: rebateRecordsVisible || rebateManagementVisible,
  }
}
