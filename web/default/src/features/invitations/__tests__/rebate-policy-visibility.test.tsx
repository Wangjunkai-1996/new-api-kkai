import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, onTestFinished, test, vi } from 'vitest'

import { KkaiWalletRewardsCard } from '@/features/kkai-wallet/invitation-rewards-card'
import { api } from '@/lib/api'
import { Route as InvitationsRoute } from '@/routes/_authenticated/invitations/index'
import { useAuthStore, type AuthUser } from '@/stores/auth-store'

import { Invitations } from '../index'
import type { InvitationFeatureStatus } from '../types'

vi.mock('qrcode.react', () => ({
  QRCodeSVG: ({ value }: { value: string }) => (
    <svg role='img' aria-label={value} />
  ),
}))

const accountA = { id: 21, username: 'qa-a', role: 1, group: 'qa-main' }
const accountB = { id: 22, username: 'qa-b', role: 1, group: 'qa-other' }
const seller = { id: 23, username: 'qa-s', role: 1, group: 'qa-main' }
const base = '/invitations/api/invitations'
const rate = (percent: string) => `Rebate on a friend's top-up: ${percent}`
const basis = "Based on the credits actually added to your friend's account."
const disabled = 'Order rebates are not enabled.'
const unavailable = 'The top-up rebate rate is temporarily unavailable.'
const available = (ratio: unknown) => ({ state: 'available', ratio })
const ok = (data: unknown) => ({ data: { success: true, data } })
const failure = {
  data: { success: false, message: 'Invitation service unavailable' },
}
type Reply = ReturnType<typeof ok> | typeof failure
const emptyPage = { items: [], total: 0, page: 1, pageSize: 10 }

function summary(invitationCode: string, policy?: unknown) {
  return ok({
    invitationCode,
    invitedCount: 0,
    confirmingRebate: 0,
    pendingRebate: 0,
    completedRebate: 0,
    totalRebate: 0,
    ...(policy === undefined ? {} : { topupRebatePolicy: policy }),
  })
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}

function mockInvitations() {
  const status: Partial<InvitationFeatureStatus> = {
    available: true,
    userInvitationRebateEnabled: true,
    orderRebateEnabled: true,
    invitationSignupRewardEnabled: false,
    rebateToBalanceEnabled: true,
    settlementEnabled: true,
  }
  const profile = { registered: false, active: false }
  const myCode = vi
    .fn<() => Promise<Reply>>()
    .mockResolvedValue(summary('QA-A', available(0.075)))
  const records = vi.fn<() => Promise<Reply>>().mockResolvedValue(ok(emptyPage))
  const responses: Record<string, () => Reply | Promise<Reply>> = {
    [`${base}/status`]: () => ok({ ...status }),
    [`${base}/sales-profile`]: () => ok({ ...profile }),
    [`${base}/my-code`]: myCode,
    [`${base}/rebate-records`]: records,
    [`${base}/available-rebates`]: () =>
      ok({ amount: 500, recordIds: [1], creditCurrency: 'usd_credit_v1' }),
    [`${base}/rebate-requests`]: () => ok(emptyPage),
    [`${base}/sales-customers`]: () =>
      ok({
        ...emptyPage,
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
      }),
  }
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (!responses[url]) throw new Error(`Unexpected GET ${url}`)
    return responses[url]()
  })
  const post = vi
    .spyOn(api, 'post')
    .mockRejectedValue(new Error('Unexpected write'))
  return { status, profile, myCode, records, get, post }
}

async function visit(
  account: AuthUser | null = accountA,
  path = '/invitations'
) {
  useAuthStore.getState().auth.setUser(account)
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  const user = userEvent.setup()
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const page = createRoute({
    getParentRoute: () => auth,
    path: 'invitations/',
    validateSearch: InvitationsRoute.options.validateSearch,
    component: Invitations,
  })
  const wallet = createRoute({
    getParentRoute: () => auth,
    path: 'wallet',
    component: () => (
      <KkaiWalletRewardsCard
        user={null}
        affiliateLink='/test-referral'
        onTransfer={vi.fn()}
      />
    ),
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([page, wallet])]),
    history: createMemoryHistory({ initialEntries: [path] }),
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 10_000 } },
  })
  await router.load()
  const view = render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  onTestFinished(() => {
    view.unmount()
    client.clear()
    useAuthStore.getState().auth.reset()
  })
  return { user, router }
}

test('shows the ordinary rate before the tabs and keeps all sharing available on a policy-only failure', async () => {
  const server = mockInvitations()
  const code = 'QA-A+/='
  const link = `${window.location.origin}/sign-up?aff=QA-A%2B%2F%3D`
  server.myCode.mockResolvedValue(summary(code, available(0.075)))
  const { user } = await visit()
  await screen.findByText(code)
  const policy = screen.getByText(rate('7.5%'))
  expect(
    screen.getByRole('heading', { name: 'Invitation Rebate' })
  ).toAppearBefore(policy)
  expect(policy).toAppearBefore(screen.getByRole('tablist'))
  expect(screen.getByText(basis)).toBeVisible()
  const panel = within(screen.getByRole('tabpanel', { name: 'My Invitation' }))
  expect(panel.getByText('Invited Users')).toBeVisible()
  expect(panel.getByText('0')).toBeVisible()
  expect(panel.getAllByText(/^\$0(?:\.0+)?$/)).toHaveLength(4)
  await user.click(screen.getByText('Other reward rules'))
  expect(
    screen.getByText(
      'For subscriptions and redemptions, see each record for any rebate and its rate.'
    )
  ).toBeVisible()
  expect(server.myCode).toHaveBeenCalledTimes(1)
  expect(server.records).not.toHaveBeenCalled()

  server.myCode.mockResolvedValue(summary(code, { state: 'unavailable' }))
  await user.click(screen.getByRole('button', { name: 'Refresh' }))
  expect(await screen.findByText(unavailable)).toBeVisible()
  expect(screen.queryByText(rate('7.5%'))).not.toBeInTheDocument()
  expect(panel.getByDisplayValue(link)).toBeVisible()
  await user.click(panel.getByRole('button', { name: 'Copy invitation code' }))
  expect(await navigator.clipboard.readText()).toBe(code)
  await user.click(panel.getByRole('button', { name: 'Copy invitation link' }))
  expect(await navigator.clipboard.readText()).toBe(link)
  await user.click(
    panel.getByRole('button', { name: 'Show invitation QR code' })
  )
  const dialog = within(
    await screen.findByRole('dialog', { name: 'Invitation QR Code' })
  )
  expect(dialog.getByRole('img', { name: link })).toBeVisible()
  await user.keyboard('{Escape}')
  expect(server.myCode).toHaveBeenCalledTimes(2)

  server.myCode.mockResolvedValue(summary(code, available(0.03)))
  server.get.mockClear()
  await user.click(screen.getByRole('button', { name: 'Retry' }))
  expect(await screen.findByText(rate('3%'))).toBeVisible()
  expect(server.get.mock.calls.map(([url]) => url)).toEqual([`${base}/my-code`])
  expect(server.post).not.toHaveBeenCalled()
})

test('keeps the rate and totals visible with an empty invitation code and no usable share actions', async () => {
  const server = mockInvitations()
  server.myCode.mockResolvedValue(
    ok({
      invitationCode: '',
      invitedCount: 4,
      confirmingRebate: 0,
      pendingRebate: 125,
      completedRebate: 250,
      totalRebate: 375,
      topupRebatePolicy: available(0.075),
    })
  )
  await visit()
  expect(await screen.findByText(rate('7.5%'))).toBeVisible()
  const panel = within(screen.getByRole('tabpanel', { name: 'My Invitation' }))
  expect(panel.getByText('Invited Users')).toBeVisible()
  expect(panel.getByText('4')).toBeVisible()
  expect(panel.getByText('Total Rebate')).toBeVisible()
  expect(panel.getByText('$3.75')).toBeVisible()
  expect(panel.queryByDisplayValue(/\/sign-up\?aff=/)).not.toBeInTheDocument()
  for (const action of panel.queryAllByRole('button', {
    name: /Copy invitation (code|link)|Show invitation QR code/,
  })) {
    expect(action).toBeDisabled()
  }
})

test('shows the server-selected rate on the sales landing and preserves customer and request actions on refresh', async () => {
  const server = mockInvitations()
  server.status.userInvitationRebateEnabled = false
  Object.assign(server.profile, { registered: true, active: true })
  server.myCode.mockResolvedValue(summary('QA-S', available(0.15)))
  const { user } = await visit(seller)
  await screen.findByText('No invited customers')
  expect(
    screen.getByRole('tab', { name: 'Invited Customers', selected: true })
  ).toBeVisible()
  expect(await screen.findByText(rate('15%'))).toAppearBefore(
    screen.getByRole('tablist')
  )
  expect(screen.getByText(basis)).toBeVisible()
  expect(server.records).not.toHaveBeenCalled()
  expect(server.myCode).toHaveBeenCalledTimes(1)

  server.myCode.mockResolvedValue(summary('QA-S', { state: 'unavailable' }))
  await user.click(screen.getByRole('button', { name: 'Refresh' }))
  await screen.findByText(unavailable)
  expect(
    screen.getByRole('tabpanel', { name: 'Invited Customers' })
  ).toBeVisible()
  expect(
    server.get.mock.calls.filter(([url]) => url === `${base}/sales-customers`)
  ).toHaveLength(2)
  await user.click(screen.getByRole('tab', { name: 'My Invitation' }))
  await screen.findByText('QA-S')
  await user.click(screen.getByRole('tab', { name: 'Rebate Requests' }))
  await screen.findByText('No rebate requests')
  expect(
    screen.getByRole('button', { name: 'Request Rebate Review' })
  ).toBeEnabled()
  expect(server.myCode).toHaveBeenCalledTimes(2)

  server.get.mockClear()
  server.myCode.mockResolvedValue(summary('QA-S', available(0.075)))
  await user.click(screen.getByRole('button', { name: 'Refresh' }))
  await screen.findByText(rate('7.5%'))
  expect(
    screen.getByRole('tab', { name: 'Rebate Requests', selected: true })
  ).toBeVisible()
  expect(server.get.mock.calls.map(([url]) => url)).toEqual(
    expect.arrayContaining([
      `${base}/available-rebates`,
      `${base}/rebate-requests`,
    ])
  )
  expect(server.myCode).toHaveBeenCalledTimes(3)
  server.profile.active = false
  await user.click(screen.getByRole('button', { name: 'Refresh' }))
  await screen.findByText(
    'Sales access is inactive. Existing requests remain available.'
  )
  expect(
    screen.queryByRole('button', { name: 'Request Rebate Review' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('tab', { name: 'Transfer to Balance' })
  ).not.toBeInTheDocument()
  expect(server.post).not.toHaveBeenCalled()
})

test.each([
  ['explicit zero', true, available(0), rate('0%')],
  ['small fraction', true, available(0.0001), rate('0.01%')],
  ['full fraction', true, available(1), rate('100%')],
  [
    'unmatched rule',
    true,
    { state: 'not_configured', ratio: 0.15 },
    'No top-up rebate rule currently applies.',
  ],
  ['personal disable', true, { state: 'disabled', ratio: 0.15 }, disabled],
  ['disabled old service', false, undefined, disabled],
  ['disabled with a personal rate', false, available(0.15), disabled],
  ['unknown public switch', undefined, available(0.15), unavailable],
  ['enabled old service', true, undefined, unavailable],
  ['unknown state', true, { state: 'future', ratio: 0.15 }, unavailable],
  ['missing rate', true, { state: 'available' }, unavailable],
  ['null rate', true, available(null), unavailable],
  ['numeric string', true, available('0.15'), unavailable],
  ['negative fraction', true, available(-0.01), unavailable],
  ['oversized fraction', true, available(1.01), unavailable],
  ['NaN', true, available(Number.NaN), unavailable],
  ['Infinity', true, available(Number.POSITIVE_INFINITY), unavailable],
] as const)(
  'replaces the previous rate on refresh for %s',
  async (_label, enabled, policy, text) => {
    const server = mockInvitations()
    const { user } = await visit()
    await screen.findByText('QA-A')
    expect(screen.getByText(rate('7.5%'))).toBeVisible()
    server.status.orderRebateEnabled = enabled
    server.myCode.mockResolvedValue(summary('QA-A', policy))
    await user.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(await screen.findByText(text)).toBeVisible()
    expect(screen.queryByText(rate('7.5%'))).not.toBeInTheDocument()
    expect(screen.getByText('QA-A')).toBeVisible()
    if (!text.includes('%')) {
      expect(screen.queryByText(/\d\s*%/)).not.toBeInTheDocument()
    }
    if (text === unavailable) {
      expect(screen.getByRole('button', { name: 'Retry' })).toBeEnabled()
    }
  }
)

test('withdraws cached data after a whole my-code failure and restores sharing only on a successful retry', async () => {
  const server = mockInvitations()
  const { user } = await visit()
  await screen.findByText('QA-A')
  expect(screen.getByText(rate('7.5%'))).toBeVisible()
  server.myCode.mockRejectedValueOnce(
    new Error('Invitation service unavailable')
  )
  await user.click(screen.getByRole('button', { name: 'Refresh' }))
  await screen.findByText('Failed to load invitation code')
  expect(screen.getByText(unavailable)).toBeVisible()
  expect(screen.queryByText(rate('7.5%'))).not.toBeInTheDocument()
  expect(screen.queryByText('QA-A')).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Copy invitation code' })
  ).not.toBeInTheDocument()

  const retry = deferred<Reply>()
  server.myCode.mockReturnValueOnce(retry.promise)
  await user.click(
    within(screen.getByRole('tabpanel', { name: 'My Invitation' })).getByRole(
      'button',
      { name: 'Retry' }
    )
  )
  expect(server.myCode).toHaveBeenCalledTimes(3)
  expect(screen.queryByText(rate('7.5%'))).not.toBeInTheDocument()
  expect(screen.queryByDisplayValue(/aff=QA-A/)).not.toBeInTheDocument()
  await act(async () => retry.resolve(summary('QA-A-new', available(0.03))))
  expect(await screen.findByText('QA-A-new')).toBeVisible()
  expect(screen.getByText(rate('3%'))).toBeVisible()
  expect(
    screen.queryByText('Failed to load invitation code')
  ).not.toBeInTheDocument()
})

test('removes the open QR on logout and waits for the new account before offering to share', async () => {
  const server = mockInvitations()
  const { user } = await visit()
  await user.click(
    await screen.findByRole('button', { name: 'Show invitation QR code' })
  )
  expect(
    within(await screen.findByRole('dialog')).getByRole('img', {
      name: /aff=QA-A$/,
    })
  ).toBeVisible()
  const next = deferred<Reply>()
  server.myCode.mockReturnValue(next.promise)
  await act(async () => useAuthStore.getState().auth.setUser(null))
  expect(
    screen.queryByRole('img', { name: /aff=QA-A$/ })
  ).not.toBeInTheDocument()
  expect(screen.queryByDisplayValue(/aff=QA-A$/)).not.toBeInTheDocument()
  expect(server.myCode).toHaveBeenCalledTimes(1)
  await act(async () => useAuthStore.getState().auth.setUser(accountB))
  await waitFor(() => expect(server.myCode).toHaveBeenCalledTimes(2))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Copy invitation link' })
  ).not.toBeInTheDocument()
  expect(screen.queryByText(/\d\s*%/)).not.toBeInTheDocument()
  await act(async () => next.resolve(summary('QA-B', available(0.03))))
  await screen.findByText('QA-B')
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Copy invitation link' }))
  expect(await navigator.clipboard.readText()).toBe(
    `${window.location.origin}/sign-up?aff=QA-B`
  )
})

test('keeps B’s rate and same-page records when A’s in-flight refresh arrives last', async () => {
  const server = mockInvitations()
  const lateA = deferred<void>()
  const pendingB = deferred<void>()
  let refreshing = false
  const replyForAccount = async (first: Reply, second: Reply) => {
    const id = useAuthStore.getState().auth.user?.id
    if (id === accountB.id) {
      await pendingB.promise
      return second
    }
    if (id !== accountA.id) throw new Error('No invitation account')
    if (refreshing) await lateA.promise
    return first
  }
  const recordPage = (inviterId: number, orderAmount: number) =>
    ok({
      ...emptyPage,
      total: 1,
      items: [
        {
          id: 1,
          inviterId,
          orderType: 'topup',
          orderAmount,
          rebateAmount: 50,
          rebateRatio: 0.05,
          status: 'completed',
          unlockRequired: false,
          createdAt: '2026-01-01',
          updatedAt: '2026-01-01',
        },
      ],
    })
  server.myCode.mockImplementation(() =>
    replyForAccount(
      summary('QA-A', available(0.075)),
      summary('QA-B', available(0.03))
    )
  )
  server.records.mockImplementation(() =>
    replyForAccount(
      recordPage(accountA.id, 1111),
      recordPage(accountB.id, 2222)
    )
  )
  const { user } = await visit(accountA, '/invitations?tab=records')
  await screen.findByRole('cell', { name: '$11.11' })
  expect(screen.getByText(rate('7.5%'))).toBeVisible()
  refreshing = true
  await user.click(screen.getByRole('button', { name: 'Refresh' }))
  expect(server.records).toHaveBeenCalledTimes(2)
  expect(server.myCode).toHaveBeenCalledTimes(2)
  expect(screen.queryByText(rate('7.5%'))).not.toBeInTheDocument()
  await act(async () => useAuthStore.getState().auth.setUser(accountB))
  await waitFor(() => expect(server.records).toHaveBeenCalledTimes(3))
  expect(server.myCode).toHaveBeenCalledTimes(3)
  expect(
    screen.getByRole('tab', { name: 'Rebate Records', selected: true })
  ).toBeVisible()
  expect(screen.queryAllByText('$11.11')).toHaveLength(0)
  expect(screen.queryByText(rate('7.5%'))).not.toBeInTheDocument()
  await act(async () => pendingB.resolve())
  await screen.findByRole('cell', { name: '$22.22' })
  expect(screen.getByText(rate('3%'))).toBeVisible()
  await act(async () => lateA.resolve())
  expect(screen.getByRole('cell', { name: '$22.22' })).toBeVisible()
  expect(screen.queryAllByText('$11.11')).toHaveLength(0)
  await user.click(screen.getByRole('tab', { name: 'My Invitation' }))
  expect(await screen.findByText('QA-B')).toBeVisible()
  expect(screen.getByText(rate('3%'))).toBeVisible()
  expect(screen.queryByDisplayValue(/aff=QA-A$/)).not.toBeInTheDocument()
})

test('isolates the wallet fallback from the invitation page’s strict error and retry for the same account', async () => {
  const server = mockInvitations()
  server.myCode.mockResolvedValue(failure)
  const { router, user } = await visit(accountA, '/wallet')
  await screen.findByRole('heading', { name: 'Referral Program' })
  await act(async () => {
    await router.navigate({ to: '/invitations' })
  })
  await screen.findByText('Failed to load invitation code')
  expect(server.myCode).toHaveBeenCalledTimes(2)
  server.myCode.mockResolvedValue(summary('QA-A', available(0.075)))
  await user.click(
    within(screen.getByRole('tabpanel', { name: 'My Invitation' })).getByRole(
      'button',
      { name: 'Retry' }
    )
  )
  await screen.findByText('QA-A')
  await act(async () => {
    await router.navigate({ to: '/wallet' })
  })
  expect(
    await screen.findByRole('heading', { name: 'Referral Program' })
  ).toBeVisible()
  expect(screen.queryByDisplayValue(/aff=QA-A$/)).not.toBeInTheDocument()
})
