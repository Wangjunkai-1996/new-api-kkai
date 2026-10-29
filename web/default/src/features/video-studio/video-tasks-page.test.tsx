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
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import type { ComponentProps } from 'react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import type { VideoGeneration } from './types'
import { VideoTasksPage } from './video-tasks-page'

const mocks = vi.hoisted(() => ({
  query: {} as Record<string, unknown>,
  media: {
    data: { urls: {} as Record<string, string>, deniedPaths: {} },
    identityDenied: false,
  },
  resize: undefined as ((width: number) => void) | undefined,
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, unknown>) =>
      key.replaceAll(/{{(\w+)}}/g, (_, name: string) =>
        String(values?.[name] ?? '')
      ),
    i18n: { language: 'en' },
  }),
}))
vi.mock('@tanstack/react-router', async (importOriginal) => {
  const original =
    await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...original,
    Link: (
      props: ComponentProps<'a'> & {
        to: string
        search?: { task?: string; generation?: number }
      }
    ) => {
      const { to, search, ...anchorProps } = props
      const params = new URLSearchParams()
      if (search?.task) params.set('task', search.task)
      if (search?.generation) {
        params.set('generation', String(search.generation))
      }
      return <a {...anchorProps} href={`${to}?${params}`} />
    },
  }
})
vi.mock('./components/video-studio-nav', () => ({
  VideoStudioNav: () => <nav>Studio</nav>,
}))
vi.mock('./queries', () => ({
  useVideoGenerations: () => mocks.query,
}))
vi.mock('@/lib/studio-media', async (importOriginal) => {
  const original = await importOriginal<typeof import('@/lib/studio-media')>()
  return {
    ...original,
    useStudioMediaUrls: () => mocks.media,
  }
})

const generation = (
  id: number,
  status: VideoGeneration['status'] = 'ready'
): VideoGeneration => ({
  id,
  task_id: `task_${id}`,
  model_profile_id: 1,
  model: `model-${id}`,
  mode: 'text_to_video',
  prompt: `Prompt for task ${id}`,
  parameters: { ratio: '16:9', duration: 5 },
  status,
  progress: '',
  quota: 0,
  poster_url: `/api/video-studio/assets/${id}/poster`,
  created_at: 1,
  updated_at: 1,
})

beforeEach(() => {
  mocks.query = {
    items: [generation(2), generation(1, 'failed')],
    isSuccess: true,
    isError: false,
    isLoading: false,
    hasNextPage: false,
    refresh: vi.fn(),
  }
  mocks.media = {
    data: {
      urls: {
        '/api/video-studio/assets/2/poster': '/signed-poster-2',
      },
      deniedPaths: {},
    },
    identityDenied: false,
  }
  mocks.resize = undefined
  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(callback: ResizeObserverCallback) {
        mocks.resize = (width) =>
          callback(
            [{ contentRect: { width } } as ResizeObserverEntry],
            this as ResizeObserver
          )
      }
      observe() {}
      unobserve() {}
      disconnect() {}
    }
  )
})

afterEach(() => vi.unstubAllGlobals())

describe('video task workspace', () => {
  test('selecting a task updates the detail without losing its task link', () => {
    render(<VideoTasksPage />)
    const detail = screen.getByRole('region', { name: 'Task Details' })
    expect(within(detail).getByText('Prompt for task 2')).toBeInTheDocument()
    expect(
      within(detail).getByRole('button', { name: 'View result' })
    ).toHaveAttribute('href', '/video-studio/library?task=task_2&generation=2')

    fireEvent.click(
      within(screen.getByRole('region', { name: 'Recent tasks' })).getByRole(
        'button',
        { name: /Prompt for task 1/ }
      )
    )
    expect(within(detail).getByText('Prompt for task 1')).toBeInTheDocument()
    expect(
      within(detail).getByRole('button', { name: 'View task' })
    ).toHaveAttribute('href', '/video-studio/create?task=task_1&generation=1')
    expect(
      within(detail).getByText('videoStudio.failure.generic')
    ).toBeInTheDocument()
  })

  test('a compact task opens its selected detail and can close it', () => {
    render(<VideoTasksPage />)
    act(() => mocks.resize?.(800))
    fireEvent.click(
      within(screen.getByRole('region', { name: 'Recent tasks' })).getByRole(
        'button',
        { name: /Prompt for task 1/ }
      )
    )
    const sheet = screen.getByRole('dialog', { name: 'Task Details' })
    expect(within(sheet).getByText('Prompt for task 1')).toBeInTheDocument()
    expect(
      within(sheet).getByRole('button', { name: 'View task' })
    ).toHaveAttribute('href', '/video-studio/create?task=task_1&generation=1')
    fireEvent.click(within(sheet).getByRole('button', { name: 'Close' }))
    expect(
      screen.queryByRole('dialog', { name: 'Task Details' })
    ).not.toBeInTheDocument()
  })

  test('identity denial removes signed poster and preview action', () => {
    const view = render(<VideoTasksPage />)
    expect(screen.getAllByAltText('Video cover')).toHaveLength(1)
    mocks.media = { ...mocks.media, identityDenied: true }
    view.rerender(<VideoTasksPage />)
    expect(screen.queryByAltText('Video cover')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Enlarge video cover' })
    ).not.toBeInTheDocument()
    expect(view.container.querySelector('[src="/signed-poster-2"]')).toBeNull()
  })
})
