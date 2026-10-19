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
import { render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { expect, it, vi } from 'vitest'

import { Wallet } from './index'

const site = vi.hoisted(() => ({ tenant_id: 0 }))
vi.mock('@/features/site-config', () => ({
  useSiteConfig: () => ({ data: site }),
}))
vi.mock('@/lib/api', () => ({
  getSelf: async () => ({ success: true, data: { quota: 10000 } }),
}))
vi.mock('@/components/layout', () => {
  const SectionPageLayout = (props: { children: ReactNode }) => (
    <div>{props.children}</div>
  )
  SectionPageLayout.Title = SectionPageLayout
  SectionPageLayout.Actions = SectionPageLayout
  SectionPageLayout.Content = SectionPageLayout
  return { SectionPageLayout }
})
vi.mock('@/features/membership/components/wallet-summary', () => ({
  MembershipWalletSummary: () => <p>Membership benefits</p>,
}))
vi.mock('./components/subscription-plans-card', () => ({
  SubscriptionPlansCard: () => <p>Subscription benefits</p>,
}))
vi.mock('./components/recharge-form-card', () => ({
  RechargeFormCard: () => <p>Customer topup</p>,
}))
vi.mock('./components/wallet-stats-card', () => ({
  WalletStatsCard: () => null,
}))
vi.mock('./components/dialogs/billing-history-dialog', () => ({
  BillingHistoryDialog: () => null,
}))
vi.mock('./components/dialogs/creem-confirm-dialog', () => ({
  CreemConfirmDialog: () => null,
}))
vi.mock('./components/dialogs/payment-confirm-dialog', () => ({
  PaymentConfirmDialog: () => null,
}))
vi.mock('./hooks', () => ({
  useTopupInfo: () => ({ topupInfo: null, presetAmounts: [], loading: false }),
  usePayment: () => ({
    processing: false,
    calculatePaymentAmount: vi.fn(),
    processPayment: vi.fn(),
  }),
  useRedemption: () => ({ redeeming: false, redeemCode: vi.fn() }),
  useCreemPayment: () => ({ processing: false, processCreemPayment: vi.fn() }),
  useWaffoPayment: () => ({ processing: false, processWaffoPayment: vi.fn() }),
  useWaffoPancakePayment: () => ({
    processing: false,
    processWaffoPancakePayment: vi.fn(),
  }),
}))

it.each([0, 12])(
  'Host tenant %s offers topup and only platform Hosts mount subscription modules',
  async (tenantId) => {
    site.tenant_id = tenantId
    render(<Wallet />)
    expect(await screen.findByText('Customer topup')).toBeVisible()
    for (const label of ['Membership benefits', 'Subscription benefits']) {
      if (tenantId === 0) expect(screen.getByText(label)).toBeVisible()
      else expect(screen.queryByText(label)).not.toBeInTheDocument()
    }
  }
)
