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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { ImageLibraryPage } from './image-library-page'
import type { ImageGeneration } from './types'

const mocks = vi.hoisted(() => ({
  userId: 1,
  query: {} as {
    data: { pages: { items: ImageGeneration[] }[] }
    isLoading: boolean
    isError: boolean
    isFetching: boolean
    hasNextPage: boolean
    isFetchingNextPage: boolean
    refetch: ReturnType<typeof vi.fn>
    fetchNextPage: ReturnType<typeof vi.fn>
  },
  deleteGeneration: vi.fn(),
  getGeneration: vi.fn(),
  mediaPaths: [] as readonly (string | undefined)[],
  mediaUrls: {} as Record<string, string>,
}))

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (
    select: (state: { auth: { user: { id: number } } }) => number
  ) => select({ auth: { user: { id: mocks.userId } } }),
}))
vi.mock('@/lib/studio-media', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/studio-media')>()),
  useStudioMediaUrls: (paths: readonly (string | undefined)[]) => {
    mocks.mediaPaths = paths
    return { data: { urls: mocks.mediaUrls } }
  },
}))
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  getImageGeneration: (id: number) => mocks.getGeneration(id),
}))
vi.mock('./queries', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./queries')>()),
  useImageGenerations: () => mocks.query,
  useDeleteImageGeneration: () => ({
    mutateAsync: mocks.deleteGeneration,
    isPending: false,
  }),
}))

let queryClient: QueryClient

function TestQueryProvider(props: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      {props.children}
    </QueryClientProvider>
  )
}
vi.mock('./components/image-studio-nav', () => ({
  ImageStudioNav: () => <header>Image Studio</header>,
}))
vi.mock('./components/image-regeneration-sheet', () => ({
  ImageRegenerationSheet: (props: {
    generation: ImageGeneration | null
    onSubmitted: (generation: ImageGeneration) => void
  }) =>
    props.generation ? (
      <section aria-label='Regeneration form'>
        <span>{props.generation.prompt}</span>
        <button
          type='button'
          onClick={() =>
            props.onSubmitted({
              ...(props.generation as ImageGeneration),
              id: 88,
              prompt: 'New private batch',
            })
          }
        >
          Submit new generation
        </button>
      </section>
    ) : null,
}))

const generation = (id: number): ImageGeneration => ({
  id,
  model_profile_id: 7,
  specification_version: 2,
  model: 'gpt-image-2',
  prompt: `Private image ${String(id)}`,
  parameters: {},
  request_id: `request-${String(id)}`,
  status: 'succeeded',
  requested_count: 1,
  succeeded_count: 1,
  final_quota: 100,
  started_at: id,
  finished_at: id,
  created_at: id,
  assets: [
    {
      id,
      position: 0,
      state: 'ready',
      thumbnail_state: 'ready',
      mime_type: 'image/png',
      size_bytes: 100,
      width: 768,
      height: 1024,
      content_url: `/private-${String(id)}.png`,
    },
  ],
})

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  mocks.userId = 1
  mocks.getGeneration
    .mockReset()
    .mockImplementation(async (id: number) => generation(id))
  mocks.mediaPaths = []
  mocks.mediaUrls = {}
  mocks.query = {
    data: { pages: [{ items: [generation(2), generation(1)] }] },
    isLoading: false,
    isError: false,
    isFetching: false,
    hasNextPage: true,
    isFetchingNextPage: false,
    refetch: vi.fn(),
    fetchNextPage: vi.fn(),
  }
})

afterEach(() => queryClient.clear())

describe('image library continuity and privacy', () => {
  test('defers new batches while reading below, but appends older pagination immediately', async () => {
    const { container, rerender } = render(<ImageLibraryPage />, {
      wrapper: TestQueryProvider,
    })
    const scroller = container.querySelector('.overflow-y-auto')
    expect(scroller).not.toBeNull()
    if (!scroller) return
    fireEvent.scroll(scroller, { target: { scrollTop: 600 } })

    mocks.query = {
      ...mocks.query,
      data: {
        pages: [
          { items: [generation(3), generation(2), generation(1)] },
          { items: [generation(0)] },
        ],
      },
    }
    rerender(<ImageLibraryPage />)
    expect(screen.queryByText('Private image 3')).not.toBeInTheDocument()
    expect(screen.getByText('Private image 0')).toBeVisible()
    expect(screen.getByRole('list')).toHaveTextContent('Private image 2')
    const scrollTo = vi.fn()
    scroller.scrollTo = scrollTo
    await userEvent.click(
      screen.getByRole('button', { name: 'Show 1 new batches' })
    )
    expect(screen.getByText('Private image 3')).toBeVisible()
    expect(scrollTo).toHaveBeenCalledWith({ top: 0, behavior: 'auto' })
  })

  test('account changes unmount an open private detail before rendering the next account', async () => {
    const { rerender } = render(<ImageLibraryPage />, {
      wrapper: TestQueryProvider,
    })
    await userEvent.click(
      screen.getAllByRole('button', { name: 'View image details' })[0]
    )
    expect(screen.getByRole('dialog', { name: 'Image details' })).toBeVisible()
    expect(
      within(screen.getByRole('dialog')).getByText('Private image 2')
    ).toBeVisible()
    mocks.userId = 2
    mocks.query = { ...mocks.query, data: { pages: [{ items: [] }] } }
    rerender(<ImageLibraryPage />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.queryByText('Private image 2')).not.toBeInTheDocument()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    const nextAccountGeneration = {
      ...generation(2),
      prompt: 'Next account image',
    }
    mocks.getGeneration.mockResolvedValue(nextAccountGeneration)
    mocks.query = {
      ...mocks.query,
      data: { pages: [{ items: [nextAccountGeneration] }] },
    }
    rerender(<ImageLibraryPage />)
    await userEvent.click(
      screen.getByRole('button', { name: 'View image details' })
    )
    expect(
      await within(screen.getByRole('dialog')).findByText('Next account image')
    ).toBeVisible()
    await waitFor(() => expect(mocks.getGeneration).toHaveBeenCalledTimes(2))
    expect(
      within(screen.getByRole('dialog')).queryByText('Private image 2')
    ).not.toBeInTheDocument()
  })

  test('account changes discard an open regeneration form', async () => {
    const { rerender } = render(<ImageLibraryPage />, {
      wrapper: TestQueryProvider,
    })
    await userEvent.click(
      screen.getAllByRole('button', { name: 'Generate again' })[0]
    )
    expect(
      screen.getByRole('region', { name: 'Regeneration form' })
    ).toBeVisible()
    mocks.userId = 2
    mocks.query = { ...mocks.query, data: { pages: [{ items: [] }] } }
    rerender(<ImageLibraryPage />)
    expect(
      screen.queryByRole('region', { name: 'Regeneration form' })
    ).not.toBeInTheDocument()
    expect(screen.queryByText('Private image 2')).not.toBeInTheDocument()
  })

  test('account changes discard a locally submitted result', async () => {
    const { rerender } = render(<ImageLibraryPage />, {
      wrapper: TestQueryProvider,
    })
    await userEvent.click(
      screen.getAllByRole('button', { name: 'Generate again' })[0]
    )
    expect(
      screen.getByRole('region', { name: 'Regeneration form' })
    ).toBeVisible()
    await userEvent.click(
      screen.getByRole('button', { name: 'Submit new generation' })
    )
    expect(screen.getByText('New private batch')).toBeVisible()
    mocks.userId = 2
    mocks.query = { ...mocks.query, data: { pages: [{ items: [] }] } }
    rerender(<ImageLibraryPage />)
    expect(
      screen.queryByRole('region', { name: 'Regeneration form' })
    ).not.toBeInTheDocument()
    expect(screen.queryByText('New private batch')).not.toBeInTheDocument()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
  })

  test('refreshes an open task by ID after completion removes it from the submitting filter and signs its new media', async () => {
    const pending: ImageGeneration = {
      ...generation(2),
      status: 'submitting',
      succeeded_count: 0,
      finished_at: 0,
      assets: [],
    }
    const completed = generation(2)
    completed.assets[0].content_url = '/api/image-studio/assets/2/content'
    completed.assets[0].download_url = '/api/image-studio/assets/2/download'
    mocks.mediaUrls = {
      '/api/image-studio/assets/2/content':
        'https://media.example/signed-image',
      '/api/image-studio/assets/2/download':
        'https://media.example/signed-download',
    }
    mocks.query.data = { pages: [{ items: [pending] }] }
    mocks.getGeneration.mockResolvedValue(pending)
    const { rerender } = render(<ImageLibraryPage />, {
      wrapper: TestQueryProvider,
    })
    await userEvent.selectOptions(
      screen.getByRole('combobox', { name: 'imageStudio.filterStatus' }),
      'submitting'
    )
    await userEvent.click(
      screen.getByRole('button', { name: 'View image details' })
    )
    const dialog = screen.getByRole('dialog', { name: 'Image details' })
    await waitFor(() =>
      expect(mocks.getGeneration).toHaveBeenCalledExactlyOnceWith(2)
    )
    await waitFor(() =>
      expect(
        within(dialog).getByRole('button', { name: 'Check status' })
      ).toBeEnabled()
    )
    mocks.query = { ...mocks.query, data: { pages: [{ items: [] }] } }
    mocks.getGeneration.mockResolvedValue(completed)
    rerender(<ImageLibraryPage />)
    await userEvent.click(
      within(dialog).getByRole('button', { name: 'Check status' })
    )

    expect(await within(dialog).findByRole('img')).toHaveAttribute(
      'src',
      'https://media.example/signed-image'
    )
    expect(
      within(dialog).getByRole('button', { name: 'imageStudio.download 1' })
    ).toHaveAttribute('href', 'https://media.example/signed-download')
    expect(mocks.mediaPaths).toContain(completed.assets[0].content_url)
    expect(mocks.mediaPaths).toContain(completed.assets[0].download_url)
    expect(
      within(dialog).getByRole('button', { name: 'Generate again' })
    ).toBeEnabled()
    expect(
      within(dialog).getByRole('button', { name: 'Delete this batch' })
    ).toBeEnabled()
    expect(
      within(dialog).queryByRole('button', { name: 'Check status' })
    ).not.toBeInTheDocument()
    expect(mocks.query.refetch).not.toHaveBeenCalled()
    expect(mocks.getGeneration).toHaveBeenCalledTimes(2)
  })

  test('polls an open submitting task outside its filter, stops on close, and refetches when reopened', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    try {
      const pending: ImageGeneration = {
        ...generation(2),
        status: 'submitting',
        succeeded_count: 0,
        started_at: Math.floor(Date.now() / 1000),
        finished_at: 0,
        assets: [],
      }
      mocks.query.data = { pages: [{ items: [pending] }] }
      mocks.getGeneration.mockResolvedValue(pending)
      const { rerender } = render(<ImageLibraryPage />, {
        wrapper: TestQueryProvider,
      })
      await userEvent.selectOptions(
        screen.getByRole('combobox', { name: 'imageStudio.filterStatus' }),
        'submitting'
      )
      await userEvent.click(
        screen.getByRole('button', { name: 'View image details' })
      )
      await waitFor(() => expect(mocks.getGeneration).toHaveBeenCalledTimes(1))
      await waitFor(() =>
        expect(
          screen.getByRole('button', { name: 'Check status' })
        ).toBeEnabled()
      )
      mocks.query = { ...mocks.query, data: { pages: [{ items: [] }] } }
      rerender(<ImageLibraryPage />)
      await act(() => vi.advanceTimersByTime(3000))
      await waitFor(() => expect(mocks.getGeneration).toHaveBeenCalledTimes(2))
      await userEvent.click(screen.getByRole('button', { name: 'Close' }))
      await act(() => vi.advanceTimersByTime(30_000))
      expect(mocks.getGeneration).toHaveBeenCalledTimes(2)

      mocks.getGeneration.mockResolvedValue(generation(2))
      mocks.query = { ...mocks.query, data: { pages: [{ items: [pending] }] } }
      rerender(<ImageLibraryPage />)
      await userEvent.click(
        screen.getByRole('button', { name: 'View image details' })
      )
      expect(
        await within(screen.getByRole('dialog')).findByRole('img')
      ).toHaveAttribute('src', '/private-2.png')
      expect(mocks.getGeneration).toHaveBeenCalledTimes(3)
      await act(() => vi.advanceTimersByTime(30_000))
      expect(mocks.getGeneration).toHaveBeenCalledTimes(3)
    } finally {
      vi.useRealTimers()
    }
  })
})
