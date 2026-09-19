import { useQuery } from '@tanstack/react-query'

import { api, getStatus } from '@/lib/api'
import { parseConsoleContract, supportsConsole } from '@/lib/console-contract'

export function useConsoleContract() {
  const delivery = import.meta.env.VITE_KKAI_FRONTEND_DELIVERY
  const query = useQuery({
    queryKey: ['console-contract', delivery],
    queryFn: async () => {
      const raw =
        delivery === 'independent'
          ? (await api.get('/console-runtime.json', { skipErrorHandler: true }))
              .data
          : (await getStatus())?.console_contract
      const contract = parseConsoleContract(raw)
      if (!contract) throw new Error('Console compatibility is unavailable')
      return contract
    },
    staleTime: 0,
    refetchInterval: 30_000,
    refetchOnWindowFocus: 'always',
    retry: false,
  })
  const contract = query.isError ? null : (query.data ?? null)
  return {
    contract,
    compatible: supportsConsole(contract),
    required: delivery === 'independent',
    retry: query.refetch,
  }
}
