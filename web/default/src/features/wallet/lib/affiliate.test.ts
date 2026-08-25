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

import { generateAffiliateLink } from './affiliate'

describe('affiliate links', () => {
  test('uses the sign-up route', () => {
    const link = new URL(generateAffiliateLink('gd5c'))

    expect(link.pathname).toBe('/sign-up')
    expect(link.searchParams.get('aff')).toBe('gd5c')
  })

  test('encodes affiliate codes for use in a query string', () => {
    const link = generateAffiliateLink('a+b/c')

    expect(link).toContain('/sign-up?aff=a%2Bb%2Fc')
  })
})
