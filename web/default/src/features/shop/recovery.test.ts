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
import { beforeEach, describe, expect, it } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import {
  beginRecovery,
  finishRecovery,
  readRecovery,
  recoveryName,
} from './recovery'

describe('private operation recovery references', () => {
  beforeEach(() => {
    sessionStorage.clear()
    useAuthStore.getState().auth.setUser({ id: 1, username: 'buyer', role: 1 })
  })
  it('retains the same key until an operation is resolved and stores no payload', () => {
    const first = beginRecovery('create-order', 'p1')
    expect(beginRecovery('create-order', 'p1')).toEqual(first)
    expect(Object.keys(first).sort()).toEqual([
      'created_at',
      'identity',
      'key',
      'scope',
      'target',
    ])
    finishRecovery('create-order', 'p1')
    expect(readRecovery('create-order', 'p1')).toBeNull()
  })
  it('does not reuse references across identities, target, or operation scope', () => {
    const marker = beginRecovery('create-order', 'p1')
    sessionStorage.setItem(
      recoveryName('import-commit', 'p1'),
      JSON.stringify(marker)
    )
    sessionStorage.setItem(
      recoveryName('create-order', 'p2'),
      JSON.stringify(marker)
    )
    expect(readRecovery('import-commit', 'p1')).toBeNull()
    expect(readRecovery('create-order', 'p2')).toBeNull()
    useAuthStore.getState().auth.setUser({ id: 2, username: 'other', role: 1 })
    expect(readRecovery('create-order', 'p1')).toBeNull()
  })
})
