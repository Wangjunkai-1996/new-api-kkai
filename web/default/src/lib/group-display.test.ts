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
*/

import assert from 'node:assert/strict'

import { describe, test } from 'vitest'

import {
  getUserGroupDisplayName,
  resolveGroupDisplayName,
  toUserGroupOption,
} from './group-display'

describe('user group display metadata', () => {
  test('resolves a configured map label without changing the key', () => {
    assert.equal(
      resolveGroupDisplayName(
        'stable-key',
        { 'stable-key': 'Customer plan' },
        'Legacy description'
      ),
      'Customer plan'
    )
    assert.equal(
      resolveGroupDisplayName('legacy-key', {}, 'Legacy description'),
      'Legacy description'
    )
  })

  test('prefers display_name while retaining the canonical key as value', () => {
    const option = toUserGroupOption('legacy-key', {
      display_name: '  Customer plan  ',
      desc: 'Visible to subscribed customers',
      ratio: 0.5,
    })

    assert.deepEqual(option, {
      label: 'Customer plan',
      value: 'legacy-key',
      ratio: 0.5,
      desc: 'Visible to subscribed customers',
    })
  })

  test('uses desc as the legacy display label without duplicating it', () => {
    const option = toUserGroupOption('legacy-key', {
      desc: 'Customer plan',
      ratio: 1,
    })

    assert.deepEqual(option, {
      label: 'Customer plan',
      value: 'legacy-key',
      ratio: 1,
    })
  })

  test('falls back to the key when display metadata is blank', () => {
    assert.equal(
      getUserGroupDisplayName('legacy-key', {
        display_name: '  ',
        displayName: '',
        desc: '\n',
      }),
      'legacy-key'
    )
  })

  test('accepts camelCase display metadata and preserves non-name descriptions', () => {
    const option = toUserGroupOption('legacy-key', {
      displayName: 'Customer plan',
      desc: 'Requests use the low-cost pool',
      ratio: '自动',
    })

    assert.equal(option.value, 'legacy-key')
    assert.equal(option.label, 'Customer plan')
    assert.equal(option.desc, 'Requests use the low-cost pool')
    assert.equal(option.ratio, '自动')
  })
})
