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
import { describe, test } from 'node:test'

import { resolveImageStudioDraftPersistence } from '../image-draft-persistence'

describe('image studio draft persistence', () => {
  test('clears a saved generation draft when the prompt becomes empty', () => {
    assert.deepEqual(
      resolveImageStudioDraftPersistence('generation', true, 11, {
        model_profile_id: 7,
        prompt: '   ',
        parameters: { size: '1024x1024', count: 1 },
      }),
      { action: 'clear' }
    )
    assert.equal(
      resolveImageStudioDraftPersistence('generation', true, 11, {
        model_profile_id: 7,
        prompt: 'keep this prompt',
        parameters: { size: '1024x1024', count: 1 },
      }).action,
      'save'
    )
    assert.deepEqual(
      resolveImageStudioDraftPersistence('edit', true, 11, {
        model_profile_id: 7,
        prompt: '',
        parameters: {},
      }),
      { action: 'skip' }
    )
  })
})
