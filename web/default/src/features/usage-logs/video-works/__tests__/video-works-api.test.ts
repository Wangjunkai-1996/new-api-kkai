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
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { getVideoWorks } from '../../api'

const item = {
  id: 11,
  user_id: 7,
  task_id: 'task-video',
  platform: 'sora',
  action: 'text_to_video',
  channel_id: 60,
  group: 'default',
  quota: 320000,
  submit_time: 1,
  status: 'SUCCESS',
  billing: { amount_available: true, state: 'completed' },
}
const page = {
  items: [item],
  total: 1,
  page: 1,
  page_size: 20,
  status_counts: { all: 1, success: 1, processing: 0, failed: 0, unknown: 0 },
}

afterEach(() => vi.restoreAllMocks())

test('requests video-only server filtering and parses media warning without dropping task records', async () => {
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: { ...page, media_warning: 'media_temporarily_unavailable' },
    },
  })
  const controller = new AbortController()
  const result = await getVideoWorks(
    { q: 'sora %', status_group: 'success', p: 1, page_size: 20 },
    true,
    controller.signal
  )
  expect(result.items).toHaveLength(1)
  expect(result.media_warning).toBe('media_temporarily_unavailable')
  const url = new URL(String(get.mock.calls[0][0]), 'https://app.example')
  expect(url.pathname).toBe('/api/task/')
  expect(Object.fromEntries(url.searchParams)).toEqual({
    q: 'sora %',
    status_group: 'success',
    p: '1',
    page_size: '20',
    media_type: 'video',
  })
  expect(get.mock.calls[0][1]?.signal).toBe(controller.signal)
})

test('preserves unavailable monetary truth separately from zero and accepts a missing pricing snapshot', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        ...page,
        items: [{ ...item, quota: 0, billing: { amount_available: false } }],
      },
    },
  })
  const result = await getVideoWorks({}, false)
  expect(result.items[0].billing).toEqual({ amount_available: false })
  expect(result.items[0].quota).toBe(0)
})

test('rejects missing video billing availability, status counts, and failed business responses', async () => {
  const get = vi.spyOn(api, 'get')
  get.mockResolvedValueOnce({
    data: {
      success: true,
      data: { ...page, items: [{ ...item, billing: { state: 'completed' } }] },
    },
  })
  await expect(getVideoWorks({}, false)).rejects.toThrow()
  get.mockResolvedValueOnce({
    data: { success: true, data: { ...page, status_counts: undefined } },
  })
  await expect(getVideoWorks({}, false)).rejects.toThrow()
  get.mockResolvedValueOnce({
    data: { success: false, message: 'list unavailable' },
  })
  await expect(getVideoWorks({}, false)).rejects.toThrow('list unavailable')
})

test('known hosted tasks cannot fall back to an unexpected generic artifact response', async () => {
  const { getTaskArtifacts } = await import('../../api')
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { artifacts: [] } },
  })
  await expect(
    getTaskArtifacts('task-video', { recordId: 11, requireHosted: true })
  ).rejects.toThrow('missing_hosted_video')
})

test('keeps corrupt historical amounts unavailable without rejecting the entire list, but rejects a negative available charge', async () => {
  const get = vi.spyOn(api, 'get')
  get.mockResolvedValueOnce({
    data: {
      success: true,
      data: {
        ...page,
        items: [{ ...item, quota: -1, billing: { amount_available: false } }],
      },
    },
  })
  const result = await getVideoWorks({}, false)
  expect(result.items[0].billing?.amount_available).toBe(false)
  get.mockResolvedValueOnce({
    data: { success: true, data: { ...page, items: [{ ...item, quota: -1 }] } },
  })
  await expect(getVideoWorks({}, false)).rejects.toThrow()
})
