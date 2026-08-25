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
import { describe, expect, test } from 'vitest'

import {
  resolveRechargePriceDisplay,
  serializeRechargePriceDisplay,
} from './recharge-price'

describe('KKAI recharge price default', () => {
  test('defaults to recharge pricing while preserving an explicit opt-out', () => {
    expect(resolveRechargePriceDisplay()).toBe(true)
    expect(resolveRechargePriceDisplay(false)).toBe(false)
  })

  test('only omits the configured default from route state', () => {
    expect(serializeRechargePriceDisplay(true)).toBeUndefined()
    expect(serializeRechargePriceDisplay(false)).toBe(false)
  })
})
