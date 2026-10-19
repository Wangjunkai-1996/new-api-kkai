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
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ModelPriceCell } from '@/features/pricing/components/model-price-cell'
import { submitPaymentForm } from '@/features/wallet/lib/payment'

import { MembershipCenter } from '.'
import {
  createMembershipOrder,
  getMembershipCatalog,
  getMembershipOrders,
  getMembershipSelf,
  publishMembershipCatalog,
  resolveMembershipBillingReview,
  resolveMembershipOrder,
  resumeMembershipPayment,
} from './api'
import { BillingReviewDialog } from './components/billing-reviews'
import { MembershipCatalogEditor } from './components/catalog-editor'
import { CurrentMembership } from './components/current-membership'
import { MembershipDiscounts } from './components/discount-comparison'
import { MembershipOrders } from './components/orders'
import { MembershipPurchaseDialog } from './components/purchase-dialog'
import { ResolveMembershipOrderDialog } from './components/resolve-order-dialog'
import { MembershipTierCard } from './components/tier-card'
import { membershipCatalogSchema } from './form'
import {
  findMembershipDiscountConflicts,
  formatMembershipPrice,
  normalizeMembershipCatalogForSubmission,
} from './lib'
import type {
  MembershipCatalog,
  MembershipOrder,
  MembershipSelf,
  MembershipTier,
} from './types'

vi.mock('./api', () => ({
  createMembershipOrder: vi.fn(),
  getMembershipCatalog: vi.fn(),
  getMembershipOrders: vi.fn(),
  getMembershipSelf: vi.fn(),
  publishMembershipCatalog: vi.fn(),
  resumeMembershipPayment: vi.fn(),
  resolveMembershipOrder: vi.fn(),
  getMembershipBillingReviews: vi.fn(),
  resolveMembershipBillingReview: vi.fn(),
}))
vi.mock('@/lib/handle-server-error', () => ({ handleServerError: vi.fn() }))

const tier: MembershipTier = {
  id: 'level-a',
  name: 'Gold',
  rank: 1,
  style: 'gold',
  price_minor: 9900,
  currency: 'USD',
  quota: 1_000_000,
  enabled: true,
  discounts: { enterprise: 0.9 },
  eligibility: { action: 'purchase' },
  payment_methods: [{ provider: 'stripe', method: 'card', name: 'Card' }],
}
const catalog: MembershipCatalog = {
  release_id: 3,
  sales_enabled: true,
  tiers: [
    tier,
    {
      ...tier,
      id: 'level-b',
      name: 'Diamond',
      rank: 2,
      style: 'diamond',
      discounts: { enterprise: 0.8 },
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
const membership: MembershipSelf = {
  server_time: 1000,
  account: {
    id: 20,
    user_id: 9,
    tier_id: 'level-a',
    rank: 1,
    release_id: 1,
    amount_total: 1000000,
    amount_used: 600000,
    amount_reserved: 400000,
    expires_at: 2000,
    revision: 4,
    status: 'active',
  },
  tier,
  discounts: [
    {
      group_key: 'enterprise',
      display_name: 'Enterprise',
      coefficient: 0.95,
      membership_ratio: 0.15,
      wallet_ratio: 0.15,
      enabled: true,
      published: true,
    },
  ],
  pending_order: null,
}

beforeEach(() => {
  vi.mocked(createMembershipOrder).mockReset()
  vi.mocked(getMembershipCatalog).mockReset()
  vi.mocked(getMembershipOrders).mockReset()
  vi.mocked(getMembershipSelf).mockReset()
  vi.mocked(publishMembershipCatalog).mockReset()
  vi.mocked(resolveMembershipOrder).mockReset()
  vi.mocked(resumeMembershipPayment).mockReset()
})

describe('membership purchase and benefits', () => {
  it('uses server eligibility rather than inferring rank from the tier name', async () => {
    const onPurchase = vi.fn()
    const { rerender } = render(
      <MembershipTierCard
        tier={{ ...tier, eligibility: { action: 'current' } }}
        salesEnabled
        onPurchase={onPurchase}
      />
    )
    expect(
      screen.getByRole('button', { name: 'Current membership' })
    ).toBeDisabled()
    rerender(
      <MembershipTierCard
        tier={{
          ...tier,
          name: 'Custom level',
          eligibility: { action: 'upgrade' },
        }}
        salesEnabled
        onPurchase={onPurchase}
      />
    )
    await userEvent.click(
      screen.getByRole('button', { name: 'Upgrade membership' })
    )
    expect(onPurchase).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'level-a' })
    )
    rerender(
      <MembershipTierCard
        tier={tier}
        salesEnabled={false}
        onPurchase={onPurchase}
      />
    )
    expect(
      screen.getByRole('button', { name: 'Currently unavailable' })
    ).toBeDisabled()
  })

  it('does not offer an unsupported payment channel', () => {
    render(
      <MembershipTierCard
        tier={{ ...tier, payment_methods: [] }}
        salesEnabled
        onPurchase={vi.fn()}
      />
    )
    expect(
      screen.getByRole('button', { name: 'Currently unavailable' })
    ).toBeDisabled()
  })

  it('keeps membership active while all remaining quota is reserved', () => {
    render(<CurrentMembership membership={membership} now={1000} />)
    expect(screen.getByText('Active')).toBeVisible()
    expect(screen.getByText('Reserved by requests')).toBeVisible()
    expect(screen.queryByText('Quota exhausted')).not.toBeInTheDocument()
    expect(
      screen.getByText(
        'Reserved quota is temporarily unavailable. It does not end your membership.'
      )
    ).toBeVisible()
  })

  it('shows an expired membership even when the last fetched status was active', () => {
    render(<CurrentMembership membership={membership} now={2000} />)
    expect(screen.getByText('Expired')).toBeVisible()
    expect(screen.queryByText('Active')).not.toBeInTheDocument()
  })

  it('keeps purchased coefficients separate from current offers and displays the server price', () => {
    render(
      <MembershipDiscounts
        catalog={catalog}
        lockedDiscounts={membership.discounts}
        active
      />
    )
    expect(screen.getByText('95%')).toBeVisible()
    expect(screen.getByText('90%')).toBeVisible()
    expect(screen.getByText('80%')).toBeVisible()
    expect(screen.getByText(/Current billing ratio.*0.15/)).toBeVisible()
    fireEvent.change(screen.getByRole('textbox', { name: 'Search groups' }), {
      target: { value: 'missing' },
    })
    expect(screen.getByText('No matching groups')).toBeVisible()
  })

  it('submits the shown release and account revision and keeps a failed payment unactivated', async () => {
    vi.mocked(createMembershipOrder).mockRejectedValue(
      new Error('Membership changed')
    )
    const onCreated = vi.fn()
    const client = new QueryClient({
      defaultOptions: {
        mutations: { retry: false },
        queries: { retry: false },
      },
    })
    render(
      <QueryClientProvider client={client}>
        <MembershipPurchaseDialog
          tier={{ ...tier, id: 'level-b', eligibility: { action: 'upgrade' } }}
          membership={membership}
          releaseId={3}
          salesEnabled
          onClose={vi.fn()}
          onOrderCreated={onCreated}
        />
      </QueryClientProvider>
    )
    const dialog = screen.getByRole('dialog')
    expect(
      within(dialog).queryByText('Pay with Balance')
    ).not.toBeInTheDocument()
    expect(within(dialog).getByText('Estimated carried quota')).toBeVisible()
    await userEvent.click(
      within(dialog).getByRole('button', { name: 'Continue to payment' })
    )
    expect(createMembershipOrder).toHaveBeenCalledWith(
      {
        tier_id: 'level-b',
        payment_provider: 'stripe',
        payment_method: 'card',
        release_id: 3,
        expected_account_id: 20,
        expected_revision: 4,
      },
      expect.anything()
    )
    expect(
      await screen.findByText(
        'Payment could not be started. Refresh your membership before trying again.'
      )
    ).toBeVisible()
    expect(onCreated).not.toHaveBeenCalled()
  })
})

describe('membership publication validation', () => {
  it('adds tiers to an empty catalog and preserves edited values when either tier is removed', async () => {
    const emptyCatalog = {
      ...catalog,
      release_id: 0,
      sales_enabled: false,
      tiers: [],
    }
    vi.mocked(publishMembershipCatalog).mockResolvedValue(emptyCatalog)
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MembershipCatalogEditor catalog={emptyCatalog} />
      </QueryClientProvider>
    )
    await userEvent.click(
      screen.getByRole('button', { name: 'Add membership tier' })
    )
    expect(screen.getByRole('spinbutton', { name: 'Price' })).toHaveValue(0)
    await userEvent.type(
      screen.getByRole('textbox', { name: 'Plan Title' }),
      'Gold'
    )
    expect(
      screen.queryByRole('textbox', { name: 'Tier identifier' })
    ).not.toBeInTheDocument()
    expect(screen.getByLabelText('Creem one-time product ID')).not.toBeVisible()
    expect(
      screen.getByLabelText('Pancake one-time product ID')
    ).not.toBeVisible()
    fireEvent.change(screen.getByRole('spinbutton', { name: 'Price' }), {
      target: { value: '600' },
    })
    fireEvent.change(
      screen.getByRole('spinbutton', { name: /^Included quota/ }),
      { target: { value: '600' } }
    )
    fireEvent.change(screen.getByRole('spinbutton', { name: 'Gold' }), {
      target: { value: '0.95' },
    })

    await userEvent.click(
      screen.getByRole('button', { name: 'Add membership tier' })
    )
    expect(screen.getAllByRole('spinbutton', { name: 'Price' })[1]).toHaveValue(
      0
    )
    await userEvent.type(
      screen.getAllByRole('textbox', { name: 'Plan Title' })[1],
      'Discarded'
    )
    await userEvent.click(screen.getAllByRole('button', { name: 'Remove' })[1])
    expect(screen.getByRole('textbox', { name: 'Plan Title' })).toHaveValue(
      'Gold'
    )
    expect(screen.getByRole('spinbutton', { name: 'Price' })).toHaveValue(600)
    expect(
      screen.getByRole('spinbutton', { name: /^Included quota/ })
    ).toHaveValue(600)
    expect(screen.getByRole('spinbutton', { name: 'Gold' })).toHaveValue(0.95)

    await userEvent.click(
      screen.getByRole('button', { name: 'Add membership tier' })
    )
    expect(
      screen.getAllByRole('textbox', { name: 'Plan Title' })[1]
    ).toHaveValue('')
    expect(
      screen.getAllByRole('spinbutton', { name: 'Membership rank' })[1]
    ).toHaveValue(2)
    await userEvent.type(
      screen.getAllByRole('textbox', { name: 'Plan Title' })[1],
      'Diamond'
    )
    fireEvent.change(screen.getAllByRole('spinbutton', { name: 'Price' })[1], {
      target: { value: '2000' },
    })
    fireEvent.change(
      screen.getAllByRole('spinbutton', { name: /^Included quota/ })[1],
      { target: { value: '2000' } }
    )
    fireEvent.change(screen.getByRole('spinbutton', { name: 'Diamond' }), {
      target: { value: '0.9' },
    })
    await userEvent.click(screen.getAllByRole('button', { name: 'Remove' })[0])
    expect(screen.getByRole('textbox', { name: 'Plan Title' })).toHaveValue(
      'Diamond'
    )
    expect(screen.getByRole('spinbutton', { name: 'Price' })).toHaveValue(2000)
    expect(
      screen.getByRole('spinbutton', { name: /^Included quota/ })
    ).toHaveValue(2000)
    expect(screen.getByRole('spinbutton', { name: 'Diamond' })).toHaveValue(0.9)

    await userEvent.click(
      screen.getByRole('button', { name: 'Publish membership configuration' })
    )
    await userEvent.click(
      await screen.findByRole('button', { name: 'Confirm publication' })
    )
    await waitFor(() => expect(publishMembershipCatalog).toHaveBeenCalledOnce())
    expect(publishMembershipCatalog).toHaveBeenCalledWith(
      expect.objectContaining({
        sales_enabled: false,
        tiers: [
          expect.objectContaining({
            id: expect.stringMatching(/^[a-zA-Z0-9_-]{1,64}$/),
            name: 'Diamond',
            rank: 2,
            price_minor: 200000,
            quota: 1_000_000_000,
            discounts: { enterprise: 0.9 },
          }),
        ],
      }),
      0
    )
  })

  it('preserves existing identifiers and payment products when editing a tier name', async () => {
    const existingCatalog = {
      ...catalog,
      tiers: [{ ...tier, payment_products: { creem: 'prod_existing' } }],
    }
    vi.mocked(publishMembershipCatalog).mockResolvedValue(existingCatalog)
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MembershipCatalogEditor catalog={existingCatalog} />
      </QueryClientProvider>
    )
    await userEvent.clear(screen.getByRole('textbox', { name: 'Plan Title' }))
    await userEvent.type(
      screen.getByRole('textbox', { name: 'Plan Title' }),
      'Renamed Gold'
    )
    await userEvent.click(
      screen.getByRole('button', { name: 'Publish membership configuration' })
    )
    await userEvent.click(
      await screen.findByRole('button', { name: 'Confirm publication' })
    )
    await waitFor(() => expect(publishMembershipCatalog).toHaveBeenCalledOnce())
    expect(publishMembershipCatalog).toHaveBeenCalledWith(
      expect.objectContaining({
        tiers: [
          expect.objectContaining({
            id: tier.id,
            name: 'Renamed Gold',
            payment_products: expect.objectContaining({
              creem: 'prod_existing',
            }),
          }),
        ],
      }),
      catalog.release_id
    )
  })

  it('rejects a higher-tier coefficient above the lower-tier coefficient for the same group', () => {
    const invalid = {
      ...catalog,
      tiers: [tier, { ...catalog.tiers[1], discounts: { enterprise: 0.96 } }],
    }
    expect(findMembershipDiscountConflicts(invalid)).toEqual([
      { group: 'enterprise', tier: 'level-b' },
    ])
    expect(membershipCatalogSchema(i18next.t).safeParse(invalid).success).toBe(
      false
    )
    expect(membershipCatalogSchema(i18next.t).safeParse(catalog).success).toBe(
      true
    )
  })

  it('treats omitted published coefficients as 1 and allows an equal higher-tier coefficient', () => {
    expect(
      findMembershipDiscountConflicts({
        ...catalog,
        tiers: [tier, { ...catalog.tiers[1], discounts: {} }],
      })
    ).toEqual([{ group: 'enterprise', tier: 'level-b' }])
    expect(
      findMembershipDiscountConflicts({
        ...catalog,
        tiers: [tier, { ...catalog.tiers[1], discounts: { enterprise: 0.9 } }],
      })
    ).toEqual([])
  })

  it('publishes every group and fills missing coefficients when submitting', () => {
    const unpublishedConflict = {
      ...catalog,
      groups: [{ ...catalog.groups[0], published: false }],
      tiers: [tier, { ...catalog.tiers[1], discounts: { enterprise: 0.96 } }],
    }
    expect(findMembershipDiscountConflicts(unpublishedConflict)).toEqual([
      { group: 'enterprise', tier: 'level-b' },
    ])

    const normalized = normalizeMembershipCatalogForSubmission({
      ...catalog,
      groups: [{ ...catalog.groups[0], published: false }],
      tiers: catalog.tiers.map((item) => ({ ...item, discounts: {} })),
    })
    expect(normalized.groups[0].published).toBe(true)
    expect(normalized.tiers[0].discounts).toEqual({ enterprise: 1 })
    expect(normalized.tiers[1].discounts).toEqual({ enterprise: 1 })
  })

  it('prevents selling incomplete parameters and duplicate tier identities', () => {
    const incomplete = {
      ...catalog,
      tiers: [{ ...tier, quota: 0, price_minor: 0 }],
    }
    expect(
      membershipCatalogSchema(i18next.t).safeParse(incomplete).success
    ).toBe(false)
    expect(
      membershipCatalogSchema(i18next.t).safeParse({
        ...incomplete,
        sales_enabled: false,
      }).success
    ).toBe(true)
    expect(
      membershipCatalogSchema(i18next.t).safeParse({
        ...catalog,
        tiers: [tier, tier],
      }).success
    ).toBe(false)
  })

  it('accepts the 5000-unit tier quota at the default 500000 quota-per-unit scale', () => {
    expect(
      membershipCatalogSchema(i18next.t).safeParse({
        ...catalog,
        tiers: [
          { ...tier, price_minor: 500000, quota: 2_500_000_000 },
          catalog.tiers[1],
        ],
      }).success
    ).toBe(true)
  })
})

describe('membership payment safety', () => {
  it('submits Epay checkout once as a same-tab POST with the frozen payment fields', async () => {
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(
      'Mozilla/5.0 Chrome/130 Safari/537.36'
    )
    const fields = {
      out_trade_no: 'MEMBER-9-epay',
      money: '99.00',
      type: 'alipay',
      sign: 'gateway-signature',
    }
    const submit = vi
      .spyOn(HTMLFormElement.prototype, 'submit')
      .mockImplementation(function (this: HTMLFormElement) {
        expect(this.isConnected).toBe(true)
        expect(this.action).toBe('https://pay.example.test/submit.php')
        expect(this.method).toBe('post')
        expect(this.target).toBe('_self')
        expect(Object.fromEntries(new FormData(this))).toEqual(fields)
        expect(
          [...this.elements].every(
            (input) => input.getAttribute('type') === 'hidden'
          )
        ).toBe(true)
      })
    const order: MembershipOrder = {
      id: 1,
      trade_no: fields.out_trade_no,
      tier_id: tier.id,
      tier_name: tier.name,
      status: 'pending',
      price_minor: 9900,
      currency: 'CNY',
      quota: tier.quota,
      created_at: 1000,
      checkout_state: 'ready',
    }
    vi.mocked(createMembershipOrder).mockResolvedValue({
      order,
      trade_no: order.trade_no,
      status: order.status,
      submit_url: 'https://pay.example.test/submit.php',
      form_fields: fields,
    })
    const onClose = vi.fn()
    const onCreated = vi.fn()
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MembershipPurchaseDialog
          tier={{
            ...tier,
            currency: 'CNY',
            payment_methods: [
              { provider: 'epay', method: 'alipay', name: 'Alipay' },
            ],
          }}
          membership={{
            ...membership,
            account: null,
            tier: null,
            discounts: [],
          }}
          releaseId={3}
          salesEnabled
          onClose={onClose}
          onOrderCreated={onCreated}
        />
      </QueryClientProvider>
    )
    await userEvent.dblClick(
      screen.getByRole('button', { name: 'Continue to payment' })
    )
    await waitFor(() => expect(submit).toHaveBeenCalledTimes(1))
    expect(createMembershipOrder).toHaveBeenCalledTimes(1)
    expect(createMembershipOrder).toHaveBeenCalledWith(
      {
        tier_id: tier.id,
        payment_provider: 'epay',
        payment_method: 'alipay',
        release_id: 3,
        expected_account_id: 0,
        expected_revision: 0,
      },
      expect.anything()
    )
    expect(onCreated).toHaveBeenCalledWith(
      expect.objectContaining({ status: 'pending' })
    )
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(screen.queryByText('Membership activated')).not.toBeInTheDocument()
    expect(
      document.querySelector(
        'form[action="https://pay.example.test/submit.php"]'
      )
    ).toBeNull()
    expect(
      screen.getByRole('button', { name: 'Continue to payment' })
    ).toBeDisabled()
  })

  it.each([
    ['Mozilla/5.0 Chrome/130 Safari/537.36', '_blank'],
    ['Mozilla/5.0 Version/18.0 Mobile/15E148 Safari/604.1', ''],
  ])('preserves wallet checkout navigation for %s', (userAgent, target) => {
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(userAgent)
    const submit = vi
      .spyOn(HTMLFormElement.prototype, 'submit')
      .mockImplementation(function (this: HTMLFormElement) {
        expect(this.method).toBe('post')
        expect(this.target).toBe(target)
      })
    submitPaymentForm('https://pay.example.test/submit.php', { money: '10.00' })
    expect(submit).toHaveBeenCalledOnce()
  })

  it('resumes the pending order using its original amount and trade number', async () => {
    const order: MembershipOrder = {
      id: 5,
      trade_no: 'MEMBER-9-frozen',
      tier_id: tier.id,
      tier_name: 'Original Gold',
      status: 'pending',
      price_minor: 1000,
      currency: 'CNY',
      quota: 1000,
      created_at: 1000,
      checkout_state: 'ready',
    }
    vi.mocked(getMembershipOrders).mockResolvedValue([order])
    vi.mocked(resumeMembershipPayment).mockResolvedValue({
      order,
      trade_no: order.trade_no,
      status: 'pending',
      submit_url: 'https://pay.example.test/submit.php',
      form_fields: { money: '10.00', out_trade_no: order.trade_no },
    })
    const submit = vi
      .spyOn(HTMLFormElement.prototype, 'submit')
      .mockImplementation(function (this: HTMLFormElement) {
        expect(this.method).toBe('post')
        expect(this.target).toBe('_self')
        expect(Object.fromEntries(new FormData(this))).toEqual({
          money: '10.00',
          out_trade_no: order.trade_no,
        })
      })
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MembershipOrders />
      </QueryClientProvider>
    )
    const button = await screen.findByRole('button', {
      name: 'Continue payment',
    })
    expect(screen.getByText('Original Gold')).toBeVisible()
    expect(screen.getByText('CN¥10.00')).toBeVisible()
    await userEvent.click(button)
    await waitFor(() => expect(submit).toHaveBeenCalledOnce())
    expect(resumeMembershipPayment).toHaveBeenCalledWith(
      order.trade_no,
      expect.anything()
    )
    expect(createMembershipOrder).not.toHaveBeenCalled()
  })

  it('refreshes authoritative state on payment return and never activates from return parameters', async () => {
    const originalURL = window.location.href
    window.history.replaceState(
      null,
      '',
      '/membership?order=MEMBER-9-return&trade_status=TRADE_SUCCESS'
    )
    const order: MembershipOrder = {
      id: 6,
      trade_no: 'MEMBER-9-return',
      tier_id: tier.id,
      tier_name: tier.name,
      status: 'pending',
      price_minor: 9900,
      currency: 'CNY',
      quota: tier.quota,
      created_at: 1000,
      checkout_state: 'ready',
    }
    vi.mocked(getMembershipCatalog).mockResolvedValue(catalog)
    vi.mocked(getMembershipSelf)
      .mockResolvedValueOnce({
        ...membership,
        account: null,
        tier: null,
        discounts: [],
        pending_order: order,
      })
      .mockResolvedValue(membership)
    vi.mocked(getMembershipOrders)
      .mockResolvedValueOnce([order])
      .mockResolvedValue([{ ...order, status: 'success' }])
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    try {
      render(
        <QueryClientProvider client={client}>
          <MembershipCenter />
        </QueryClientProvider>
      )
      expect(
        await screen.findByText('Awaiting payment confirmation')
      ).toBeVisible()
      expect(
        screen.getByText(
          'Choose a membership to prepay quota and receive group discounts.'
        )
      ).toBeVisible()
      expect(screen.queryByText('Active')).not.toBeInTheDocument()
      expect(screen.queryByText('Membership activated')).not.toBeInTheDocument()
      expect(getMembershipSelf).toHaveBeenCalledOnce()
      const ordersSection = screen
        .getByRole('heading', { name: 'My membership orders' })
        .closest('section')
      if (!ordersSection) throw new Error('Membership orders section missing')
      await userEvent.click(
        within(ordersSection).getByRole('button', { name: 'Refresh' })
      )
      expect(await screen.findByText('Membership activated')).toBeVisible()
      expect(await screen.findByText('Active')).toBeVisible()
      expect(getMembershipSelf).toHaveBeenCalledTimes(2)
      expect(createMembershipOrder).not.toHaveBeenCalled()
      expect(resolveMembershipOrder).not.toHaveBeenCalled()
      expect(resumeMembershipPayment).not.toHaveBeenCalled()
    } finally {
      window.history.replaceState(null, '', originalURL)
    }
  })

  it('prevents retrying an externally unresolved checkout', async () => {
    vi.mocked(getMembershipOrders).mockResolvedValue([
      {
        id: 1,
        trade_no: 'pending-1',
        tier_id: 'gold',
        tier_name: 'Gold',
        status: 'pending',
        price_minor: 1000,
        currency: 'USD',
        quota: 1000,
        created_at: 1000,
        checkout_state: 'creating',
      },
    ])
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <MembershipOrders />
      </QueryClientProvider>
    )
    expect(
      await screen.findByRole('button', { name: 'Continue payment' })
    ).toBeDisabled()
    expect(
      screen.getByText(
        'Payment confirmation is pending. Please wait before paying again.'
      )
    ).toBeVisible()
  })

  it('never re-grants a successful order and handles its extra receipt by trade number', async () => {
    vi.mocked(resolveMembershipOrder).mockResolvedValue()
    const client = new QueryClient()
    render(
      <QueryClientProvider client={client}>
        <ResolveMembershipOrderDialog
          order={{
            id: 2,
            trade_no: 'success-1',
            tier_id: 'gold',
            tier_name: 'Gold',
            status: 'success',
            price_minor: 1000,
            currency: 'USD',
            quota: 1000,
            created_at: 1000,
            payments: [
              {
                receipt_id: 'extra-payment',
                status: 'paid_exception',
                amount_minor: 1000,
                currency: 'USD',
              },
            ],
          }}
          onClose={vi.fn()}
        />
      </QueryClientProvider>
    )
    expect(
      screen.queryByRole('option', {
        name: 'Retry activation from original order',
      })
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('option', { name: 'Record as handled manually' })
    ).toBeInTheDocument()
    expect(screen.getByText('extra-payment')).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Confirm handling' })
    ).toBeDisabled()
    await userEvent.type(
      screen.getByRole('textbox', { name: 'Handling reason' }),
      'Extra receipt handled separately'
    )
    await userEvent.click(
      screen.getByRole('button', { name: 'Confirm handling' })
    )
    expect(resolveMembershipOrder).toHaveBeenCalledWith(
      {
        trade_no: 'success-1',
        operation_id: expect.any(String),
        action: 'dismiss',
        reason: 'Extra receipt handled separately',
      },
      expect.anything()
    )
  })

  it('shows server membership and wallet ratios as separate model prices', () => {
    render(
      <ModelPriceCell
        model={{
          id: 1,
          model_name: 'test-model',
          quota_type: 0,
          model_ratio: 1,
          completion_ratio: 2,
          enable_groups: ['enterprise'],
          group_ratio: { enterprise: 0.2 },
          membership_group_ratio: { enterprise: 0.19 },
        }}
        options={{ selectedGroup: 'enterprise', tokenUnit: 'M' }}
      />
    )
    expect(screen.getByText('Membership quota price')).toBeVisible()
    expect(screen.getByText('Wallet price')).toBeVisible()
    expect(screen.getByText('0.38')).toBeVisible()
    expect(screen.getByText('0.4')).toBeVisible()
  })
})

describe('membership administrative safeguards', () => {
  it('accepts task-only limits but rejects missing evidence and unbounded facts', () => {
    const limit = {
      max_input_tokens: 0,
      max_output_tokens: 0,
      max_tool_calls: 0,
      task_usage_limits: { seconds: 60 },
      evidence: 'Verified provider request maximum',
    }
    expect(
      membershipCatalogSchema(i18next.t).safeParse({
        ...catalog,
        model_limits: { video: limit },
      }).success
    ).toBe(true)
    expect(
      membershipCatalogSchema(i18next.t).safeParse({
        ...catalog,
        model_limits: { video: { ...limit, evidence: '' } },
      }).success
    ).toBe(false)
    expect(
      membershipCatalogSchema(i18next.t).safeParse({
        ...catalog,
        model_limits: {
          video: { ...limit, task_usage_limits: { seconds: Infinity } },
        },
      }).success
    ).toBe(false)
  })

  it.each(['zhCN', 'zhTW'])(
    'formats purchase prices using project locale %s',
    (locale) => {
      expect(formatMembershipPrice(12345, 'USD', locale)).toContain('123.45')
    }
  )

  it('requires explicit upstream verification and a reason before releasing reserved quota', async () => {
    vi.mocked(resolveMembershipBillingReview).mockResolvedValue()
    const client = new QueryClient()
    render(
      <QueryClientProvider client={client}>
        <BillingReviewDialog
          record={{
            request_id: 'review-1',
            user_id: 9,
            source: 'membership',
            group_key: 'enterprise',
            reserved_quota: 100,
            actual_quota: 0,
            charged_quota: 0,
            excess_quota: 0,
            state: 'reserved',
            review_reason: 'unknown',
            review_started_at: 1,
            review_resolved_at: 0,
          }}
          onClose={vi.fn()}
        />
      </QueryClientProvider>
    )
    await userEvent.selectOptions(
      screen.getByRole('combobox', { name: 'Action' }),
      'release'
    )
    expect(
      screen.getByRole('button', { name: 'Confirm handling' })
    ).toBeDisabled()
    await userEvent.type(
      screen.getByRole('textbox', { name: 'Handling reason' }),
      'Provider confirmed no charge'
    )
    expect(
      screen.getByRole('button', { name: 'Confirm handling' })
    ).toBeDisabled()
    await userEvent.click(
      screen.getByRole('checkbox', {
        name: 'I verified that the upstream request was not charged.',
      })
    )
    await userEvent.click(
      screen.getByRole('button', { name: 'Confirm handling' })
    )
    expect(resolveMembershipBillingReview).toHaveBeenCalledWith(
      {
        request_id: 'review-1',
        operation_id: expect.any(String),
        action: 'release',
        actual_quota: 0,
        reason: 'Provider confirmed no charge',
      },
      expect.anything()
    )
  })
})
