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
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  paymentOrder,
  paymentQuote,
  paymentWallet,
} from '../payment-flow-fixtures'
import {
  formatPaymentMoney,
  hasSamePaymentTerms,
  mergeWalletState,
  openPaymentCheckout,
  preparePaymentWindow,
} from './payment-flow'

afterEach(() => vi.restoreAllMocks())
describe('payment facts and external checkout', () => {
  it('keeps full decimal precision and currency exponents', () => {
    expect(
      formatPaymentMoney({
        amount_minor: '9223372036854775807',
        currency: 'CNY',
        exponent: 2,
      })
    ).toBe('CNY 92,233,720,368,547,758.07')
    expect(
      formatPaymentMoney({ amount_minor: '1234', currency: 'KWD', exponent: 3 })
    ).toBe('KWD 1.234')
    expect(
      formatPaymentMoney({ amount_minor: '1234', currency: 'JPY', exponent: 0 })
    ).toBe('JPY 1,234')
  })
  it.each(['en', 'zhCN', 'zhTW', 'fr', 'ru', 'ja', 'vi'])(
    'formats payment amounts using the interface language %s',
    (locale) => {
      const canonical =
        ({ zhCN: 'zh-CN', zhTW: 'zh-TW' } as Record<string, string>)[locale] ??
        locale
      const expected = new Intl.NumberFormat(canonical, {
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
      }).format(1234.56)
      expect(
        formatPaymentMoney(
          { amount_minor: '123456', currency: 'CNY', exponent: 2 },
          locale
        )
      ).toBe(`CNY ${expected}`)
    }
  )
  it('prevents a late acknowledgement or stale order response from releasing a newer flow', () => {
    const current = paymentWallet(
      paymentOrder({ version: '9223372036854775807' }),
      {
        flow: {
          id: 'NEW',
          revision: '9223372036854775807',
          phase: 'order',
          order: paymentOrder({ version: '9223372036854775807' }),
        },
      }
    )
    expect(
      mergeWalletState(
        current,
        paymentWallet(null, {
          flow: {
            id: null,
            revision: '9223372036854775806',
            phase: 'idle',
            order: null,
          },
        })
      )
    ).toBe(current)
    expect(
      mergeWalletState(current, {
        ...current,
        flow: { ...current.flow, order: paymentOrder({ version: '2' }) },
      })
    ).toBe(current)
  })
  it('handles popup denial without navigating the wallet or submitting a form', () => {
    vi.spyOn(window, 'open').mockReturnValue(null)
    const target = preparePaymentWindow()
    expect(
      openPaymentCheckout(
        { kind: 'url', url: 'https://payments.example/checkout' },
        target
      )
    ).toBe(false)
    expect(window.location.pathname).toBe('/')
  })
  it('rejects unsafe destinations before they can navigate a payment tab', () => {
    expect(() =>
      openPaymentCheckout(
        { kind: 'url', url: 'javascript:alert(1)' },
        { window: null, navigated: false }
      )
    ).toThrow('Unsafe payment destination')
    expect(() =>
      openPaymentCheckout(
        { kind: 'url', url: 'https://user:secret@example.com/' },
        { window: null, navigated: false }
      )
    ).toThrow('Unsafe payment destination')
  })
  it('submits signed POST fields inside the external tab', () => {
    const doc = document.implementation.createHTMLDocument('payment')
    const submit = vi
      .spyOn(HTMLFormElement.prototype, 'submit')
      .mockImplementation(() => {})
    const target = {
      window: { document: doc, closed: false } as Window,
      navigated: false,
    }
    expect(
      openPaymentCheckout(
        {
          kind: 'form',
          method: 'POST',
          action: 'https://pay.example/submit',
          fields: { sign: 'literal <value>', order: 'ORDER-1' },
        },
        target
      )
    ).toBe(true)
    const form = submit.mock.contexts[0] as HTMLFormElement | undefined
    expect(form?.target).toBe('_self')
    expect(form?.querySelector('input[name=sign]')?.getAttribute('value')).toBe(
      'literal <value>'
    )
    expect(document.querySelector('form')).toBeNull()
    expect(target.navigated).toBe(true)
  })
})

it('only continues refreshed checkout when all payment terms remain unchanged', () => {
  const quote = paymentQuote()
  expect(
    hasSamePaymentTerms(quote, {
      ...quote,
      quote_token: 'FRESH',
      expires_at: quote.expires_at + 60,
    })
  ).toBe(true)
  for (const change of [
    {
      expected_payment: { amount_minor: '1001', currency: 'CNY', exponent: 2 },
    },
    { payable_payment: { amount_minor: '1000', currency: 'USD', exponent: 2 } },
    { credited_quota: '10001' },
    { credit_display: { amount: '10', unit: 'CNY' } },
    { payment_method: 'wxpay' },
    { payment_provider: 'stripe' },
    { pricing_mode: 'channel_adjusted' as const },
  ]) {
    expect(hasSamePaymentTerms(quote, { ...quote, ...change })).toBe(false)
  }
})
