import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useConsoleContract } from './use-console-contract'

const apiGet = vi.hoisted(() => vi.fn())
const getStatus = vi.hoisted(() => vi.fn())

vi.mock('@/lib/api', () => ({
  api: { get: apiGet },
  getStatus,
}))

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    )
  }
}

describe('useConsoleContract', () => {
  beforeEach(() => {
    vi.stubEnv('VITE_KKAI_FRONTEND_DELIVERY', 'legacy')
    getStatus.mockResolvedValue({
      console_contract: {
        format_version: 1,
        api_contracts: [1],
        capabilities: ['token_group_inline'],
      },
    })
    apiGet.mockResolvedValue({
      data: { format_version: 1, api_contracts: [1], capabilities: [] },
    })
  })

  afterEach(() => {
    vi.clearAllMocks()
    vi.unstubAllEnvs()
  })

  it('reads legacy capability declarations from the backend status endpoint', async () => {
    const { result } = renderHook(() => useConsoleContract(), {
      wrapper: createWrapper(),
    })

    await waitFor(() => expect(result.current.contract).not.toBeNull())

    expect(getStatus).toHaveBeenCalledTimes(1)
    expect(apiGet).not.toHaveBeenCalled()
    expect(result.current.compatible).toBe(true)
  })

  it('uses the static runtime declaration only for independent delivery', async () => {
    vi.stubEnv('VITE_KKAI_FRONTEND_DELIVERY', 'independent')

    const { result } = renderHook(() => useConsoleContract(), {
      wrapper: createWrapper(),
    })

    await waitFor(() => expect(result.current.contract).not.toBeNull())

    expect(apiGet).toHaveBeenCalledWith('/console-runtime.json', {
      skipErrorHandler: true,
    })
    expect(getStatus).not.toHaveBeenCalled()
    expect(result.current.required).toBe(true)
  })
})
