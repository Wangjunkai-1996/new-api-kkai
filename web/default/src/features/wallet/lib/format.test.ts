import { afterEach, describe, expect, it } from 'vitest'

import { useSystemConfigStore } from '@/stores/system-config-store'

import { formatTopupCreditAmount, formatWalletPaymentAmount } from './format'

const originalCurrency = useSystemConfigStore.getState().config.currency

afterEach(() => {
  useSystemConfigStore.getState().setConfig({ currency: originalCurrency })
})

describe('formatTopupCreditAmount', () => {
  it('keeps token top-up units as tokens instead of labelling them as currency', () => {
    useSystemConfigStore.getState().setConfig({
      currency: { ...originalCurrency, quotaDisplayType: 'TOKENS' },
    })

    const display = formatTopupCreditAmount(500000, 'Tokens')
    expect(display).toContain('500,000')
    expect(display).toContain('Tokens')
    expect(display).not.toContain('$')
  })

  it('shows credited USD separately from the payment currency', () => {
    useSystemConfigStore.getState().setConfig({
      currency: {
        ...originalCurrency,
        quotaDisplayType: 'CNY',
        usdExchangeRate: 7,
      },
    })

    expect(formatTopupCreditAmount(10, 'Tokens')).toBe('$10')
  })
})

describe('formatWalletPaymentAmount', () => {
  it('uses yuan only for CNY payment methods', () => {
    expect(formatWalletPaymentAmount(98, 'alipay')).toBe('¥98')
    expect(formatWalletPaymentAmount(98, 'wxpay')).toBe('¥98')
    expect(formatWalletPaymentAmount(98, 'custom1')).toBe('¥98')
    expect(formatWalletPaymentAmount(98, 'waffo_pancake')).toBe('$98')
    expect(formatWalletPaymentAmount(98, 'stripe')).toBe('98')
    expect(formatWalletPaymentAmount(98, 'waffo')).toBe('98')
  })
})
