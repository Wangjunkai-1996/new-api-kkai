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
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { ImageCreatePage } from './image-create-page'
import { imageGenerationQueryOptions } from './queries'
import type { ImageGeneration, ImageGenerationStatus } from './types'

const mocks = vi.hoisted(() => ({
  userId: 1,
  submitted: {} as ImageGeneration,
  getGeneration: vi.fn(),
  getInput: vi.fn(),
  output: vi.fn(),
}))
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))
vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (
    select: (state: { auth: { user: { id: number } } }) => number
  ) => select({ auth: { user: { id: mocks.userId } } }),
}))
vi.mock('./api', async (original) => ({
  ...(await original<typeof import('./api')>()),
  getImageGeneration: (id: number) => mocks.getGeneration(id),
  getImageGenerationInput: (id: number) => mocks.getInput(id),
}))
vi.mock('./queries', async (original) => ({
  ...(await original<typeof import('./queries')>()),
  useImageSample: () => ({ data: undefined }),
}))
vi.mock('./hooks/use-image-token-gate', () => ({
  useImageTokenGate: () => ({
    tokenId: 3,
    capability: { max_reference_bytes: 1024, max_reference_total_bytes: 2048 },
  }),
}))
vi.mock('./image-output-reference', async (original) => ({
  ...(await original<typeof import('./image-output-reference')>()),
  getImageOutputReference: (...args: unknown[]) => mocks.output(...args),
}))
vi.mock('./components/image-studio-nav', () => ({
  ImageStudioNav: () => <header>Image Studio</header>,
}))
vi.mock('./components/image-sample-gallery', () => ({
  ImageSampleGallery: () => <section>Sample gallery</section>,
}))
vi.mock('./components/image-composer', () => ({
  ImageComposer: (props: {
    loadRequest?: {
      kind: string
      generation?: ImageGeneration
      revision: number
    }
    onSubmitted: (generation: ImageGeneration) => void
    onEditorRevisionChange: (revision: number) => void
  }) => {
    const [draft, setDraft] = useState('')
    return (
      <section aria-label='Creation form'>
        <input
          aria-label='Next draft'
          value={draft}
          onChange={(event) => {
            setDraft(event.target.value)
            props.onEditorRevisionChange(event.target.value.length)
          }}
        />
        <button
          type='button'
          onClick={() => props.onSubmitted(mocks.submitted)}
        >
          Generate test image
        </button>
        {props.loadRequest && (
          <p>
            Loaded {props.loadRequest.kind} {props.loadRequest.generation?.id}
          </p>
        )}
      </section>
    )
  },
}))
vi.mock('./components/image-creation-results', () => ({
  ImageCreationResults: (props: {
    generation?: ImageGeneration
    history: ImageGeneration[]
    onSelectGeneration: (id: number) => void
    onRegenerate: (generation: ImageGeneration) => void
    onEditAsset: (
      asset: ImageGeneration['assets'][number],
      generation: ImageGeneration
    ) => void
  }) => (
    <section aria-label='Results'>
      <p>
        {props.generation
          ? `${props.generation.prompt}: ${props.generation.status}`
          : 'No current result'}
      </p>
      {props.history.map((generation) => (
        <button
          type='button'
          key={generation.id}
          onClick={() => props.onSelectGeneration(generation.id)}
        >
          View {generation.id}
        </button>
      ))}
      {props.generation && (
        <>
          <button
            type='button'
            onClick={() => {
              if (props.generation) props.onRegenerate(props.generation)
            }}
          >
            Load original settings
          </button>
          <button
            type='button'
            onClick={() => {
              if (props.generation) {
                props.onEditAsset(props.generation.assets[0], props.generation)
              }
            }}
          >
            Edit output
          </button>
        </>
      )}
    </section>
  ),
}))

function generation(
  id: number,
  status: ImageGenerationStatus = 'succeeded'
): ImageGeneration {
  return {
    id,
    model_profile_id: 2,
    specification_version: 1,
    model: 'gpt-image-2',
    prompt: `Creation ${id}`,
    parameters: {},
    request_id: `request-${id}`,
    status,
    requested_count: 1,
    succeeded_count: status === 'succeeded' ? 1 : 0,
    final_quota: 100,
    started_at: 1,
    finished_at: 2,
    created_at: 1,
    assets: [
      {
        id,
        position: 0,
        state: 'ready',
        thumbnail_state: 'ready',
        mime_type: 'image/png',
        size_bytes: 4,
        width: 1,
        height: 1,
      },
    ],
  }
}
let queryClient: QueryClient
function Workbench() {
  return (
    <QueryClientProvider client={queryClient}>
      <ImageCreatePage />
    </QueryClientProvider>
  )
}
beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  mocks.userId = 1
  mocks.submitted = generation(1)
  mocks.getGeneration
    .mockReset()
    .mockImplementation(async (id: number) =>
      generation(id, mocks.submitted.status)
    )
  mocks.getInput.mockReset().mockImplementation(async (id: number) => ({
    generation_id: id,
    mode: 'generation',
    references_available: false,
    references: [],
  }))
  mocks.output.mockReset()
})
afterEach(() => queryClient.clear())

describe('creation workbench continuity', () => {
  test.each<ImageGenerationStatus>([
    'succeeded',
    'partial',
    'failed',
    'submitting',
    'unknown',
    'archive_failed',
  ])('keeps a %s response on the creation page', async (status) => {
    mocks.submitted = generation(1, status)
    render(<Workbench />)
    fireEvent.click(screen.getByRole('button', { name: 'Generate test image' }))
    expect(await screen.findByText(`Creation 1: ${status}`)).toBeVisible()
    expect(
      screen.getAllByRole('region', { name: 'Creation form' })
    ).toHaveLength(1)
  })

  test('switching results and receiving a late task update preserves the next draft', async () => {
    render(<Workbench />)
    fireEvent.click(screen.getByRole('button', { name: 'Generate test image' }))
    await screen.findByText('Creation 1: succeeded')
    fireEvent.change(screen.getByLabelText('Next draft'), {
      target: { value: 'My next untouched idea' },
    })
    mocks.submitted = generation(2, 'submitting')
    fireEvent.click(screen.getByRole('button', { name: 'Generate test image' }))
    await screen.findByText('Creation 2: submitting')
    fireEvent.click(screen.getByRole('button', { name: 'View 1' }))
    act(() =>
      queryClient.setQueryData(
        imageGenerationQueryOptions(1, 2).queryKey,
        generation(2)
      )
    )
    expect(screen.getByText('Creation 1: succeeded')).toBeVisible()
    expect(screen.getByLabelText('Next draft')).toHaveValue(
      'My next untouched idea'
    )
    expect(screen.queryByText(/Loaded regenerate/)).not.toBeInTheDocument()
  })

  test('a failed settings load keeps the draft and allows another attempt', async () => {
    mocks.getInput.mockRejectedValueOnce(new Error('offline'))
    render(<Workbench />)
    fireEvent.click(screen.getByRole('button', { name: 'Generate test image' }))
    await screen.findByText('Creation 1: succeeded')
    fireEvent.change(screen.getByLabelText('Next draft'), {
      target: { value: 'keep draft' },
    })
    fireEvent.click(
      screen.getByRole('button', { name: 'Load original settings' })
    )
    expect(
      await screen.findByText(
        'Unable to load the original generation settings.'
      )
    ).toBeVisible()
    expect(screen.getByLabelText('Next draft')).toHaveValue('keep draft')
    fireEvent.click(
      screen.getByRole('button', { name: 'Load original settings' })
    )
    expect(await screen.findByText('Loaded regenerate 1')).toBeVisible()
  })

  test('switching accounts removes all private results and discards a late source read', async () => {
    let resolveFile: ((file: File) => void) | undefined
    mocks.output.mockImplementation(
      () =>
        new Promise<File>((resolve) => {
          resolveFile = resolve
        })
    )
    const view = render(<Workbench />)
    fireEvent.click(screen.getByRole('button', { name: 'Generate test image' }))
    await screen.findByText('Creation 1: succeeded')
    fireEvent.click(screen.getByRole('button', { name: 'Edit output' }))
    await waitFor(() => expect(mocks.output).toHaveBeenCalledTimes(1))
    mocks.userId = 2
    view.rerender(<Workbench />)
    await act(async () =>
      resolveFile?.(new File(['data'], 'image.png', { type: 'image/png' }))
    )
    expect(screen.getByText('No current result')).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'View 1' })
    ).not.toBeInTheDocument()
    expect(screen.queryByText(/Loaded edit-output/)).not.toBeInTheDocument()
  })
})
