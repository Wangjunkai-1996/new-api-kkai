import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import {
  getFlowQuotaDates,
  getUserQuotaDataByUsers,
  getUserQuotaDates,
} from './api'

const timeRange = { start_timestamp: 3600, end_timestamp: 7200 }
const response = { success: true, data: [] }

afterEach(() => vi.restoreAllMocks())

it('uses the self route without forwarding username', async () => {
  const get = vi.spyOn(api, 'get').mockResolvedValue({ data: response })

  await getUserQuotaDates({ ...timeRange, username: 'alice' })

  expect(get).toHaveBeenCalledWith('/api/data/self', { params: timeRange })
})

it('uses the admin route and forwards username', async () => {
  const get = vi.spyOn(api, 'get').mockResolvedValue({ data: response })
  const params = { ...timeRange, username: 'alice' }

  await getUserQuotaDates(params, true)

  expect(get).toHaveBeenCalledWith('/api/data', { params })
})

it('passes signals with duplicate GET protection', async () => {
  const get = vi.spyOn(api, 'get').mockResolvedValue({ data: response })
  const signal = new AbortController().signal

  await getUserQuotaDates(timeRange, false, signal)
  await getUserQuotaDataByUsers(timeRange, signal)

  expect(get).toHaveBeenNthCalledWith(1, '/api/data/self', {
    params: timeRange,
    signal,
    disableDuplicate: true,
  })
  expect(get).toHaveBeenNthCalledWith(2, '/api/data/users', {
    params: timeRange,
    signal,
    disableDuplicate: true,
  })
})

it('does not forward a username to the self flow route', async () => {
  const get = vi.spyOn(api, 'get').mockResolvedValue({ data: response })

  await getFlowQuotaDates({ ...timeRange, username: 'alice' })

  expect(get).toHaveBeenCalledWith('/api/data/flow/self', { params: timeRange })
})
