import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useStatus } from '@/hooks/use-status'
import { api } from '@/lib/api'
import { STATUS_QUERY_KEY, ensureStatus } from '@/lib/status-query'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { useConsoleContract } from './use-console-contract'

const contract = {
  format_version: 1,
  api_contracts: [2],
  capabilities: [
    'dashboard_jwt',
    'scoped_access_tokens',
    'task_plugins',
    'studio_media_urls',
    'token_group_inline',
    'reseller_sites',
    'reseller_group_pricing',
  ],
}
let queryClient: QueryClient

function Wrapper(props: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      {props.children}
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.stubEnv('VITE_KKAI_FRONTEND_DELIVERY', 'legacy')
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  localStorage.clear()
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
})

afterEach(() => {
  cleanup()
  queryClient.clear()
  localStorage.clear()
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  vi.restoreAllMocks()
  vi.unstubAllEnvs()
})

describe('useConsoleContract', () => {
  it('shares the status request with bootstrap and hook consumers', async () => {
    const apiGet = vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: { console_contract: contract } },
    })
    const bootstrap = ensureStatus(queryClient)
    const { result } = renderHook(
      () => ({ status: useStatus(), console: useConsoleContract() }),
      { wrapper: Wrapper }
    )

    await bootstrap
    await waitFor(() => expect(result.current.console.compatible).toBe(true))
    expect(result.current.status.status).toMatchObject({
      console_contract: contract,
    })
    expect(apiGet).toHaveBeenCalledExactlyOnceWith('/api/status')
    expect(result.current.console.required).toBe(false)
  })

  it('reuses fresh status and observes a shared refresh immediately', async () => {
    queryClient.setQueryData(STATUS_QUERY_KEY, { console_contract: contract })
    const apiGet = vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: { console_contract: { ...contract, api_contracts: [1] } },
      },
    })
    const { result } = renderHook(() => useConsoleContract(), {
      wrapper: Wrapper,
    })

    expect(result.current.compatible).toBe(true)
    expect(apiGet).not.toHaveBeenCalled()
    await act(() =>
      queryClient.invalidateQueries({ queryKey: STATUS_QUERY_KEY })
    )
    await waitFor(() => expect(result.current.compatible).toBe(false))
    expect(result.current.contract?.api_contracts).toEqual([1])
    expect(apiGet).toHaveBeenCalledExactlyOnceWith('/api/status')
  })

  it('uses only the runtime declaration for independent delivery', async () => {
    vi.stubEnv('VITE_KKAI_FRONTEND_DELIVERY', 'independent')
    const apiGet = vi.spyOn(api, 'get').mockResolvedValue({ data: contract })
    const { result } = renderHook(() => useConsoleContract(), {
      wrapper: Wrapper,
    })

    expect(result.current.loading).toBe(true)
    expect(result.current.compatible).toBe(false)
    await waitFor(() => expect(result.current.compatible).toBe(true))
    expect(result.current.loading).toBe(false)
    expect(apiGet).toHaveBeenCalledExactlyOnceWith('/console-runtime.json', {
      skipErrorHandler: true,
    })
    expect(result.current.required).toBe(true)
  })

  it.each(['reseller_sites', 'reseller_group_pricing'])(
    'blocks independent delivery against a backend without %s',
    async (missingCapability) => {
      vi.stubEnv('VITE_KKAI_FRONTEND_DELIVERY', 'independent')
      vi.spyOn(api, 'get').mockResolvedValue({
        data: {
          ...contract,
          capabilities: contract.capabilities.filter(
            (name) => name !== missingCapability
          ),
        },
      })
      const { result } = renderHook(() => useConsoleContract(), {
        wrapper: Wrapper,
      })
      await waitFor(() => expect(result.current.loading).toBe(false))
      expect(result.current.required).toBe(true)
      expect(result.current.contract?.api_contracts).toEqual([2])
      expect(result.current.contract?.capabilities).not.toContain(
        missingCapability
      )
      expect(result.current.compatible).toBe(false)
    }
  )

  it.each(['legacy', 'independent'])(
    'does not trust persisted status after a cold %s request fails',
    async (delivery) => {
      vi.stubEnv('VITE_KKAI_FRONTEND_DELIVERY', delivery)
      localStorage.setItem(
        'status',
        JSON.stringify({ console_contract: contract })
      )
      const apiGet = vi
        .spyOn(api, 'get')
        .mockRejectedValue(new Error('temporary network failure'))
      const { result } = renderHook(() => useConsoleContract(), {
        wrapper: Wrapper,
      })

      await waitFor(() => expect(result.current.loading).toBe(false))
      expect(apiGet).toHaveBeenCalledOnce()
      expect(result.current.contract).toBeNull()
      expect(result.current.compatible).toBe(false)
      expect(result.current.required).toBe(delivery === 'independent')
    }
  )

  it.each(['network failure', 'invalid declaration'])(
    'blocks an independent frontend on %s and recovers on retry',
    async (failure) => {
      vi.stubEnv('VITE_KKAI_FRONTEND_DELIVERY', 'independent')
      const apiGet = vi.spyOn(api, 'get').mockResolvedValue({ data: contract })
      const { result } = renderHook(() => useConsoleContract(), {
        wrapper: Wrapper,
      })
      await waitFor(() => expect(result.current.compatible).toBe(true))

      if (failure === 'network failure') {
        apiGet.mockRejectedValueOnce(new Error('temporary network failure'))
      } else {
        apiGet.mockResolvedValueOnce({ data: '<html>Unavailable</html>' })
      }
      await act(() => result.current.retry())
      await waitFor(() => expect(result.current.contract).toBeNull())
      expect(result.current.compatible).toBe(false)
      expect(result.current.loading).toBe(false)

      await act(() => result.current.retry())
      await waitFor(() => expect(result.current.compatible).toBe(true))
      expect(result.current.contract).toEqual(contract)
    }
  )
})
