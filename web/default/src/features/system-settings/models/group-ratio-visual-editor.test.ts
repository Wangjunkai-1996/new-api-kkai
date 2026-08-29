/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

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
  buildGroupPricingRows,
  serializeGroupPricingRows,
} from './group-ratio-serialization'

function parseObject(value: string) {
  return JSON.parse(value) as Record<string, unknown>
}

describe('group ratio visual editor serialization', () => {
  test('keeps display-only labels display-only and preserves auxiliary maps', () => {
    const rows = buildGroupPricingRows(
      '{"stable": 0.5}',
      '{"selectable": "Selectable only"}',
      '{"topup": 1.2}',
      '{"stable": "Stable plan", "auto-only": "Automatic plan"}',
      () => 'row'
    )

    const displayOnly = rows.find((row) => row.name === 'auto-only')
    expect(displayOnly).toMatchObject({
      displayName: 'Automatic plan',
      hasRatio: false,
      hasTopupRatio: false,
      hasUserUsable: false,
    })

    expect(displayOnly).toBeDefined()
    if (!displayOnly) throw new Error('display-only row was not built')
    displayOnly.displayName = 'Renamed automatic plan'
    const serialized = serializeGroupPricingRows(rows)

    expect(parseObject(serialized.GroupRatio)).toEqual({ stable: 0.5 })
    expect(parseObject(serialized.UserUsableGroups)).toEqual({
      selectable: 'Selectable only',
    })
    expect(parseObject(serialized.TopupGroupRatio)).toEqual({ topup: 1.2 })
    expect(parseObject(serialized.GroupDisplayNames)).toEqual({
      stable: 'Stable plan',
      'auto-only': 'Renamed automatic plan',
    })
  })

  test('materializes an absent canonical field only after that field is edited', () => {
    const rows = buildGroupPricingRows(
      '{"ratio-only": 1}',
      '{}',
      '{}',
      '{"ratio-only": "Ratio only"}',
      () => 'row'
    )
    const row = rows[0]

    row.topupRatio = '1.4'
    row.editedFields.topupRatio = true
    row.selectable = true
    row.description = 'Can select'
    row.editedFields.selectable = true

    const serialized = serializeGroupPricingRows(rows)
    expect(parseObject(serialized.GroupRatio)).toEqual({ 'ratio-only': 1 })
    expect(parseObject(serialized.TopupGroupRatio)).toEqual({
      'ratio-only': 1.4,
    })
    expect(parseObject(serialized.UserUsableGroups)).toEqual({
      'ratio-only': 'Can select',
    })
  })

  test('does not trim an existing canonical identifier while editing its label', () => {
    const rows = buildGroupPricingRows(
      '{" stable ": 1}',
      '{}',
      '{}',
      '{" stable ": "Legacy"}',
      () => 'row'
    )
    rows[0].displayName = 'New label'

    const serialized = serializeGroupPricingRows(rows)
    expect(parseObject(serialized.GroupRatio)).toEqual({ ' stable ': 1 })
    expect(parseObject(serialized.GroupDisplayNames)).toEqual({
      ' stable ': 'New label',
    })
  })
})
