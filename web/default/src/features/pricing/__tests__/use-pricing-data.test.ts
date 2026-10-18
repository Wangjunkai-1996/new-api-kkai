import { describe, expect, it } from 'vitest'

import { getEffectiveUsdExchangeRate } from '../hooks/use-pricing-data'

describe('pricing exchange rate', () => {
  it('uses the configured rate only for local currency displays', () => {
    expect(getEffectiveUsdExchangeRate('USD', 7)).toBe(1)
    expect(getEffectiveUsdExchangeRate('TOKENS', 7)).toBe(1)
    expect(getEffectiveUsdExchangeRate('CNY', 7)).toBe(7)
    expect(getEffectiveUsdExchangeRate('CUSTOM', 7, 4)).toBe(4)
  })
})
