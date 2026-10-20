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
import {
  cleanup,
  getDefaultNormalizer,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import * as adminApi from './admin-api'
import { AdminApplications } from './admin-applications'
import { AdminTenants } from './admin-tenants'
import { ResellerApplicationPage } from './application'

const site = vi.hoisted(() => ({
  data: {
    reseller_root_domain: 'kkai.win',
    reseller_domain_prefix_enabled: true,
  },
  isPending: false,
}))
const i18n = vi.hoisted(() => ({ language: 'en' }))
vi.mock('@/features/site-config', () => ({ useSiteConfig: () => site }))
vi.mock('@/components/layout', () => ({
  PublicLayout: (props: { children: React.ReactNode }) => props.children,
}))
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key, i18n }),
}))
vi.mock('@/lib/handle-server-error', () => ({
  handleServerError: vi.fn(),
  markServerErrorHandled: vi.fn(),
}))
vi.mock('sonner', () => ({ toast: { success: vi.fn() } }))
const clients: QueryClient[] = []
afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
})
beforeEach(() => {
  i18n.language = 'en'
  site.data.reseller_domain_prefix_enabled = true
  vi.spyOn(adminApi, 'getApplications').mockResolvedValue([])
})
function renderPage(content: React.ReactNode = <ResellerApplicationPage />) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>{content}</QueryClientProvider>
  )
}
const application: adminApi.ResellerApplication = {
  id: 3,
  applicant_user_id: 7,
  tenant_id: 47,
  business_name: 'KimiAI',
  desired_domain_prefix: 'kimi',
  contact_name: '',
  contact_email: 'owner@example.com',
  notes: '',
  status: 'approved',
  rejection_reason: '',
  created_at: '2026-10-20T00:00:00Z',
}

test.each([
  { language: 'zhCN', locale: 'zh-CN' },
  { language: 'zhTW', locale: 'zh-TW' },
])(
  'applicants see their pending application after submitting a preferred prefix ($language)',
  async ({ language, locale }) => {
    i18n.language = language
    const pendingApplication: adminApi.ResellerApplication = {
      ...application,
      tenant_id: 0,
      status: 'pending',
    }
    vi.mocked(adminApi.getApplications)
      .mockResolvedValueOnce([])
      .mockResolvedValue([pendingApplication])
    const submit = vi
      .spyOn(adminApi, 'submitApplication')
      .mockResolvedValue(pendingApplication)
    renderPage()
    const user = userEvent.setup()
    await user.type(await screen.findByLabelText('Site Name'), 'KimiAI')
    await user.type(
      screen.getByLabelText('Preferred Domain Prefix (optional)'),
      'KiMi'
    )
    expect(screen.getByText('https://kimi.kkai.win')).toBeVisible()
    await user.type(screen.getByLabelText('Contact Email'), 'owner@example.com')
    await user.click(screen.getByRole('button', { name: 'Submit Application' }))
    await waitFor(() =>
      expect(submit).toHaveBeenCalledWith(
        {
          business_name: 'KimiAI',
          desired_domain_prefix: 'kimi',
          contact_name: '',
          contact_email: 'owner@example.com',
          notes: '',
        },
        expect.anything()
      )
    )
    expect(await screen.findByText('KimiAI')).toBeVisible()
    expect(
      screen.getByText(
        getDefaultNormalizer()(
          `Pending · ${new Date(pendingApplication.created_at).toLocaleString(locale)}`
        )
      )
    ).toBeVisible()
    expect(screen.queryByLabelText('Site Name')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Submit Application' })
    ).not.toBeInTheDocument()
  }
)

test('a claimed prefix is shown beside the input while preserving the application for correction', async () => {
  vi.spyOn(adminApi, 'submitApplication').mockRejectedValue({
    code: 'RESELLER_DOMAIN_PREFIX_TAKEN',
  })
  renderPage()
  const user = userEvent.setup()
  await user.type(await screen.findByLabelText('Site Name'), 'KimiAI')
  await user.type(
    screen.getByLabelText('Preferred Domain Prefix (optional)'),
    'kimi'
  )
  await user.type(screen.getByLabelText('Contact Email'), 'owner@example.com')
  await user.click(screen.getByRole('button', { name: 'Submit Application' }))
  expect(
    await screen.findByText(
      'This domain prefix is already assigned. Choose another one.'
    )
  ).toBeVisible()
  expect(screen.getByLabelText('Site Name')).toHaveValue('KimiAI')
  expect(
    screen.getByLabelText('Preferred Domain Prefix (optional)')
  ).toHaveFocus()
})

test('an older backend keeps blank-prefix applications available without discarding a prefix', async () => {
  site.data.reseller_domain_prefix_enabled = false
  const submit = vi
    .spyOn(adminApi, 'submitApplication')
    .mockResolvedValue(application)
  renderPage()
  expect(
    await screen.findByLabelText('Preferred Domain Prefix (optional)')
  ).toBeDisabled()
  expect(
    screen.getByText(
      'Domain prefix selection is temporarily unavailable. Leave it blank to submit your application.'
    )
  ).toBeVisible()
  const user = userEvent.setup()
  await user.type(screen.getByLabelText('Site Name'), 'KimiAI')
  await user.type(screen.getByLabelText('Contact Email'), 'owner@example.com')
  await user.click(screen.getByRole('button', { name: 'Submit Application' }))
  await waitFor(() =>
    expect(submit).toHaveBeenCalledWith(
      {
        business_name: 'KimiAI',
        desired_domain_prefix: '',
        contact_name: '',
        contact_email: 'owner@example.com',
        notes: '',
      },
      expect.anything()
    )
  )
})

test.each([null, undefined])(
  'approved tenants with %s domains remain available for domain assignment',
  async (domains) => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: {
          applications: [application],
          tenants: [
            {
              id: 47,
              display_name: 'KimiAI',
              status: 'provisioning',
              ...(domains === undefined ? {} : { domains }),
            },
          ],
          root_domain: 'kkai.win',
        },
      },
    })
    const managed = await adminApi.getManagedResellers()
    renderPage(
      <AdminTenants
        tenants={managed.tenants}
        applications={managed.applications}
        rootDomain={managed.root_domain}
      />
    )
    expect(screen.getByText('Provisioning')).toBeVisible()
    expect(screen.getByLabelText('Hostname')).toHaveValue('kimi.kkai.win')
    expect(screen.getByRole('button', { name: 'Enable Site' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Assign Domain' })).toBeEnabled()
  }
)

test.each(['zhCN', 'zhTW'])(
  'administrators see the requested domain and explicitly assign the prefilled hostname (%s)',
  async (language) => {
    i18n.language = language
    const provision = vi.spyOn(adminApi, 'provisionDomain').mockResolvedValue()
    renderPage(
      <>
        <AdminApplications applications={[application]} rootDomain='kkai.win' />
        <AdminTenants
          tenants={[
            {
              id: 47,
              display_name: 'KimiAI',
              status: 'provisioning',
              domains: [],
            },
          ]}
          applications={[application]}
          rootDomain='kkai.win'
        />
      </>
    )
    expect(screen.getByText('kimi.kkai.win')).toBeVisible()
    expect(screen.getByLabelText('Hostname')).toHaveValue('kimi.kkai.win')
    expect(provision).not.toHaveBeenCalled()
    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: 'Assign Domain' }))
    await waitFor(() =>
      expect(provision).toHaveBeenCalledExactlyOnceWith(47, 'kimi.kkai.win')
    )
  }
)
