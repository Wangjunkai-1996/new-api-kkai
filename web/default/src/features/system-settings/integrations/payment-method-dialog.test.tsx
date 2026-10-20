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
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { PaymentMethodDialog } from './payment-method-dialog'

describe('payment method selection', () => {
  it('shows presets outside the clipping dialog and saves WeChat Pay after selecting it', async () => {
    const save = vi.fn()
    const onOpenChange = vi.fn()
    render(
      <PaymentMethodDialog open onOpenChange={onOpenChange} onSave={save} />
    )
    const user = userEvent.setup()
    const input = screen.getByRole('combobox', { name: 'Payment type key' })

    await user.click(input)
    const wechat = screen.getByRole('option', {
      name: 'WeChat Pay (Epay: wxpay)',
    })
    expect(wechat).toBeVisible()
    // A popup inside DialogContent is clipped by its transform and overflow.
    expect(screen.getByRole('dialog')).not.toContainElement(wechat)
    await user.click(wechat)

    expect(input).toHaveValue('WeChat Pay (Epay: wxpay)')
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue(
      'WeChat Pay'
    )
    expect(screen.getByPlaceholderText('e.g., SiAlipay')).toHaveValue(
      'SiWechat'
    )
    expect(onOpenChange).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: /^Add$/ }))

    expect(save).toHaveBeenCalledWith({
      name: 'WeChat Pay',
      type: 'wxpay',
      icon: 'SiWechat',
    })
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('selects a preset with the keyboard without submitting the form', async () => {
    const save = vi.fn()
    render(<PaymentMethodDialog open onOpenChange={vi.fn()} onSave={save} />)
    const user = userEvent.setup()
    const input = screen.getByRole('combobox', { name: 'Payment type key' })

    await user.click(input)
    await user.keyboard('{ArrowDown}{ArrowDown}{Enter}')

    expect(input).toHaveValue('WeChat Pay (Epay: wxpay)')
    expect(input).toHaveAttribute('aria-expanded', 'false')
    expect(save).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeVisible()
  })

  it('preserves custom payment identifiers and the entered display name', async () => {
    const save = vi.fn()
    render(<PaymentMethodDialog open onOpenChange={vi.fn()} onSave={save} />)
    const user = userEvent.setup()

    await user.type(screen.getByRole('textbox', { name: 'Name' }), 'My gateway')
    await user.type(
      screen.getByRole('combobox', { name: 'Payment type key' }),
      'custom_pay{Enter}'
    )
    expect(save).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: /^Add$/ }))

    expect(save).toHaveBeenCalledWith({
      name: 'My gateway',
      type: 'custom_pay',
    })
  })
})
