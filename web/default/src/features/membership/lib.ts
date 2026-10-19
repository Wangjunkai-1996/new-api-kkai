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
import { submitPaymentForm } from '@/features/wallet/lib/payment'
import { toIntlLocale } from '@/i18n/languages'

import type {
  MembershipCatalog,
  MembershipCheckout,
  MembershipOrder,
  MembershipTier,
} from './types'

export function openMembershipCheckout(checkout: MembershipCheckout): boolean {
  const destination = checkout.payment_url || checkout.submit_url
  if (!destination) return false
  const url = new URL(destination)
  if (url.protocol !== 'https:' && url.protocol !== 'http:') {
    throw new Error('Invalid payment link')
  }
  if (checkout.submit_url && checkout.form_fields) {
    submitPaymentForm(url.href, checkout.form_fields, '_self')
  } else window.location.assign(url.href)
  return true
}

export function formatMembershipPrice(
  amount: number,
  currency: string,
  locale: string
): string {
  return new Intl.NumberFormat(toIntlLocale(locale), {
    style: 'currency',
    currency,
  }).format(amount / 100)
}

export function canPurchaseMembership(
  tier: MembershipTier,
  salesEnabled: boolean
): boolean {
  const action = tier.eligibility?.action
  return (
    salesEnabled &&
    tier.enabled &&
    (action === 'purchase' || action === 'upgrade') &&
    !!tier.payment_methods?.length
  )
}

export function isMembershipOrderPending(order: MembershipOrder): boolean {
  return order.status === 'pending'
}

export function normalizeMembershipCatalogForSubmission(
  catalog: MembershipCatalog
): MembershipCatalog {
  return {
    ...catalog,
    model_limits: catalog.model_limits ?? {},
    groups: catalog.groups.map((group) => ({ ...group, published: true })),
    tiers: catalog.tiers.map((tier) => ({
      ...tier,
      discounts: catalog.groups.reduce(
        (discounts, group) => ({
          ...discounts,
          [group.key]: tier.discounts[group.key] ?? 1,
        }),
        { ...tier.discounts }
      ),
    })),
  }
}

export interface MembershipDiscountConflict {
  group: string
  tier: string
}

export function findMembershipDiscountConflicts(
  catalog: MembershipCatalog
): MembershipDiscountConflict[] {
  const tiers = [...catalog.tiers].sort((a, b) => a.rank - b.rank)
  const conflicts: MembershipDiscountConflict[] = []
  for (const group of catalog.groups) {
    let previous = 1
    for (const tier of tiers) {
      const coefficient = tier.discounts[group.key] ?? 1
      if (
        !Number.isFinite(coefficient) ||
        coefficient <= 0 ||
        coefficient > previous
      ) {
        conflicts.push({ group: group.key, tier: tier.id })
      }
      previous = coefficient
    }
  }
  return conflicts
}
