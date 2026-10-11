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
import { describe, expect, it } from 'vitest'

import {
  getPriceDetail,
  getPriceSummary,
  getSecondsUnitPrice,
  type ModelPricingSnapshot,
} from './model-pricing-snapshots'

const translate = (key: string) => key

const snapshot = (billingExpr: string): ModelPricingSnapshot => ({
  name: 'seedance-2.5',
  billingMode: 'tiered_expr',
  billingExpr,
  hasConflict: false,
})

describe('seconds task pricing summaries', () => {
  it('recognizes the generated seconds-only expression', () => {
    expect(getSecondsUnitPrice('tier("base", u("seconds") * 1.25)')).toBe(
      '1.25'
    )
    expect(
      getSecondsUnitPrice('tier("base", u("seconds") * 1.25 + u("width") * 2)')
    ).toBeNull()
    expect(getSecondsUnitPrice('tier("base", c * 1.25)')).toBeNull()
  })

  it('shows seconds expressions with an explicit per-second price', () => {
    const row = snapshot('tier("base", u("seconds") * 1.25)')
    expect(getPriceSummary(row, translate)).toBe('$1.25 / second')
    expect(getPriceDetail(row, translate)).toBe(
      'Expression pricing · $1.25 / second'
    )
  })
})
