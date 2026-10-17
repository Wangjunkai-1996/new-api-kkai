import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'
import { parseConsoleContract, supportsConsole } from '@/lib/console-contract'
import { statusQueryOptions } from '@/lib/status-query'

export function useConsoleContract() {
  const delivery = import.meta.env.VITE_KKAI_FRONTEND_DELIVERY
  const independent = delivery === 'independent'
  const statusQuery = useQuery({
    ...statusQueryOptions,
    enabled: !independent,
    select: (status) => parseConsoleContract(status?.console_contract),
    refetchInterval: 30_000,
    refetchOnWindowFocus: 'always',
    retry: false,
  })
  const runtimeQuery = useQuery({
    queryKey: ['console-contract', delivery],
    enabled: independent,
    queryFn: async () => {
      const raw = (
        await api.get('/console-runtime.json', { skipErrorHandler: true })
      ).data
      const contract = parseConsoleContract(raw)
      if (!contract) throw new Error('Console compatibility is unavailable')
      return contract
    },
    staleTime: 0,
    refetchInterval: 30_000,
    refetchOnWindowFocus: 'always',
    retry: false,
  })
  const query = independent ? runtimeQuery : statusQuery
  const contract = query.isError ? null : (query.data ?? null)
  return {
    contract,
    compatible: supportsConsole(contract),
    required: independent,
    loading: query.isPending,
    retry: query.refetch,
  }
}
