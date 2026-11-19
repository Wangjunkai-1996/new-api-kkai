import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook } from '@testing-library/react'
import { expect, it, vi } from 'vitest'

import { aboutContentQueryOptions } from '@/features/about/api'
import { siteConfigQueryOptions } from '@/features/site-config'
import { STATUS_QUERY_KEY } from '@/lib/status-query'

import { useTopNavLinks } from '../use-top-nav-links'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

it.each([
  { content: '  ', enabled: true, tenant: 0, visible: false },
  { content: 'About this store', enabled: true, tenant: 0, visible: true },
  { content: 'About this store', enabled: false, tenant: 0, visible: false },
  { content: 'About this store', enabled: true, tenant: 1, visible: false },
])(
  'shows About=$visible for content="$content", enabled=$enabled, tenant=$tenant',
  (scenario) => {
    const client = new QueryClient()
    client.setQueryData(siteConfigQueryOptions().queryKey, {
      tenant_id: scenario.tenant,
      hostname: 'localhost',
      site_name: 'KKAI',
      status: 'active',
    })
    client.setQueryData(STATUS_QUERY_KEY, {
      HeaderNavModules: { about: scenario.enabled },
    })
    client.setQueryData(aboutContentQueryOptions.queryKey, {
      success: true,
      message: '',
      data: scenario.content,
    })
    const { result } = renderHook(useTopNavLinks, {
      wrapper: ({ children }) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    })
    expect(result.current.some((link) => link.href === '/about')).toBe(
      scenario.visible
    )
  }
)
