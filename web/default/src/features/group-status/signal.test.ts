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

import {
  getGroupLastSignalAt,
  getGroupSignalEvents,
  GROUP_SIGNAL_COUNT,
} from './signal'
import type { GroupRecentEvent } from './types'

describe('group signal history', () => {
  test('keeps the latest 60 valid events in chronological order', () => {
    const events: GroupRecentEvent[] = Array.from(
      { length: 65 },
      (_, index) => ({
        ts: 65 - index,
        status: index % 2 === 0 ? 'success' : 'failure',
      })
    )

    const result = getGroupSignalEvents(events)

    assert.equal(GROUP_SIGNAL_COUNT, 60)
    assert.equal(result.length, 60)
    assert.equal(result[0]?.ts, 6)
    assert.equal(result.at(-1)?.ts, 65)
    assert.equal(events[0]?.ts, 65)
  })

  test('uses the newest event timestamp and falls back to the sample time', () => {
    assert.equal(
      getGroupLastSignalAt({
        recent_events: [
          { ts: 100, status: 'success' },
          { ts: 300, status: 'failure' },
          { ts: 200, status: 'success' },
        ],
        sampled_at: 999,
      }),
      300
    )
    assert.equal(
      getGroupLastSignalAt({ recent_events: [], sampled_at: 999 }),
      999
    )
    assert.equal(
      getGroupLastSignalAt({ recent_events: null, sampled_at: 888 }),
      888
    )
  })
})
