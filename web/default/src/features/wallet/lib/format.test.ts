import { afterEach, describe, expect, it } from 'vitest'

import { useSystemConfigStore } from '@/stores/system-config-store'

import { formatTopupCreditAmount } from './format'

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

  it('converts USD credit units to the configured display currency', () => {
    useSystemConfigStore.getState().setConfig({
      currency: {
        ...originalCurrency,
        quotaDisplayType: 'CNY',
        usdExchangeRate: 7,
      },
    })

    expect(formatTopupCreditAmount(10, 'Tokens')).toContain('¥')
    expect(formatTopupCreditAmount(10, 'Tokens')).toContain('70')
  })
})
