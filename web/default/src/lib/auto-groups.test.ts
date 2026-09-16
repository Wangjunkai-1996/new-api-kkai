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
*/

import { describe, expect, test } from 'vitest'

import {
  getAutoGroupChain,
  getAutoGroupNames,
  isAutoGroupName,
  normalizeAutoGroupChains,
} from './auto-groups'

describe('auto group chains', () => {
  test('keeps the legacy auto_groups response compatible', () => {
    const chains = normalizeAutoGroupChains(undefined, ['default', 'vip'])

    expect(chains).toEqual({ auto: ['default', 'vip'] })
    expect(isAutoGroupName('auto', chains)).toBe(true)
  })

  test('normalizes named chains and ignores invalid entries', () => {
    const chains = normalizeAutoGroupChains({
      auto: ['default', '', 1],
      auto2: ['vip', 'default'],
      broken: [],
    })

    expect(chains).toEqual({
      auto: ['default'],
      auto2: ['vip', 'default'],
    })
    expect(getAutoGroupNames(chains)).toEqual(['auto', 'auto2'])
    expect(getAutoGroupChain(chains, 'auto2')).toEqual(['vip', 'default'])
    expect(isAutoGroupName('auto2', chains)).toBe(true)
    expect(isAutoGroupName('automatic', chains)).toBe(false)
  })

  test('falls back to the legacy chain when the new value is empty', () => {
    expect(normalizeAutoGroupChains({}, ['default'])).toEqual({
      auto: ['default'],
    })
    expect(normalizeAutoGroupChains(null, [])).toEqual({})
  })
})

