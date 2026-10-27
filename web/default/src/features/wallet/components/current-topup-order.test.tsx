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
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { paymentOrder, paymentWallet } from '../payment-flow-fixtures'
import { CurrentTopupOrder } from './current-topup-order'
import { PaymentAmounts } from './payment-amounts'
import { PaymentEvidence } from './payment-evidence'

describe('current top-up result and payment facts', () => {
  it('shows a prominent return action for a reliable timeout and never acknowledges automatically', () => {
    const acknowledge = vi.fn()
    const order = paymentOrder({
      display_status: 'expired',
      status_group: 'closed',
      allowed_actions: ['acknowledge'],
    })
    render(
      <CurrentTopupOrder
        wallet={paymentWallet(order, {
          flow: { id: 'FLOW-1', revision: '2', phase: 'result', order },
        })}
        busy={false}
        onResume={vi.fn()}
        onEnd={vi.fn()}
        onReconcile={vi.fn()}
        onAcknowledge={acknowledge}
        onHistory={vi.fn()}
      />
    )
    expect(
      screen.getByRole('heading', { name: 'Order timed out' })
    ).toBeVisible()
    expect(
      screen.getByText(
        'This order timed out and was automatically cancelled. It can no longer be paid.'
      )
    ).toBeVisible()
    const button = screen.getByRole('button', { name: 'Return to top-up' })
    expect(button).toBeVisible()
    expect(acknowledge).not.toHaveBeenCalled()
    fireEvent.click(button)
    expect(acknowledge).toHaveBeenCalledTimes(1)
  })
  it('does not offer a return or another payment while expiry is still processing', () => {
    const order = paymentOrder({
      display_status: 'expiry_processing',
      status_group: 'processing',
      allowed_actions: ['reconcile'],
    })
    render(
      <CurrentTopupOrder
        wallet={paymentWallet(order, {
          flow: { id: 'FLOW-1', revision: '2', phase: 'processing', order },
        })}
        busy={false}
        onResume={vi.fn()}
        onEnd={vi.fn()}
        onReconcile={vi.fn()}
        onAcknowledge={vi.fn()}
        onHistory={vi.fn()}
      />
    )
    expect(
      screen.queryByRole('button', { name: 'Return to top-up' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Continue payment' })
    ).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Check payment' })).toBeVisible()
  })
  it('labels an adjustable base quote as an order amount until the final amount is verified', () => {
    render(
      <PaymentAmounts
        pricing={paymentOrder({
          pricing_mode: 'channel_adjusted',
          payable_payment: null,
          pricing_notice_code: 'CHANNEL_ADJUSTMENTS_APPLY',
        })}
      />
    )
    expect(screen.getByText('Order amount')).toBeVisible()
    expect(screen.queryByText('Amount due')).not.toBeInTheDocument()
    expect(
      screen.getByText(
        'The final amount includes any valid discounts and taxes shown on the payment page.'
      )
    ).toBeVisible()
  })
  it('shows configured top-up savings in the lifecycle payment summary', () => {
    render(<PaymentAmounts pricing={paymentOrder()} discountRate={0.8} />)

    expect(screen.getByText('Original Price')).toBeVisible()
    expect(screen.getByText('You save')).toBeVisible()
    expect(screen.getByText('CNY 12.50')).toBeVisible()
    expect(screen.getByText('CNY 2.50')).toBeVisible()
  })
  it('offers a local end action without claiming to cancel the provider session', () => {
    const end = vi.fn()
    const order = paymentOrder({
      allowed_actions: ['resume', 'end', 'reconcile'],
    })
    render(
      <CurrentTopupOrder
        wallet={paymentWallet(order)}
        busy={false}
        onResume={vi.fn()}
        onEnd={end}
        onReconcile={vi.fn()}
        onAcknowledge={vi.fn()}
        onHistory={vi.fn()}
      />
    )
    expect(
      screen.getByRole('button', { name: 'Continue payment' })
    ).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Cancel order' })
    ).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'End this top-up' }))
    expect(end).not.toHaveBeenCalled()
    expect(
      screen.getByText(
        'Ending this top-up lets you start a new order after returning to top-up. It does not guarantee that the previous payment page is closed. Any payment actually received will still be verified and credited.'
      )
    ).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm ending' }))
    expect(end).toHaveBeenCalledTimes(1)
  })
  it.each(['ended', 'timed_out'] as const)(
    'keeps a %s result with an explicit return action and no payment operations',
    (status) => {
      const acknowledge = vi.fn()
      const order = paymentOrder({
        display_status: status,
        status_group: 'closed',
        allowed_actions: [
          'resume',
          'end',
          'cancel',
          'reconcile',
          'acknowledge',
          'contact_support',
        ],
        local_end_reason: status === 'ended' ? 'user_ended' : 'timeout',
        local_ended_at: 1_790_000_100,
      })
      render(
        <CurrentTopupOrder
          wallet={paymentWallet(order, {
            flow: { id: 'FLOW-1', revision: '2', phase: 'result', order },
          })}
          busy={false}
          onResume={vi.fn()}
          onEnd={vi.fn()}
          onReconcile={vi.fn()}
          onAcknowledge={acknowledge}
          onHistory={vi.fn()}
        />
      )
      expect(
        screen.getByRole('heading', {
          name: status === 'ended' ? 'Top-up ended' : 'Top-up timed out',
        })
      ).toBeVisible()
      expect(
        screen.getByRole('button', { name: 'Return to top-up' })
      ).toBeVisible()
      expect(
        screen.queryByRole('button', { name: 'Continue payment' })
      ).not.toBeInTheDocument()
      expect(
        screen.queryByRole('button', { name: 'End this top-up' })
      ).not.toBeInTheDocument()
      expect(
        screen.queryByRole('button', { name: 'Check payment' })
      ).not.toBeInTheDocument()
      expect(
        screen.queryByText(
          'This order timed out and was automatically cancelled. It can no longer be paid.'
        )
      ).not.toBeInTheDocument()
      expect(
        screen.getByText(
          'Do not pay through the previous payment page. If a payment was actually received, it will still be verified and credited.'
        )
      ).toBeVisible()
      expect(acknowledge).not.toHaveBeenCalled()
      fireEvent.click(screen.getByRole('button', { name: 'Return to top-up' }))
      expect(acknowledge).toHaveBeenCalledTimes(1)
    }
  )
  it('distinguishes verified payment evidence from credit processing and a review exception', () => {
    render(
      <PaymentEvidence
        receipts={[
          {
            id: 'RECEIPT-1',
            payment: { amount_minor: '1000', currency: 'CNY', exponent: 2 },
            paid_at: 1_790_000_000,
            received_at: 1_790_000_010,
            verification_status: 'verified',
            processing_status: 'review',
            exception_reason: 'CREDIT_REVIEW',
            review_owner: 'payment-operations',
            evidence_source: 'provider_query',
          },
        ]}
      />
    )
    expect(screen.getByText('Verified')).toBeVisible()
    expect(screen.getByText('Under review')).toBeVisible()
    expect(screen.getByText('CREDIT_REVIEW')).toBeVisible()
    expect(screen.getByText('payment-operations')).toBeVisible()
    expect(screen.queryByText('Credits added')).not.toBeInTheDocument()
  })
  it('prioritizes received funds over a retained local-end marker without offering another payment', () => {
    const order = paymentOrder({
      display_status: 'credit_review',
      status_group: 'review',
      allowed_actions: [
        'resume',
        'end',
        'cancel',
        'reconcile',
        'contact_support',
      ],
      local_end_reason: 'timeout',
      local_ended_at: 1_790_000_050,
    })
    render(
      <CurrentTopupOrder
        wallet={paymentWallet(order, {
          flow: { id: 'FLOW-1', revision: '3', phase: 'review', order },
        })}
        busy={false}
        onResume={vi.fn()}
        onEnd={vi.fn()}
        onReconcile={vi.fn()}
        onAcknowledge={vi.fn()}
        onHistory={vi.fn()}
      />
    )
    expect(
      screen.getByRole('heading', { name: 'Credit under review' })
    ).toBeVisible()
    expect(screen.getByRole('button', { name: 'Check payment' })).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Continue payment' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'End this top-up' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Return to top-up' })
    ).not.toBeInTheDocument()
  })
})
