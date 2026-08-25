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
import assert from 'node:assert/strict'

import { describe, test } from 'vitest'

import { DEFAULT_TOPUP_LINK, resolveTopupLink } from './constants'

describe('wallet topup link resolution', () => {
  test('uses the configured HTTP or HTTPS link', () => {
    assert.equal(
      resolveTopupLink('  https://example.com/checkout?order=1  '),
      'https://example.com/checkout?order=1'
    )
    assert.equal(
      resolveTopupLink('http://example.com/checkout'),
      'http://example.com/checkout'
    )
  })

  test('falls back for empty, relative, or non-web URLs', () => {
    for (const value of [
      undefined,
      '',
      '/checkout',
      'javascript:alert(1)',
      'data:text/html,checkout',
      'ftp://example.com/checkout',
    ]) {
      assert.equal(resolveTopupLink(value), DEFAULT_TOPUP_LINK)
    }
  })
})
