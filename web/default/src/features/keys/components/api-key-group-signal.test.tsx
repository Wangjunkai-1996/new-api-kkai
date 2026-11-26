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

For commercial licensing, please contact support@quantumnous.com
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, within } from '@testing-library/react'
import i18next from 'i18next'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type {
  GroupStatusEntry,
  GroupStatusResult,
} from '@/features/group-status/types'
import { api } from '@/lib/api'

import {
  ApiKeyGroupSignal,
  ApiKeyGroupSignalProvider,
} from './api-key-group-signal'

const site = vi.hoisted(() => ({ data: { tenant_id: 0 } }))
vi.mock('@/features/site-config', () => ({ useSiteConfig: () => site }))

const sample: GroupStatusEntry = {
  group: 'auto-plus',
  desc: '',
  status: 'operational',
  confidence: 'high',
  message: '',
  confidence_status: 'stable',
  experience_label: 'normal',
  display_message: '',
  request_count: 400,
  success_rate: 98.5,
  avg_latency_ms: 0,
  avg_ttft_ms: 0,
  updated_at: 1,
  sampled_at: 1,
  stale: false,
  data_source: 'redis',
  recent_events: Array.from({ length: 18 }, (_, i) => ({
    ts: 18 - i,
    status: i === 0 ? 'failure' : 'success',
  })),
}

function renderSignals(...keys: string[]) {
  const client = new QueryClient()
  render(
    <QueryClientProvider client={client}>
      <ApiKeyGroupSignalProvider>
        {keys.map((group) => (
          <ApiKeyGroupSignal key={group} group={group} variant='trigger' />
        ))}
      </ApiKeyGroupSignalProvider>
    </QueryClientProvider>
  )
  return client
}

function statusResponse(...groups: GroupStatusEntry[]) {
  const result: GroupStatusResult = {
    window: 'now',
    window_minutes: 5,
    window_hours: 1,
    generated_at: 1,
    data_source: 'redis',
    redis_available: true,
    groups,
  }
  return { data: { success: true, data: result } }
}

afterEach(async () => {
  site.data.tenant_id = 0
  await i18next.changeLanguage('en')
})

describe('shared group signals', () => {
  it.each(['zhCN', 'zhTW'])(
    'renders the request count with the project locale %s',
    async (language) => {
      await i18next.changeLanguage(language)
      vi.spyOn(api, 'get').mockResolvedValue(
        statusResponse({ ...sample, request_count: 1234 })
      )
      renderSignals('auto-plus')
      expect(
        await screen.findByRole('img', { name: /1,234.*98\.5%/ })
      ).toBeInTheDocument()
    }
  )

  it('renders the latest 16 events and uses the 5-minute aggregate across all samples, including named auto groups', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue(
      statusResponse(sample, {
        ...sample,
        group: 'outage',
        confidence_status: 'unavailable',
        status: 'outage',
        success_rate: 0,
        recent_events: [],
      })
    )
    renderSignals('auto-plus', 'outage')

    const signal = await screen.findByRole('img', {
      name: /98.5%.*15 successful and 1 failed/,
    })
    expect(within(signal).getByText(/400/)).toBeInTheDocument()
    expect(
      [...signal.querySelectorAll('[data-result]')].map((tick) =>
        tick.getAttribute('data-result')
      )
    ).toEqual([...Array<string>(15).fill('success'), 'failure'])
    expect(
      screen.getByRole('img', { name: /Unavailable.*Success rate 0%/ })
    ).toHaveAttribute('data-tone', 'danger')
    expect(get).toHaveBeenCalledTimes(1)
    expect(get).toHaveBeenCalledWith('/api/status/groups', {
      params: { window: 'now' },
    })
  })

  it('retains actual historical events but hides stale, empty and unknown window rates', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(
      statusResponse(
        { ...sample, group: 'old', stale: true },
        { ...sample, group: 'quiet', request_count: 0, recent_events: [] },
        { ...sample, group: 'unknown', confidence_status: 'unknown' }
      )
    )
    renderSignals('old', 'quiet', 'unknown')
    expect(
      await screen.findByRole('img', { name: /Stale.*15 successful/ })
    ).toHaveAttribute('data-tone', 'neutral')
    expect(
      screen.getByRole('img', { name: 'Awaiting samples' })
    ).toHaveAttribute('data-tone', 'neutral')
    expect(screen.getByRole('img', { name: /Unknown/ })).toHaveAttribute(
      'data-tone',
      'neutral'
    )
    expect(screen.queryByText('98.5%')).not.toBeInTheDocument()
  })

  it('stops presenting the cached rate when refresh fails', async () => {
    vi.spyOn(api, 'get')
      .mockResolvedValueOnce(statusResponse(sample))
      .mockRejectedValue(new Error('Offline'))
    const client = renderSignals('auto-plus')
    await screen.findByText('98.5%')
    await act(() => client.refetchQueries({ type: 'active' }))
    const signal = await screen.findByRole('img', {
      name: 'Status unavailable',
    })
    expect(signal.querySelector('[data-result="success"]')).toBeNull()
    expect(screen.queryByText('98.5%')).not.toBeInTheDocument()
  })

  it('skips the platform-only status query on reseller sites', () => {
    site.data.tenant_id = 4
    const get = vi.spyOn(api, 'get')
    renderSignals('auto-plus')
    expect(get).not.toHaveBeenCalled()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
  })
})
