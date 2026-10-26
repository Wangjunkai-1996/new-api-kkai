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
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { RatioValue } from './components/ratio-price'
import { MembershipRatiosPage } from './ratios-page'
import type {
  MembershipAccount,
  MembershipCatalog,
  MembershipSelf,
  MembershipTier,
} from './types'

const membershipMock = vi.hoisted(() => vi.fn())
vi.mock('./hooks', () => ({ useMembership: membershipMock }))

const tier: MembershipTier = {
  id: 'orbit',
  name: 'Orbit Club',
  rank: 4,
  style: 'gold',
  price_minor: 60000,
  currency: 'CNY',
  quota: 600000,
  enabled: true,
  discounts: { enterprise: 0.9, legacy: 0.95, playground: 0.9, standard: 1 },
}
const catalog: MembershipCatalog = {
  runtime_enabled: true,
  release_id: 7,
  sales_enabled: true,
  tiers: [tier],
  groups: [
    {
      key: 'enterprise',
      display_name: 'Enterprise',
      enabled: true,
      published: true,
    },
    {
      key: 'legacy',
      display_name: 'Legacy group',
      enabled: true,
      published: true,
    },
    {
      key: 'playground',
      display_name: 'Playground',
      enabled: true,
      published: true,
    },
    {
      key: 'standard',
      display_name: 'Standard group',
      enabled: true,
      published: true,
    },
  ],
  group_quotes: [
    {
      group_key: 'enterprise',
      display_name: 'Enterprise',
      description: 'Long-context requests',
      base_ratio: 0.2,
      wallet_ratio: 0.2,
      tier_quotes: {
        orbit: { coefficient: 0.9, membership_ratio: 0.18, savings: 0.1 },
      },
    },
    {
      group_key: 'legacy',
      display_name: 'Legacy group',
      description: 'Existing negotiated price',
      base_ratio: 0.2,
      wallet_ratio: 0.16,
      tier_quotes: {
        orbit: { coefficient: 0.95, membership_ratio: 0.16, savings: 0 },
      },
    },
    {
      group_key: 'playground',
      display_name: 'Playground',
      description: 'Free requests',
      base_ratio: 0,
      wallet_ratio: 0,
      tier_quotes: {
        orbit: { coefficient: 0.9, membership_ratio: 0, savings: 0 },
      },
    },
    {
      group_key: 'standard',
      display_name: 'Standard group',
      description: 'No additional discount',
      base_ratio: 0.2,
      wallet_ratio: 0.25,
      tier_quotes: {
        orbit: { coefficient: 1, membership_ratio: 0.25, savings: 0 },
      },
    },
  ],
}
const account: MembershipAccount = {
  id: 10,
  user_id: 9,
  tier_id: tier.id,
  rank: tier.rank,
  release_id: 3,
  amount_total: 600000,
  amount_used: 100000,
  amount_reserved: 0,
  expires_at: 2000,
  revision: 2,
  status: 'active',
}
const membership: MembershipSelf = {
  runtime_enabled: true,
  server_time: 1000,
  account,
  tier,
  discounts: (catalog.group_quotes ?? []).map((quote) => ({
    group_key: quote.group_key,
    display_name: quote.display_name,
    description: quote.description,
    base_ratio: quote.base_ratio,
    wallet_ratio: quote.wallet_ratio,
    ...quote.tier_quotes.orbit,
    enabled: true,
    published: true,
  })),
  pending_order: null,
}

function setMembership(
  catalogData: MembershipCatalog = catalog,
  selfData: MembershipSelf = membership
) {
  membershipMock.mockReturnValue({
    catalog: {
      data: catalogData,
      isPending: false,
      isError: false,
      isFetching: false,
    },
    self: {
      data: selfData,
      isPending: false,
      isError: false,
      isFetching: false,
    },
    refresh: vi.fn(),
    now: 1000,
    userId: 9,
  })
}

async function renderRatios() {
  const router = createRouter({
    routeTree: createRootRoute({ component: MembershipRatiosPage }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  return render(<RouterProvider router={router} />)
}

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined)
  setMembership()
})

describe('membership ratio comparison', () => {
  it('keeps tiny positive ratios distinct from free groups', () => {
    const { rerender } = render(<RatioValue value={0.000000001} />)
    expect(screen.getByText('0.000000001')).toBeVisible()
    expect(screen.queryByText('Free of charge')).not.toBeInTheDocument()

    rerender(<RatioValue value={0} />)
    expect(screen.getByText('Free of charge')).toBeVisible()
    expect(screen.queryByText('0.000000001')).not.toBeInTheDocument()
  })

  it('shows only configured tiers and authoritative group prices without stacking discounts', async () => {
    await renderRatios()

    expect(
      screen.getByRole('link', { name: 'View membership plans' })
    ).toHaveAttribute('href', '/membership')
    const table = screen.getByRole('table', { name: 'Group ratio comparison' })
    const orbitColumn = within(table).getByRole('columnheader', {
      name: /Orbit Club.*Group discounts/,
    })
    expect(orbitColumn).toHaveAttribute('aria-current', 'true')
    expect(
      within(table).queryByRole('columnheader', {
        name: /My membership ratio/,
      })
    ).not.toBeInTheDocument()
    expect(
      within(table).queryByRole('columnheader', { name: /Diamond|King/ })
    ).not.toBeInTheDocument()

    const enterprise = within(table).getByRole('row', {
      name: /Enterprise.*Long-context requests/,
    })
    const enterpriseCells = within(enterprise).getAllByRole('cell')
    expect(enterpriseCells).toHaveLength(3)
    expect(within(enterpriseCells[2]).getByText('0.18')).toBeVisible()
    expect(
      within(enterpriseCells[2]).getByText('Save 10% vs wallet')
    ).toBeVisible()

    const legacy = within(table).getByRole('row', {
      name: /Legacy group.*Existing negotiated price/,
    })
    expect(within(legacy).getAllByText('0.16')).toHaveLength(2)
    expect(
      within(within(legacy).getAllByRole('cell')[2]).getByText(
        'Existing discount retained'
      )
    ).toBeVisible()
    expect(within(legacy).queryByText('0.19')).not.toBeInTheDocument()

    const free = within(table).getByRole('row', {
      name: /Playground.*Free requests/,
    })
    expect(within(free).getAllByText('Free group')).toHaveLength(1)
    expect(within(free).queryByText(/NaN|Infinity/)).not.toBeInTheDocument()

    const standard = within(table).getByRole('row', {
      name: /Standard group.*No additional discount/,
    })
    expect(within(standard).getAllByText('0.25')).toHaveLength(2)
    expect(
      within(within(standard).getAllByRole('cell')[2]).getByText(
        'No extra membership discount'
      )
    ).toBeVisible()
  })

  it('fails clearly when an older backend omits group quotes instead of showing zero groups or savings', async () => {
    const oldCatalog = { ...catalog }
    delete oldCatalog.group_quotes
    setMembership(oldCatalog)
    await renderRatios()

    expect(
      screen.getByText(
        'Membership information could not be loaded. Please refresh before purchasing.'
      )
    ).toBeVisible()
    expect(
      screen.queryByRole('region', { name: 'Membership benefits summary' })
    ).not.toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    expect(screen.queryByText('No matching groups')).not.toBeInTheDocument()
    expect(screen.queryByText('0.18')).not.toBeInTheDocument()
    expect(screen.queryByText('Save 10% vs wallet')).not.toBeInTheDocument()
  })

  it('does not present incomplete benefit responses as current prices during a mixed backend rollout', async () => {
    setMembership(catalog, {
      ...membership,
      discounts: membership.discounts.map((quote) => {
        const legacyQuote = { ...quote }
        delete legacyQuote.base_ratio
        delete legacyQuote.savings
        return legacyQuote
      }),
    })
    await renderRatios()

    expect(
      screen.getByText(
        'Membership information could not be loaded. Please refresh before purchasing.'
      )
    ).toBeVisible()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    expect(screen.queryByText('0.18')).not.toBeInTheDocument()
    expect(screen.queryByText('Save 10% vs wallet')).not.toBeInTheDocument()
  })

  it('does not show incomplete member prices', async () => {
    setMembership(catalog, {
      ...membership,
      discounts: membership.discounts.filter(
        (quote) => quote.group_key !== 'enterprise'
      ),
    })
    await renderRatios()

    expect(
      screen.getByText(
        'Membership information could not be loaded. Please refresh before purchasing.'
      )
    ).toBeVisible()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('shows current published discounts for an existing member and preserves authoritative quotes', async () => {
    await renderRatios()
    expect(screen.getAllByText('0.18')).toHaveLength(1)
    expect(screen.queryByText('0.19')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('tab', { name: 'My benefits' }))
    expect(screen.getByText('0.18')).toBeVisible()
    expect(screen.getByText('Save 10% vs wallet')).toBeVisible()
    expect(
      screen.getByRole('columnheader', {
        name: /Orbit Club.*My benefits/,
      })
    ).toBeVisible()

    const group = screen.getByRole('button', {
      name: /Enterprise.*Long-context requests/,
    })
    await userEvent.click(group)
    expect(group).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText(/Membership coefficient/)).toHaveTextContent('90%')
    expect(
      screen.getByText(
        'Your benefits use the latest published discounts for your membership tier.'
      )
    ).toBeVisible()

    await userEvent.click(screen.getByRole('tab', { name: 'Membership plans' }))
    expect(screen.getAllByText('0.18')).toHaveLength(1)
    expect(screen.queryByText('0.19')).not.toBeInTheDocument()
  })

  it('shows the active member price in its tier without changing other tier offers', async () => {
    const otherTier = { ...tier, id: 'nova', name: 'Nova Club', rank: 5 }
    setMembership(
      {
        ...catalog,
        tiers: [tier, otherTier],
        group_quotes: catalog.group_quotes?.map((quote) => ({
          ...quote,
          tier_quotes: { ...quote.tier_quotes, nova: quote.tier_quotes.orbit },
        })),
      },
      {
        ...membership,
        discounts: membership.discounts.map((quote) => ({
          ...quote,
          membership_ratio: 0.17,
          savings: 0.15,
        })),
      }
    )
    await renderRatios()

    const row = screen.getByRole('row', {
      name: /Enterprise.*Long-context requests/,
    })
    const cells = within(row).getAllByRole('cell')
    expect(cells).toHaveLength(4)
    expect(within(cells[2]).getByText('0.17')).toBeVisible()
    expect(within(cells[2]).getByText('Save 15% vs wallet')).toBeVisible()
    expect(within(cells[3]).getByText('0.18')).toBeVisible()
    expect(within(cells[3]).getByText('Save 10% vs wallet')).toBeVisible()
    expect(
      screen.getByRole('columnheader', { name: /Orbit Club.*Group discounts/ })
    ).toHaveAttribute('aria-current', 'true')
    expect(
      screen.getByRole('columnheader', { name: /Nova Club.*Group discounts/ })
    ).not.toHaveAttribute('aria-current')
  })

  it('searches group descriptions and filters by actual savings', async () => {
    await renderRatios()
    const search = screen.getByRole('searchbox', { name: 'Search groups' })
    fireEvent.change(search, { target: { value: 'long-context' } })
    expect(
      screen.getByRole('button', { name: /Enterprise.*Long-context requests/ })
    ).toBeVisible()
    expect(
      screen.queryByRole('button', { name: /Legacy group/ })
    ).not.toBeInTheDocument()

    fireEvent.change(search, { target: { value: '' } })
    await userEvent.click(
      screen.getByRole('switch', { name: 'Only show savings' })
    )
    expect(
      screen.getByRole('button', { name: /Enterprise.*Long-context requests/ })
    ).toBeVisible()
    expect(
      screen.queryByRole('button', {
        name: /Legacy group|Playground|Standard group/,
      })
    ).not.toBeInTheDocument()
    fireEvent.change(search, { target: { value: 'missing group' } })
    expect(screen.getByText('No matching groups')).toBeVisible()
  })

  it.each([
    ['expiry', { expires_at: 1000 }],
    ['quota exhaustion', { amount_used: 600000 }],
  ])(
    'does not show active prices after %s despite a stale active status',
    async (_reason, accountChange) => {
      setMembership(catalog, {
        ...membership,
        account: { ...account, ...accountChange },
      })
      await renderRatios()
      expect(
        screen.getByRole('columnheader', { name: /Orbit Club/ })
      ).not.toHaveAttribute('aria-current')
      await userEvent.click(screen.getByRole('tab', { name: 'My benefits' }))

      expect(screen.getByText('No active membership benefits')).toBeVisible()
      expect(screen.queryByText('0.18')).not.toBeInTheDocument()
      expect(
        screen.getByText(
          'No membership discount is active. Requests use your wallet at its existing group price.'
        )
      ).toBeVisible()
    }
  )

  it.each(['catalog', 'self'])(
    'does not display membership prices when the %s runtime flag is off',
    async (source) => {
      setMembership(
        { ...catalog, runtime_enabled: source !== 'catalog' },
        { ...membership, runtime_enabled: source !== 'self' }
      )
      await renderRatios()

      expect(
        screen.getByText('Memberships are not available yet.')
      ).toBeVisible()
      expect(screen.queryByRole('table')).not.toBeInTheDocument()
      expect(screen.queryByText('0.18')).not.toBeInTheDocument()
    }
  )

  it('retains current benefits when sales stop and the purchased tier is no longer on sale', async () => {
    setMembership({ ...catalog, sales_enabled: false, tiers: [] })
    await renderRatios()
    await userEvent.click(screen.getByRole('tab', { name: 'My benefits' }))

    expect(
      screen.getByText(
        'New membership purchases are temporarily unavailable. Existing benefits remain valid.'
      )
    ).toBeVisible()
    expect(
      screen.getByRole('columnheader', {
        name: /Orbit Club.*My benefits/,
      })
    ).toBeVisible()
    expect(screen.getByText('0.18')).toBeVisible()
  })

  it('keeps benefits active when remaining quota is reserved rather than consumed', async () => {
    setMembership(catalog, {
      ...membership,
      account: { ...account, amount_reserved: 500000 },
    })
    await renderRatios()
    await userEvent.click(screen.getByRole('tab', { name: 'My benefits' }))

    expect(screen.getByText('0.18')).toBeVisible()
    expect(
      screen.queryByText('No active membership benefits')
    ).not.toBeInTheDocument()
  })
})
