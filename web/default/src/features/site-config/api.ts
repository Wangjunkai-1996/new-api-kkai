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
import { queryOptions } from '@tanstack/react-query'
import { z } from 'zod'

import { api } from '@/lib/api'
import { getCurrentHost } from '@/lib/host-scope'
import { requireServerSuccess } from '@/lib/server-error-message'

const siteConfigSchema = z.object({
  tenant_id: z.number().int().nonnegative(),
  hostname: z.string(),
  status: z.enum(['active', 'disabled', 'provisioning']),
  site_name: z.string(),
  logo: z.string().optional(),
  contact: z.string().optional(),
  announcement: z.string().optional(),
  homepage_content: z.string().optional(),
  enabled_modules: z.record(z.string(), z.boolean()).optional(),
  maintenance: z.boolean().optional(),
})

export type SiteConfig = z.infer<typeof siteConfigSchema>
export { getCurrentHost }

export function getSiteConfigQueryKey(host = getCurrentHost()) {
  return ['site-config', host] as const
}

export async function getSiteConfig(): Promise<SiteConfig> {
  const response = await api.get('/api/site-config', { skipErrorHandler: true })
  return siteConfigSchema.parse(requireServerSuccess(response.data).data)
}

export function siteConfigQueryOptions() {
  return queryOptions({
    queryKey: getSiteConfigQueryKey(),
    queryFn: getSiteConfig,
    staleTime: 30_000,
    gcTime: 10 * 60_000,
    retry: false,
  })
}
