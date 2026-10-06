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
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { createChannelFieldUpdateScheduler } from '../channel-field-update'

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

describe('channel field update scheduler', () => {
  test('coalesces rapid schedules into one update with the latest value', () => {
    const update = vi.fn()
    const scheduler = createChannelFieldUpdateScheduler(update)
    scheduler.schedule(1)
    scheduler.schedule(2)
    scheduler.schedule(3)
    expect(update).not.toHaveBeenCalled()
    vi.runAllTimers()
    expect(update).toHaveBeenCalledExactlyOnceWith(3)
  })

  test('flush commits the pending value, including zero, without a duplicate timer update', () => {
    const update = vi.fn()
    const scheduler = createChannelFieldUpdateScheduler(update)
    scheduler.schedule(7)
    scheduler.schedule(0)
    scheduler.flush()
    expect(update).toHaveBeenCalledExactlyOnceWith(0)
    scheduler.flush()
    vi.runAllTimers()
    expect(update).toHaveBeenCalledExactlyOnceWith(0)
  })
})
