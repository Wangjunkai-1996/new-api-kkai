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
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import type { VideoGeneration } from './types'
import { VideoLibraryPage } from './video-library-page'

const mocks = vi.hoisted(() => ({
  deleteVideo: vi.fn(),
  query: {} as Record<string, unknown>,
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
vi.mock('./components/video-studio-nav', () => ({
  VideoStudioNav: () => <nav>Studio</nav>,
}))
vi.mock('./queries', () => ({
  useVideoGenerations: () => mocks.query,
  useDeleteVideoGeneration: () => ({
    mutateAsync: mocks.deleteVideo,
    isPending: false,
  }),
  isVideoGenerationTombstoneError: () => false,
}))
vi.mock('@/lib/studio-media', () => ({
  useStudioMediaUrls: () => ({
    data: { urls: {}, deniedPaths: {} },
    identityDenied: false,
    refetch: vi.fn(),
  }),
  signedStudioMediaUrl: () => undefined,
}))

const generation = (id: number): VideoGeneration => ({
  id,
  task_id: `task_${id}`,
  model_profile_id: 1,
  model: 'video',
  mode: 'text_to_video',
  prompt: `Video ${id}`,
  parameters: {},
  status: 'ready',
  progress: '',
  quota: 0,
  created_at: 1,
  updated_at: 1,
})

beforeEach(() => {
  const items = [generation(2), generation(1)]
  mocks.query = {
    items,
    historyItems: items,
    data: { pages: [{ items }] },
    isSuccess: true,
    isError: false,
    isLoading: false,
    targetUnavailable: false,
    targetDiscoveryExpired: false,
    refresh: vi.fn(),
    retryTargetDiscovery: vi.fn(),
    forgetObservedGeneration: vi.fn(async (id: number) => {
      mocks.query.items = items.filter((item) => item.id !== id)
    }),
  }
  mocks.deleteVideo.mockReset()
})

describe('library deletion', () => {
  test('successful terminal deletion removes the card and restores focus to its neighbor', async () => {
    mocks.deleteVideo.mockResolvedValue(undefined)
    render(<VideoLibraryPage />)
    const triggers = screen.getAllByRole('button', {
      name: 'videoStudio.delete',
    })
    triggers[0].focus()
    fireEvent.click(triggers[0])
    const dialog = await screen.findByRole('alertdialog')
    const confirm = within(dialog).getByRole('button', {
      name: 'videoStudio.delete',
    })
    fireEvent.click(confirm)
    await waitFor(() =>
      expect(
        screen.queryByRole('button', { name: 'View video details: Video 2' })
      ).not.toBeInTheDocument()
    )
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'View video details: Video 1' })
      ).toHaveFocus()
    )
  })

  test('rechecks the latest task state before confirming deletion', async () => {
    const { rerender } = render(<VideoLibraryPage />)
    fireEvent.click(
      screen.getAllByRole('button', { name: 'videoStudio.delete' })[0]
    )
    await screen.findByRole('alertdialog')
    mocks.query.items = [
      { ...generation(2), status: 'processing' },
      generation(1),
    ]
    rerender(<VideoLibraryPage />)
    fireEvent.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'videoStudio.delete',
      })
    )
    expect(mocks.deleteVideo).not.toHaveBeenCalled()
    expect(mocks.query.refresh).toHaveBeenCalled()
  })

  test('failed deletion leaves both the confirmation and the original card available', async () => {
    mocks.deleteVideo.mockRejectedValue(new Error('Unavailable'))
    render(<VideoLibraryPage />)
    fireEvent.click(
      screen.getAllByRole('button', { name: 'videoStudio.delete' })[0]
    )
    const dialog = await screen.findByRole('alertdialog')
    fireEvent.click(
      within(dialog).getByRole('button', { name: 'videoStudio.delete' })
    )
    await waitFor(() => expect(mocks.deleteVideo).toHaveBeenCalledWith(2))
    expect(screen.getByRole('alertdialog')).toBeInTheDocument()
    expect(screen.getByText('Video 2', { selector: 'h3' })).toBeInTheDocument()
  })
})
