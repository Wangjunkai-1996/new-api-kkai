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

import { isAccessProbeAllowed } from '../cn-restricted'

describe('mainland restricted page access probe', () => {
  test('allows returning to the console when the root document is reachable', () => {
    assert.equal(isAccessProbeAllowed(new Response('', { status: 200 })), true)
    assert.equal(
      isAccessProbeAllowed(new Response(null, { status: 204 })),
      true
    )
  })

  test('stays on the notice page when the probe is redirected or fails', () => {
    assert.equal(isAccessProbeAllowed(new Response('', { status: 302 })), false)
    assert.equal(isAccessProbeAllowed(new Response('', { status: 403 })), false)
    assert.equal(
      isAccessProbeAllowed({
        status: 0,
        type: 'opaqueredirect',
      } as Response),
      false
    )
  })
})
