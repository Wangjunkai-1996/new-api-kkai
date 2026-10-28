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
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import type { VideoGeneration } from '../types'
import { VideoGenerationDetails } from './video-generation-details'
import { VideoGenerationPlayer } from './video-generation-player'
import { VideoGenerationStatus } from './video-generation-status'

const generation: VideoGeneration = {
  id: 1,
  task_id: 'task_one',
  model_profile_id: 1,
  model: 'video-model',
  mode: 'text_to_video',
  prompt: 'A quiet coast',
  parameters: {},
  status: 'ready',
  progress: '100%',
  quota: 1,
  output_asset_id: 1,
  video_url: 'https://media.test/first',
  poster_url: 'https://media.test/poster',
  created_at: 1,
  updated_at: 1,
}
beforeEach(() => {
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
  vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {})
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue()
})
describe('Studio generation playback', () => {
  test('waits for the initial signature before offering playback or reporting an error', () => {
    const unsigned = { ...generation, video_url: undefined }
    const view = render(
      <VideoGenerationPlayer generation={unsigned} active={false} loading />
    )
    expect(screen.getByRole('status')).toHaveTextContent('Loading...')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Play video' })
    ).not.toBeInTheDocument()

    view.rerender(
      <VideoGenerationPlayer generation={generation} active={false} />
    )
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Play video' })).toBeVisible()

    view.unmount()
    render(<VideoGenerationPlayer generation={unsigned} active />)
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Video preview is unavailable'
    )
  })
  test('keeps a healthy source and position across background signature renewal', () => {
    const view = render(
      <VideoGenerationPlayer
        generation={generation}
        active
        mediaIdentity='user-one:1:1'
      />
    )
    const video = screen.getByLabelText('Generated video') as HTMLVideoElement
    video.currentTime = 7
    expect(video.autoplay).toBe(false)
    view.rerender(
      <VideoGenerationPlayer
        generation={{ ...generation, video_url: 'https://media.test/renewed' }}
        active
        mediaIdentity='user-one:1:1'
      />
    )
    expect(screen.getByLabelText('Generated video')).toBe(video)
    expect(video).toHaveAttribute('src', generation.video_url)
    expect(video.currentTime).toBe(7)
    expect(video.load).not.toHaveBeenCalled()
  })
  test('renews once even with the same URL, then requires manual retry', async () => {
    const renew = vi.fn().mockResolvedValue(generation.video_url)
    render(
      <VideoGenerationPlayer generation={generation} active onRefresh={renew} />
    )
    const first = screen.getByLabelText('Generated video')
    fireEvent.error(first)
    await waitFor(() =>
      expect(screen.getByLabelText('Generated video')).not.toBe(first)
    )
    expect(renew).toHaveBeenCalledTimes(1)
    fireEvent.error(screen.getByLabelText('Generated video'))
    expect(screen.queryByLabelText('Generated video')).not.toBeInTheDocument()
    expect(screen.getByText('Video preview is unavailable')).toBeVisible()
    expect(renew).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole('button', { name: 'Retry preview' }))
    await waitFor(() =>
      expect(screen.getByLabelText('Generated video')).toBeInTheDocument()
    )
    expect(renew).toHaveBeenCalledTimes(2)
  })
  test('revocation removes a healthy source and poster', () => {
    const view = render(
      <VideoGenerationPlayer generation={generation} active />
    )
    const video = screen.getByLabelText('Generated video')
    view.rerender(
      <VideoGenerationPlayer generation={generation} active denied />
    )
    expect(video).not.toHaveAttribute('src')
    expect(screen.queryByLabelText('Generated video')).not.toBeInTheDocument()
    expect(view.container.querySelector('img')).toBeNull()
  })
  test('late renewal cannot reactivate media after another preview becomes active', async () => {
    let finish!: (url: string) => void
    const renew = vi.fn(
      () =>
        new Promise<string>((resolve) => {
          finish = resolve
        })
    )
    const view = render(
      <VideoGenerationPlayer generation={generation} active onRefresh={renew} />
    )
    fireEvent.error(screen.getByLabelText('Generated video'))
    view.rerender(
      <VideoGenerationPlayer
        generation={generation}
        active={false}
        onRefresh={renew}
      />
    )
    await act(async () => finish('https://media.test/late'))
    expect(view.container.querySelector('video')).toBeNull()
  })
  test('account identity replaces the source even with identical asset IDs', () => {
    const view = render(
      <VideoGenerationPlayer
        generation={generation}
        active
        mediaIdentity='user-one:1:1'
      />
    )
    const old = screen.getByLabelText('Generated video')
    view.rerender(
      <VideoGenerationPlayer
        generation={{ ...generation, video_url: 'https://media.test/user-two' }}
        active
        mediaIdentity='user-two:1:1'
      />
    )
    expect(old).not.toHaveAttribute('src')
    expect(screen.getByLabelText('Generated video')).toHaveAttribute(
      'src',
      'https://media.test/user-two'
    )
  })
  test('closing details stops the video even while the dialog exit animation is running', () => {
    const props = { generation, open: true, onOpenChange: vi.fn() }
    const view = render(<VideoGenerationDetails {...props} />)
    const video = screen.getByLabelText('Generated video')
    view.rerender(<VideoGenerationDetails {...props} open={false} />)
    expect(video).not.toHaveAttribute('src')
    expect(document.querySelector('video')).toBeNull()
  })
  test('details gates playback and deletion on real terminal states', () => {
    const onDelete = vi.fn()
    const pending = {
      ...generation,
      status: 'archiving' as const,
      download_url: 'https://media.test/download',
    }
    render(
      <VideoGenerationDetails
        generation={pending}
        open
        onOpenChange={vi.fn()}
        onDelete={onDelete}
      />
    )
    expect(screen.queryByLabelText('Generated video')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Delete video' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('link', { name: 'Download video' })
    ).not.toBeInTheDocument()
  })
  test.each(['', '0', 'unknown', '-1', '120%', '20oops'])(
    'does not invent progress from %j',
    (progress) => {
      render(
        <VideoGenerationStatus
          generation={{ ...generation, status: 'processing', progress }}
        />
      )
      expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
      expect(screen.getByText('Generating video')).toBeVisible()
    }
  )
  test('shows a valid upstream progress value', () => {
    render(
      <VideoGenerationStatus
        generation={{ ...generation, status: 'processing', progress: '42%' }}
      />
    )
    expect(screen.getByRole('progressbar')).toHaveAttribute('value', '42')
  })
})
