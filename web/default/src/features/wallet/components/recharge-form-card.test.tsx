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
import { fireEvent, render, screen, within } from '@testing-library/react'
import { useState, type ComponentProps } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { paymentQuote } from '../payment-flow-fixtures'
import { RechargeFormCard } from './recharge-form-card'

function rechargeProps(): ComponentProps<typeof RechargeFormCard> {
  return {
    topupInfo: {
      enable_online_topup: true,
      enable_stripe_topup: true,
      pay_methods: [
        { name: 'Alipay', type: 'alipay' },
        { name: 'WeChat Pay', type: 'wxpay', min_topup: 20 },
      ],
      min_topup: 1,
      stripe_min_topup: 1,
      amount_options: [10, 20],
      discount: { 20: 0.8 },
      enable_redemption: true,
    },
    presetAmounts: [{ value: 10 }, { value: 20, discount: 0.8 }],
    selectedPreset: 10,
    onSelectPreset: vi.fn(),
    topupAmount: 10,
    onTopupAmountChange: vi.fn(),
    paymentAmount: 10,
    calculating: false,
    paymentQuoteValid: true,
    selectedPaymentMethod: { name: 'Alipay', type: 'alipay' },
    onPaymentMethodSelect: vi.fn(),
    onPaymentSubmit: vi.fn(),
    paymentLoading: null,
    redemptionCode: '',
    onRedemptionCodeChange: vi.fn(),
    onRedeem: vi.fn(),
    redeeming: false,
  }
}

function InteractiveRecharge(props: { onAmount: (value: number) => void }) {
  const [amount, setAmount] = useState(10)
  const [preset, setPreset] = useState<number | null>(10)
  return (
    <RechargeFormCard
      {...rechargeProps()}
      topupAmount={amount}
      selectedPreset={preset}
      onTopupAmountChange={(value) => {
        props.onAmount(value)
        setAmount(value)
        setPreset(null)
      }}
      onSelectPreset={(value) => {
        setAmount(value.value)
        setPreset(value.value)
      }}
    />
  )
}

describe('wallet recharge design interactions', () => {
  it('keeps custom amount selection in the amount group and accepts only safe whole amounts', () => {
    const onAmount = vi.fn()
    render(<InteractiveRecharge onAmount={onAmount} />)
    const amounts = screen.getByRole('group', { name: 'Select credit amount' })
    const custom = within(amounts).getByRole('button', {
      name: /Custom Amount/,
    })
    fireEvent.click(custom)
    expect(onAmount).toHaveBeenLastCalledWith(0)
    const input = screen.getByRole('spinbutton', { name: 'Custom Amount' })
    expect(input).toHaveAttribute('step', '1')
    fireEvent.change(input, { target: { value: '12.5' } })
    expect(onAmount).toHaveBeenLastCalledWith(0)
    expect(
      screen.getByRole('button', { name: 'Review and Pay' })
    ).toBeDisabled()
    fireEvent.change(input, { target: { value: '9007199254740992' } })
    expect(onAmount).toHaveBeenLastCalledWith(0)
    fireEvent.change(input, { target: { value: '25' } })
    expect(onAmount).toHaveBeenLastCalledWith(25)
    expect(custom).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(custom)
    expect(input).toHaveValue(25)
    fireEvent.click(within(amounts).getByRole('button', { name: /\$20/ }))
    expect(screen.queryByRole('spinbutton')).not.toBeInTheDocument()
    expect(
      within(amounts).getByRole('button', { name: /\$20/ })
    ).toHaveAttribute('aria-pressed', 'true')
  })

  it('keeps channel minimums and never advertises configured amount discounts for Stripe', () => {
    const props = rechargeProps()
    const { rerender } = render(<RechargeFormCard {...props} />)
    expect(screen.getByRole('button', { name: 'WeChat Pay' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Stripe' }))
    expect(props.onPaymentMethodSelect).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'stripe' })
    )
    expect(screen.getByText('Up to 20% off')).toBeVisible()
    rerender(
      <RechargeFormCard
        {...props}
        topupAmount={20}
        selectedPreset={20}
        selectedPaymentMethod={{ name: 'Stripe', type: 'stripe' }}
      />
    )
    expect(screen.queryByText(/Up to .*% off/)).not.toBeInTheDocument()
    expect(screen.queryByText(/Discount 20%/)).not.toBeInTheDocument()
    expect(screen.queryByText('You save')).not.toBeInTheDocument()
  })

  it('renders the API quote currency and credited amount while keeping payment-page adjustments explicit', () => {
    const props = rechargeProps()
    const quote = {
      ...paymentQuote(),
      expected_payment: { amount_minor: '12345', currency: 'JPY', exponent: 0 },
      payable_payment: null,
      pricing_mode: 'channel_adjusted' as const,
      credit_display: { amount: '50', unit: 'USD' },
    }
    render(
      <RechargeFormCard
        {...props}
        quote={quote}
        selectedPaymentMethod={{ name: 'Stripe', type: 'stripe' }}
      />
    )
    const summary = within(
      screen.getByRole('complementary', { name: 'Payment Summary' })
    )
    expect(summary.getByText('JPY')).toBeVisible()
    expect(summary.getByText('12,345')).toBeVisible()
    expect(summary.getByText('50 USD')).toBeVisible()
    expect(summary.getByText('Order amount')).toBeVisible()
    expect(summary.queryByText('Amount due')).not.toBeInTheDocument()
    expect(
      summary.getByText(
        'The final amount includes any valid discounts and taxes shown on the payment page.'
      )
    ).toBeVisible()
  })

  it('keeps redemption collapsed until requested and preserves the redeem action', () => {
    const props = rechargeProps()
    render(<RechargeFormCard {...props} redemptionCode='EXAMPLE-CODE' />)
    expect(
      screen.getByRole('textbox', { name: 'Redemption Code' })
    ).not.toBeVisible()
    fireEvent.click(screen.getByText('Redeem with a code'))
    expect(
      screen.getByRole('textbox', { name: 'Redemption Code' })
    ).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: 'Redeem' }))
    expect(props.onRedeem).toHaveBeenCalledOnce()
  })

  it.each([
    { calculating: true },
    { paymentQuoteValid: false },
    { purchaseDisabled: true },
    { paymentLoading: 'alipay' },
  ])(
    'keeps payment disabled when the current quote is not payable: %o',
    (state) => {
      render(<RechargeFormCard {...rechargeProps()} {...state} />)
      expect(
        screen.getByRole('button', { name: 'Review and Pay' })
      ).toBeDisabled()
    }
  )
})
