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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { describe, expect, test, vi } from 'vitest'

import zh from '@/i18n/locales/zh.json'

import type { TaskArtifactProjection, TaskLog } from '../types'
import { VideoWorkItem } from './video-work-item'
import { VideoWorkPreview } from './video-work-preview'

const mediaQuery = vi.hoisted(() => ({
  data: undefined as TaskArtifactProjection | undefined,
  isLoading: false,
  isFetching: false,
  error: null,
  refetch: vi.fn(),
  renewMedia: vi.fn(),
}))
const useArtifacts = vi.hoisted(() => vi.fn())
vi.mock('./use-video-work-artifacts', () => ({
  useVideoWorkArtifacts: (...args: unknown[]) => {
    useArtifacts(...args)
    return mediaQuery
  },
}))
vi.mock('./use-video-works-identity', () => ({
  useVideoWorksIdentity: () => ({ ready: false, active: false }),
}))

function task(overrides: Partial<TaskLog> = {}): TaskLog {
  return {
    id: 17,
    user_id: 8,
    task_id: 'task-video-ui',
    platform: 'sora',
    action: 'text_to_video',
    channel_id: 3,
    group: 'default',
    quota: 6400,
    submit_time: 0,
    status: 'SUCCESS',
    properties: { origin_model_name: 'sora-2' },
    billing: { amount_available: true, state: 'completed' },
    ...overrides,
  }
}

const actions = { onPreview: vi.fn(), onBilling: vi.fn(), onDownload: vi.fn() }

describe('video work UI', () => {
  test('exposes the model, independent billing state and distinct result actions', async () => {
    const user = userEvent.setup()
    render(<VideoWorkItem log={task()} isAdmin={false} {...actions} />)
    expect(screen.getByRole('heading', { name: 'sora-2' })).toBeVisible()
    expect(screen.getByText('Billing processed')).toBeVisible()
    expect(screen.queryByText('Settled')).not.toBeInTheDocument()
    expect(screen.queryByText(/1970/)).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Billing Details' }))
    expect(actions.onBilling).toHaveBeenCalledOnce()
    await user.click(screen.getByRole('button', { name: 'Download video' }))
    expect(actions.onDownload).toHaveBeenCalledOnce()
  })

  test('does not turn phase progress into a percentage or unknown money into zero', () => {
    render(
      <VideoWorkItem
        log={task({
          status: 'IN_PROGRESS',
          progress: '68%',
          billing: { amount_available: false, state: 'reserved' },
        })}
        isAdmin={false}
        {...actions}
      />
    )
    expect(screen.getByText('Generating')).toBeVisible()
    expect(screen.getAllByText('—')).not.toHaveLength(0)
    expect(screen.queryByText('68%')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Download video' })
    ).not.toBeInTheDocument()
  })

  test('uses a real ready poster and never exposes admin metadata in self view', () => {
    const view = render(
      <VideoWorkItem
        log={task({
          username: 'another-user',
          media: {
            source: 'studio',
            state: 'ready',
            poster_url: 'https://storage.example/poster.jpg',
            poster_expires_at: 2_000_000_000,
          },
        })}
        isAdmin={false}
        {...actions}
      />
    )
    expect(view.container.querySelector('img')).toHaveAttribute(
      'src',
      'https://storage.example/poster.jpg'
    )
    expect(screen.queryByText(/another-user/)).not.toBeInTheDocument()
    expect(screen.queryByText(/Channel #3/)).not.toBeInTheDocument()
  })

  test('restricted media keeps task details available without requesting artifacts', () => {
    mediaQuery.data = undefined
    render(
      <VideoWorkPreview
        log={task({ media: { source: 'studio', state: 'restricted' } })}
        isAdmin={false}
        isRoot={false}
        onClose={vi.fn()}
        onBilling={vi.fn()}
      />
    )
    expect(useArtifacts).toHaveBeenLastCalledWith(
      expect.objectContaining({ id: 17 }),
      false
    )
    expect(screen.getByText('Media access unavailable')).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Download video' })
    ).not.toBeInTheDocument()
  })

  test('temporary unavailable media can request an authorized projection and retry', async () => {
    mediaQuery.data = {
      artifacts: [],
      hostedVideo: { task_record_id: 17, state: 'unavailable' },
    }
    mediaQuery.refetch.mockResolvedValueOnce({ error: null })
    render(
      <VideoWorkPreview
        log={task({ media: { source: 'studio', state: 'unavailable' } })}
        isAdmin={false}
        isRoot={false}
        onClose={vi.fn()}
        onBilling={vi.fn()}
      />
    )
    expect(useArtifacts).toHaveBeenLastCalledWith(
      expect.objectContaining({ id: 17 }),
      true
    )
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(mediaQuery.refetch).toHaveBeenCalledOnce()
  })

  test('renewing a hosted URL remounts a failed player even when the URL stays the same', async () => {
    mediaQuery.data = {
      artifacts: [],
      hostedVideo: {
        task_record_id: 17,
        state: 'ready',
        content_url: 'https://storage.example/video.mp4?signature=abc',
        download_url: 'https://storage.example/download.mp4?signature=def',
        expires_at: 2_000_000_000,
      },
    }
    mediaQuery.renewMedia.mockResolvedValueOnce(true)
    const view = render(
      <VideoWorkPreview
        log={task()}
        isAdmin={false}
        isRoot={false}
        onClose={vi.fn()}
        onBilling={vi.fn()}
      />
    )
    const original =
      view.container.querySelector('video') || document.querySelector('video')
    expect(original).not.toBeNull()
    if (!original) throw new Error('Expected video player')
    fireEvent.error(original)
    await waitFor(() =>
      expect(document.querySelector('video')).not.toBe(original)
    )
    expect(
      screen.getByRole('button', { name: 'Download video' })
    ).toHaveAttribute(
      'href',
      'https://storage.example/download.mp4?signature=def'
    )
  })
})

test('failure details show Chinese explanation and safe original text without a video placeholder or invented refund', async () => {
  i18next.addResourceBundle('zh', 'translation', zh.translation)
  await i18next.changeLanguage('zh')
  const message =
    "The request failed because the input image 'content[1]' may contain real person."
  try {
    render(
      <VideoWorkPreview
        log={task({
          status: 'FAILURE',
          failure: {
            code: 'privacy_restriction',
            message,
            stage: 'submission',
          },
          fail_reason: 'task submission failed',
          billing: { amount_available: false, state: 'unknown' },
        })}
        isAdmin={false}
        isRoot={false}
        onClose={vi.fn()}
        onBilling={vi.fn()}
      />
    )
    expect(
      screen.getByRole('heading', { name: '输入素材触发隐私限制' })
    ).toBeVisible()
    expect(screen.getByText(message)).toBeVisible()
    expect(
      screen.getByText(
        '上游判断输入素材可能包含真人或受限的隐私内容，因此拒绝生成。'
      )
    ).toBeVisible()
    expect(screen.getAllByText('—')).not.toHaveLength(0)
    expect(screen.queryByText('已退款')).not.toBeInTheDocument()
    expect(screen.queryByText('task submission failed')).not.toBeInTheDocument()
    expect(document.querySelector('video')).toBeNull()
    expect(document.querySelector('.video-work-player-state')).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: '复制错误详情' }))
    expect(screen.getByRole('button', { name: '已复制' })).toBeVisible()
  } finally {
    await i18next.changeLanguage('en')
  }
})

test('legacy failure diagnostics are not shown without a safe server projection', () => {
  render(
    <VideoWorkPreview
      log={task({
        status: 'FAILURE',
        fail_reason:
          'task submission failed Authorization: Bearer secret-value',
        failure: undefined,
      })}
      isAdmin={false}
      isRoot={false}
      onClose={vi.fn()}
      onBilling={vi.fn()}
    />
  )
  expect(
    screen.getByText('Detailed failure information is unavailable')
  ).toBeVisible()
  expect(screen.queryByText(/secret-value/)).not.toBeInTheDocument()
})

test('a missing API cover retains video playback and download while displaying the processing state', () => {
  render(
    <VideoWorkItem
      log={task({ poster: { state: 'pending' } })}
      isAdmin={false}
      {...actions}
    />
  )
  expect(screen.getByText('Preparing cover')).toBeVisible()
  expect(screen.getByRole('button', { name: 'Download video' })).toBeEnabled()
  expect(document.querySelector('video')).toBeNull()
})
