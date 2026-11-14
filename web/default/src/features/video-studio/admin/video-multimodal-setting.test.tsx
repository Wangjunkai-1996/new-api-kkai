import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { STATUS_QUERY_KEY } from '@/lib/status-query'
import { useAuthStore } from '@/stores/auth-store'

import { VideoMultimodalSetting } from './video-multimodal-setting'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

let effective: boolean | undefined
let client: QueryClient

beforeEach(() => {
  vi.useFakeTimers()
  effective = false
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  })
  useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role: 100 })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    expect(url).toBe('/api/status')
    return {
      data: {
        success: true,
        data: {
          video_studio:
            effective === undefined ? {} : { multimodal_enabled: effective },
        },
      },
    }
  })
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
})

afterEach(() => {
  cleanup()
  client.clear()
  useAuthStore.getState().auth.reset()
  window.localStorage.clear()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

async function renderSetting() {
  render(
    <QueryClientProvider client={client}>
      <VideoMultimodalSetting />
    </QueryClientProvider>
  )
  await act(() => vi.advanceTimersByTimeAsync(1))
}

async function toggleSetting() {
  fireEvent.click(
    screen.getByRole('switch', { name: 'Multimodal video references' })
  )
  await act(() => vi.advanceTimersByTimeAsync(1))
}

test('ordinary administrators cannot see or operate the global switch', async () => {
  useAuthStore.getState().auth.setUser({ id: 2, username: 'admin', role: 10 })
  await renderSetting()
  expect(screen.queryByRole('switch')).not.toBeInTheDocument()
  expect(api.get).not.toHaveBeenCalled()
  expect(api.put).not.toHaveBeenCalled()
})

test('a cached enabled flag is replaced by a fresh status before editing', async () => {
  client.setQueryData(STATUS_QUERY_KEY, {
    video_studio: { multimodal_enabled: true },
  })
  await renderSetting()
  expect(api.get).toHaveBeenCalledWith('/api/status')
  expect(screen.getByRole('switch')).not.toBeChecked()
  expect(screen.getByRole('switch')).not.toHaveAttribute(
    'aria-disabled',
    'true'
  )
  expect(screen.getByRole('status')).toHaveTextContent('Disabled')
})

test.each([false, true])(
  'saving from %s stays pending until the service reports the requested value',
  async (initial) => {
    effective = initial
    await renderSetting()
    await toggleSetting()
    expect(api.put).toHaveBeenCalledWith('/api/option/', {
      key: 'video_studio.multimodal_enabled',
      value: String(!initial),
    })
    expect(screen.getByRole('switch')).toHaveAttribute(
      'aria-checked',
      String(initial)
    )
    expect(screen.getByRole('switch')).toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByRole('status')).toHaveTextContent(
      'Saved. Waiting for the running service to apply this setting...'
    )
    await act(() => vi.advanceTimersByTimeAsync(57_000))
    expect(screen.getByRole('switch')).toHaveAttribute('aria-disabled', 'true')
    effective = !initial
    await act(() => vi.advanceTimersByTimeAsync(3001))
    expect(screen.getByRole('switch')).toHaveAttribute(
      'aria-checked',
      String(!initial)
    )
    expect(screen.getByRole('switch')).not.toHaveAttribute(
      'aria-disabled',
      'true'
    )
    expect(screen.getByRole('status')).toHaveTextContent(
      initial ? 'Disabled' : 'Enabled'
    )
    effective = initial
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    await act(() => vi.advanceTimersByTimeAsync(1))
    expect(screen.getByRole('switch')).not.toHaveAttribute(
      'aria-disabled',
      'true'
    )
    expect(screen.getByRole('status')).toHaveTextContent(
      initial ? 'Enabled' : 'Disabled'
    )
  }
)

test.each([false, true])(
  'polling stops after 75 seconds and a manual %s status restores editing',
  async (refreshedValue) => {
    await renderSetting()
    await toggleSetting()
    await act(() => vi.advanceTimersByTimeAsync(75_001))
    expect(screen.getByRole('status')).toHaveTextContent(
      'Saved, but the running service has not confirmed this setting. Refresh the status before trying again.'
    )
    expect(screen.getByRole('switch')).not.toBeChecked()
    expect(screen.getByRole('switch')).toHaveAttribute('aria-disabled', 'true')
    const polls = vi.mocked(api.get).mock.calls.length
    await act(() => vi.advanceTimersByTimeAsync(9000))
    expect(api.get).toHaveBeenCalledTimes(polls)
    effective = refreshedValue
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    await act(() => vi.advanceTimersByTimeAsync(1))
    expect(screen.getByRole('switch')).toHaveAttribute(
      'aria-checked',
      String(refreshedValue)
    )
    expect(screen.getByRole('switch')).not.toHaveAttribute(
      'aria-disabled',
      'true'
    )
    expect(screen.getByRole('status')).toHaveTextContent(
      refreshedValue ? 'Enabled' : 'Disabled'
    )
    await toggleSetting()
    expect(api.put).toHaveBeenLastCalledWith('/api/option/', {
      key: 'video_studio.multimodal_enabled',
      value: String(!refreshedValue),
    })
  }
)

test('an immediately updated instance does not confirm before the 60 second sync window', async () => {
  vi.mocked(api.put).mockImplementation(async () => {
    effective = true
    return { data: { success: true } }
  })
  await renderSetting()
  await toggleSetting()
  await act(() => vi.advanceTimersByTimeAsync(59_000))
  expect(screen.getByRole('switch')).toBeChecked()
  expect(screen.getByRole('switch')).toHaveAttribute('aria-disabled', 'true')
  expect(screen.getByRole('status')).toHaveTextContent(
    'Saved. Waiting for the running service to apply this setting...'
  )
  await act(() => vi.advanceTimersByTimeAsync(1001))
  expect(screen.getByRole('switch')).not.toHaveAttribute(
    'aria-disabled',
    'true'
  )
  expect(screen.getByRole('status')).toHaveTextContent('Enabled')
})

test('a rejected option update leaves the effective flag unchanged and does not poll', async () => {
  vi.mocked(api.put).mockResolvedValue({
    data: { success: false, message: 'The setting could not be saved.' },
  })
  await renderSetting()
  await toggleSetting()
  expect(screen.getByRole('alert')).toHaveTextContent(
    'The setting could not be saved.'
  )
  expect(screen.getByRole('switch')).not.toBeChecked()
  expect(screen.getByRole('switch')).not.toHaveAttribute(
    'aria-disabled',
    'true'
  )
  await act(() => vi.advanceTimersByTimeAsync(9000))
  expect(api.get).toHaveBeenCalledTimes(1)
})

test('a backend without an explicit multimodal flag cannot be edited', async () => {
  effective = undefined
  await renderSetting()
  expect(screen.getByRole('switch')).toHaveAttribute('aria-disabled', 'true')
  expect(screen.getByRole('status')).toHaveTextContent('Loading failed')
  expect(api.put).not.toHaveBeenCalled()
})
