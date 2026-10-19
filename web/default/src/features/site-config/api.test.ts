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
import { QueryClient } from '@tanstack/react-query'
import { AxiosError } from 'axios'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import * as hostScope from '@/lib/host-scope'

import { getSiteConfig, siteConfigQueryOptions } from './api'

const client = new QueryClient({
  defaultOptions: { queries: { retry: false } },
})
afterEach(() => {
  client.clear()
  vi.restoreAllMocks()
})

describe('Host-derived public branding', () => {
  test('a warm cache for one Host cannot replace the other Host response', async () => {
    const host = vi
      .spyOn(hostScope, 'getCurrentHost')
      .mockReturnValue('alpha.example.com')
    const request = vi
      .spyOn(api, 'get')
      .mockResolvedValueOnce({
        data: {
          success: true,
          data: {
            tenant_id: 1,
            hostname: 'alpha.example.com',
            status: 'active',
            site_name: 'Alpha',
          },
        },
      })
      .mockResolvedValueOnce({
        data: {
          success: true,
          data: {
            tenant_id: 2,
            hostname: 'beta.example.com',
            status: 'active',
            site_name: 'Beta',
          },
        },
      })
    expect((await client.fetchQuery(siteConfigQueryOptions())).site_name).toBe(
      'Alpha'
    )
    host.mockReturnValue('beta.example.com')
    expect((await client.fetchQuery(siteConfigQueryOptions())).site_name).toBe(
      'Beta'
    )
    host.mockReturnValue('alpha.example.com')
    expect((await client.fetchQuery(siteConfigQueryOptions())).site_name).toBe(
      'Alpha'
    )
    expect(request).toHaveBeenCalledTimes(2)
  })

  test('unknown Host failures are not replaced by platform branding', async () => {
    const failure = new AxiosError(
      'Unknown Host',
      'ERR_BAD_REQUEST',
      undefined,
      undefined,
      { status: 404 } as never
    )
    vi.spyOn(api, 'get').mockRejectedValue(failure)
    await expect(getSiteConfig()).rejects.toBe(failure)
  })

  test('rejects malformed responses before rendering tenant content', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: { site_name: 'No tenant identity' } },
    })
    await expect(getSiteConfig()).rejects.toThrow()
  })
  test('accepts a tenant that is still being provisioned', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: {
          tenant_id: 8,
          hostname: 'pilot.example.com',
          status: 'provisioning',
          site_name: 'Pilot',
          enabled_modules: { reseller: true },
        },
      },
    })
    await expect(getSiteConfig()).resolves.toMatchObject({
      status: 'provisioning',
      enabled_modules: { reseller: true },
    })
  })
})
