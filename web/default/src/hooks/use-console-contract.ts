import { useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'
import { parseConsoleContract, supportsConsole } from '@/lib/console-contract'
import { ensureStatus } from '@/lib/status-query'

export function useConsoleContract() {
  const queryClient = useQueryClient()
  const delivery = import.meta.env.VITE_KKAI_FRONTEND_DELIVERY
  const query = useQuery({
    queryKey: ['console-contract', delivery],
    queryFn: async () => {
      const raw =
        delivery === 'independent'
          ? (await api.get('/console-runtime.json', { skipErrorHandler: true }))
              .data
          : (await ensureStatus(queryClient))?.console_contract
      const contract = parseConsoleContract(raw)
      if (!contract) throw new Error('Console compatibility is unavailable')
      return contract
    },
    staleTime: 0,
    refetchInterval: 30_000,
    refetchOnWindowFocus: 'always',
    retry: false,
  })
  // Keep the last valid declaration during a transient refresh failure. The
  // boundary must still block on a cold-start failure, where no declaration
  // exists yet.
  const contract = query.data ?? null
  return {
    contract,
    compatible: supportsConsole(contract),
    required: delivery === 'independent',
    retry: query.refetch,
  }
}
