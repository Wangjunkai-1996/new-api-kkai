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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { getMembershipAdminConfig } from './api'
import { MembershipAdmin } from './admin'
import type { MembershipCatalog } from './types'

vi.mock('./api', () => ({
  getMembershipAdminConfig: vi.fn(),
  publishMembershipCatalog: vi.fn(),
}))
vi.mock('@/components/layout/components/section-page-layout', () => {
  const Slot = ({ children }: { children?: ReactNode }) => (
    <div>{children}</div>
  )
  return {
    SectionPageLayout: Object.assign(Slot, {
      Title: Slot,
      Actions: Slot,
      Content: Slot,
    }),
  }
})
vi.mock('./components/orders', () => ({ MembershipOrders: () => null }))
vi.mock('./components/billing-reviews', () => ({
  MembershipBillingReviews: () => null,
}))

const catalog: MembershipCatalog = {
  runtime_enabled: false,
  schema_ready: true,
  release_id: 5,
  sales_enabled: true,
  tiers: [
    {
      id: 'gold',
      name: 'Gold',
      rank: 1,
      style: 'gold',
      price_minor: 55800,
      currency: 'CNY',
      quota: 558_000_000,
      enabled: true,
      discounts: { enterprise: 0.96 },
    },
  ],
  groups: [
    {
      key: 'enterprise',
      display_name: 'Enterprise',
      enabled: true,
      published: true,
    },
  ],
}

function renderAdmin() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <MembershipAdmin />
    </QueryClientProvider>
  )
}

describe('membership admin readiness', () => {
  it('keeps the catalog editable when schema is ready but runtime is disabled', async () => {
    vi.mocked(getMembershipAdminConfig).mockResolvedValue(catalog)
    renderAdmin()

    expect(
      await screen.findByText('Memberships are not available yet.')
    ).toBeVisible()
    expect(screen.getByText('Runtime: Disabled')).toBeVisible()
    const title = await screen.findByRole('textbox', { name: 'Plan Title' })
    expect(title).toHaveValue('Gold')
    fireEvent.change(title, { target: { value: 'Gold Plus' } })
    expect(
      screen.getByRole('button', { name: 'Publish membership configuration' })
    ).toBeEnabled()
  })

  it('hides the catalog editor until the membership schema is ready', async () => {
    vi.mocked(getMembershipAdminConfig).mockResolvedValue({
      ...catalog,
      schema_ready: false,
    })
    renderAdmin()

    expect(
      await screen.findByText('Database: Unavailable')
    ).toBeVisible()
    expect(
      screen.queryByRole('textbox', { name: 'Plan Title' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', {
        name: 'Publish membership configuration',
      })
    ).not.toBeInTheDocument()
  })
})
