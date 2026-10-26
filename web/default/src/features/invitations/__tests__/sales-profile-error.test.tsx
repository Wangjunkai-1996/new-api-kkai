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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, expect, test, vi } from 'vitest'

import { KkaiWalletRewardsCard } from '@/features/kkai-wallet/invitation-rewards-card'
import { getSiteConfigQueryKey } from '@/features/site-config/api'
import { useSidebarData } from '@/hooks/use-sidebar-data'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { Invitations } from '../index'

const { navigate } = vi.hoisted(() => ({ navigate: vi.fn() }))
vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  useNavigate: () => navigate,
  useSearch: () => ({}),
  Link: (props: { to: string; children?: ReactNode }) => (
    <a href={props.to}>{props.children}</a>
  ),
}))

const clients: QueryClient[] = []
afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
  useAuthStore.getState().auth.reset()
  navigate.mockReset()
})

function InvitationSidebarEntry() {
  const sidebar = useSidebarData()
  const entry = sidebar.navGroups
    .flatMap((group) => group.items)
    .find((item) => item.url === '/invitations')
  return entry ? <a href='/invitations'>{entry.title}</a> : null
}

test.each([
  {
    ordinaryEnabled: false,
    failingEndpoint: 'sales-profile',
    errorTitle: 'Failed to load sales account',
  },
  {
    ordinaryEnabled: true,
    failingEndpoint: 'sales-profile',
    errorTitle: 'Failed to load sales account',
  },
  {
    ordinaryEnabled: false,
    failingEndpoint: 'status',
    errorTitle: 'Failed to load invitation settings',
  },
])(
  'keeps the invitation entry and retry page after $failingEndpoint fails (ordinary enabled: $ordinaryEnabled)',
  async (scenario) => {
    useAuthStore
      .getState()
      .auth.setUser({ id: 4, username: 'seller-four', role: 1 })
    let failing = true
    vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (
        failing &&
        url === `/invitations/api/invitations/${scenario.failingEndpoint}`
      ) {
        throw new Error('Database unavailable')
      }
      if (url.endsWith('/status')) {
        return {
          data: {
            success: true,
            data: {
              available: true,
              settlementEnabled: true,
              userInvitationRebateEnabled: scenario.ordinaryEnabled,
              orderRebateEnabled: true,
            },
          },
        }
      }
      if (url.endsWith('/sales-profile')) {
        return {
          data: { success: true, data: { active: true, registered: true } },
        }
      }
      return {
        data: {
          success: true,
          data: {
            items: [],
            total: 0,
            page: 1,
            pageSize: 20,
            creditCurrency: 'usd_credit_v1',
            summary: {
              invitedCount: 0,
              effectiveInvitedCount: 0,
              rechargeAmount: 0,
              unverifiedRechargeCount: 0,
              rebates: {
                cooling: 0,
                available: 0,
                pendingApproval: 0,
                approved: 0,
                issued: 0,
                locked: 0,
                closed: 0,
                pendingCalculationCount: 0,
              },
            },
          },
        },
      }
    })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    client.setQueryData(getSiteConfigQueryKey(), {
      tenant_id: 0,
      hostname: window.location.host,
      status: 'active',
      site_name: 'Test site',
    })
    clients.push(client)
    const user = userEvent.setup()
    render(
      <QueryClientProvider client={client}>
        <InvitationSidebarEntry />
        <Invitations />
      </QueryClientProvider>
    )
    await screen.findByText(scenario.errorTitle)
    expect(screen.getByText('Database unavailable')).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: 'Invitation Rebate' })
    ).toHaveAttribute('href', '/invitations')
    expect(screen.queryByRole('tab')).not.toBeInTheDocument()
    expect(navigate).not.toHaveBeenCalled()

    failing = false
    await user.click(screen.getByRole('button', { name: 'Retry' }))
    await screen.findByRole('heading', { name: 'Sales Invitations' })
    await screen.findByText('No invited customers')
    expect(screen.queryByText(scenario.errorTitle)).not.toBeInTheDocument()
    expect(navigate).not.toHaveBeenCalled()
  }
)

test('wallet retries unknown sales identity instead of exposing the ordinary transfer fallback', async () => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 4, username: 'seller-four', role: 1 })
  let failing = true
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url.endsWith('/status')) {
      return {
        data: {
          success: true,
          data: { available: true, userInvitationRebateEnabled: true },
        },
      }
    }
    if (url.endsWith('/sales-profile')) {
      if (failing) throw new Error('Database unavailable')
      return {
        data: { success: true, data: { registered: true, active: true } },
      }
    }
    return {
      data: {
        success: true,
        data: {
          invitationCode: 'seller-code',
          confirmingRebate: 0,
          pendingRebate: 0,
          invitedCount: 0,
        },
      },
    }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  const user = userEvent.setup()
  render(
    <QueryClientProvider client={client}>
      <KkaiWalletRewardsCard
        user={null}
        affiliateLink='https://example.test/ref'
        onTransfer={vi.fn()}
      />
    </QueryClientProvider>
  )
  await screen.findByText('Failed to load sales account')
  expect(screen.queryByText('Referral Program')).not.toBeInTheDocument()
  expect(get.mock.calls.some(([url]) => url.endsWith('/my-code'))).toBe(false)
  failing = false
  await user.click(screen.getByRole('button', { name: 'Retry' }))
  await screen.findByRole('link', { name: 'Rebate Center' })
  expect(
    screen.queryByText('Failed to load sales account')
  ).not.toBeInTheDocument()
})
