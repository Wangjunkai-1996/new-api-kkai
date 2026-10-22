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
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { useSystemConfigStore } from '@/stores/system-config-store'

import type { ImageAsset, ImageGeneration } from '../types'
import {
  ImageCreationResults,
  type ImageCreationResultsProps,
} from './image-creation-results'

const media = vi.hoisted(() => ({
  urls: {} as Record<string, string>,
  pending: false,
  reduced: false,
  refetch: vi.fn(),
}))
vi.mock('@/lib/studio-media', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/studio-media')>()),
  useStudioMediaUrls: () => ({
    data: { urls: media.urls },
    isPending: media.pending,
    refetch: media.refetch,
  }),
}))
vi.mock('motion/react', () => ({ useReducedMotion: () => media.reduced }))

const asset = (id: number): ImageAsset => ({
  id,
  position: id,
  state: 'ready',
  thumbnail_state: 'ready',
  mime_type: 'image/png',
  size_bytes: 128,
  width: 768,
  height: 1024,
  content_url: `/content-${id}.png`,
  thumbnail_url: `/thumbnail-${id}.png`,
  download_url: `/download-${id}.png`,
})
const generation = (
  overrides: Partial<ImageGeneration> = {}
): ImageGeneration => ({
  id: 1,
  model_profile_id: 7,
  specification_version: 2,
  model: 'gpt-image-2',
  prompt: 'A ceramic camellia',
  parameters: { size: '768x1024', quality: 'high' },
  request_id: 'request-1',
  status: 'succeeded',
  requested_count: 2,
  succeeded_count: 2,
  final_quota: 20_000,
  started_at: 1,
  finished_at: 2,
  created_at: 1,
  assets: [asset(2), asset(1)],
  ...overrides,
})
const propsFor = (
  item = generation()
): ImageCreationResultsProps & { generation: ImageGeneration } => ({
  generation: item,
  history: [item],
  onSelectGeneration: vi.fn(),
  onSelectAsset: vi.fn(),
  onRegenerate: vi.fn(),
  onEditAsset: vi.fn(),
  onCheckStatus: vi.fn(),
})
beforeEach(() => {
  media.urls = {}
  media.pending = false
  media.reduced = false
})

describe('creation workbench results', () => {
  test('preserves asset order and binds selection, editing, retry and download to the real task', async () => {
    const props = propsFor()
    render(<ImageCreationResults {...props} />)
    const image = screen.getByRole('img', { name: props.generation.prompt })
    expect(image).toHaveAttribute('src', '/content-1.png')
    expect(image).toHaveAttribute('width', '768')
    expect(image).toHaveAttribute('height', '1024')
    await userEvent.click(
      screen.getByRole('button', { name: 'Select image 2' })
    )
    expect(props.onSelectAsset).toHaveBeenCalledExactlyOnceWith(2)
    await userEvent.click(
      screen.getByRole('button', { name: 'Use this image to edit' })
    )
    expect(props.onEditAsset).toHaveBeenCalledExactlyOnceWith(
      props.generation.assets[1],
      props.generation
    )
    await userEvent.click(
      screen.getByRole('button', { name: 'Generate again' })
    )
    expect(props.onRegenerate).toHaveBeenCalledExactlyOnceWith(props.generation)
    expect(
      screen.getByRole('link', { name: 'imageStudio.download' })
    ).toHaveAttribute('href', '/download-1.png')
  })

  test('waits for decoding and does not replay reveal after URL refresh or revisiting an asset', async () => {
    const props = propsFor()
    const view = render(<ImageCreationResults {...props} />)
    const image = screen.getByRole('img', { name: props.generation.prompt })
    let finish!: () => void
    Object.defineProperty(image, 'decode', {
      value: () =>
        new Promise<void>((resolve) => {
          finish = resolve
        }),
    })
    fireEvent.load(image)
    expect(image).not.toBeVisible()
    expect(
      screen.getByRole('button', { name: 'View image details' })
    ).toBeDisabled()
    await act(async () => finish())
    expect(image).toBeVisible()
    expect(image).toHaveClass('is-revealing')
    fireEvent.animationEnd(image)
    const updated = {
      ...props.generation,
      assets: props.generation.assets.map((item) => ({
        ...item,
        content_url: `${item.content_url}?refresh=1`,
      })),
    }
    view.rerender(<ImageCreationResults {...props} generation={updated} />)
    fireEvent.load(image)
    await act(async () => finish())
    expect(image).not.toHaveClass('is-revealing')
    view.rerender(<ImageCreationResults {...props} selectedAssetId={2} />)
    view.rerender(<ImageCreationResults {...props} selectedAssetId={1} />)
    const revisited = screen.getByRole('img', {
      name: props.generation.prompt,
    })
    fireEvent.load(revisited)
    await waitFor(() => expect(revisited).toBeVisible())
    expect(revisited).not.toHaveClass('is-revealing')
  })

  test('ignores a stale decode when switching images', async () => {
    const props = propsFor()
    const view = render(<ImageCreationResults {...props} />)
    const first = screen.getByRole('img', { name: props.generation.prompt })
    let finishOld!: () => void
    Object.defineProperty(first, 'decode', {
      value: () =>
        new Promise<void>((resolve) => {
          finishOld = resolve
        }),
    })
    fireEvent.load(first)
    view.rerender(<ImageCreationResults {...props} selectedAssetId={2} />)
    const second = screen.getByRole('img', { name: props.generation.prompt })
    await act(async () => finishOld())
    expect(second).not.toBeVisible()
    fireEvent.load(second)
    await waitFor(() => expect(second).toBeVisible())
    view.rerender(<ImageCreationResults {...props} selectedAssetId={1} />)
    const revisited = screen.getByRole('img', {
      name: props.generation.prompt,
    })
    fireEvent.load(revisited)
    await waitFor(() => expect(revisited).toBeVisible())
    expect(revisited).toHaveClass('is-revealing')
  })

  test.each(['submitting', 'unknown', 'archive_failed'] as const)(
    'does not offer a billable retry for %s',
    async (status) => {
      const item = generation({
        status,
        assets: [],
        succeeded_count: 0,
        error_message: 'Private upstream balance https://provider.example',
      })
      const props = propsFor(item)
      render(<ImageCreationResults {...props} />)
      expect(
        screen.queryByRole('button', { name: 'Generate again' })
      ).not.toBeInTheDocument()
      expect(
        screen.queryByRole('button', { name: 'Retry generation' })
      ).not.toBeInTheDocument()
      expect(
        screen.queryByText(/Private upstream|provider\.example/)
      ).not.toBeInTheDocument()
      if (status !== 'archive_failed') {
        await userEvent.click(
          screen.getByRole('button', { name: 'Check status' })
        )
        expect(props.onCheckStatus).toHaveBeenCalledExactlyOnceWith(item)
      }
    }
  )

  test('retains the previous decoded image while the selected image loads', async () => {
    const props = propsFor()
    const view = render(<ImageCreationResults {...props} />)
    fireEvent.load(screen.getByRole('img', { name: props.generation.prompt }))
    await waitFor(() =>
      expect(
        screen.getByRole('img', { name: props.generation.prompt })
      ).toBeVisible()
    )
    view.rerender(<ImageCreationResults {...props} selectedAssetId={2} />)
    expect(screen.getByText('Previous result')).toBeVisible()
    expect(
      view.container.querySelector('img[aria-hidden="true"]')
    ).toHaveAttribute('src', '/content-1.png')
    expect(
      screen.getByRole('button', { name: 'Use this image to edit' })
    ).toBeDisabled()
    const selected = screen.getByRole('img', { name: props.generation.prompt })
    expect(selected).not.toBeVisible()
    fireEvent.load(selected)
    await waitFor(() => expect(selected).toBeVisible())
    expect(screen.queryByText('Previous result')).not.toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Use this image to edit' })
    ).toBeEnabled()
  })

  test('distinguishes a retained previous image from the failed current task', async () => {
    const previous = generation()
    const current = generation({
      id: 2,
      status: 'failed',
      assets: [],
      succeeded_count: 0,
      prompt: 'Failed idea',
      error_code: 'image_service_unavailable',
    })
    const props = propsFor(current)
    render(
      <ImageCreationResults
        {...props}
        previousGeneration={previous}
        history={[current, previous]}
      />
    )
    expect(screen.getByText('Previous result')).toBeVisible()
    expect(
      screen.getByText('No available result from this creation')
    ).toBeVisible()
    expect(screen.getByRole('img', { name: previous.prompt })).toHaveAttribute(
      'src',
      '/content-1.png'
    )
    expect(
      screen.queryByRole('button', { name: 'Use this image to edit' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('link', { name: 'imageStudio.download' })
    ).not.toBeInTheDocument()
    await userEvent.click(
      screen.getByRole('button', { name: 'Retry generation' })
    )
    expect(props.onRegenerate).toHaveBeenCalledExactlyOnceWith(current)
    await userEvent.click(
      screen.getByRole('button', {
        name: 'View creation 1: imageStudio.status.succeeded',
      })
    )
    expect(props.onSelectGeneration).toHaveBeenCalledExactlyOnceWith(
      previous.id
    )
  })

  test('uses signed URLs without falling back to protected raw media', () => {
    const protectedAsset = {
      ...asset(1),
      content_url: '/api/image-studio/assets/1/content',
      thumbnail_url: '/api/image-studio/assets/1/thumbnail',
      download_url: '/api/image-studio/assets/1/download',
    }
    const props = propsFor(generation({ assets: [protectedAsset] }))
    const view = render(<ImageCreationResults {...props} />)
    expect(
      screen.queryByRole('img', { name: props.generation.prompt })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('link', { name: 'imageStudio.download' })
    ).not.toBeInTheDocument()
    media.urls = {
      [protectedAsset.content_url]: '/signed-content',
      [protectedAsset.download_url]: '/signed-download',
    }
    view.rerender(<ImageCreationResults {...props} />)
    expect(
      screen.getByRole('img', { name: props.generation.prompt })
    ).toHaveAttribute('src', '/signed-content')
    expect(
      screen.getByRole('link', { name: 'imageStudio.download' })
    ).toHaveAttribute('href', '/signed-download')
    media.urls = {}
    view.rerender(<ImageCreationResults {...props} />)
    expect(
      screen.queryByRole('img', { name: props.generation.prompt })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('link', { name: 'imageStudio.download' })
    ).not.toBeInTheDocument()
  })

  test('shows actual site quota and only real upload progress', async () => {
    const currency = useSystemConfigStore.getState().config.currency
    useSystemConfigStore.getState().setConfig({
      currency: {
        ...currency,
        quotaDisplayType: 'USD',
        quotaPerUnit: 500_000,
        usdExchangeRate: 1,
      },
    })
    try {
      const props = propsFor()
      const view = render(
        <ImageCreationResults
          {...props}
          activity={{ phase: 'uploading', progress: 42 }}
        />
      )
      expect(screen.getByText('Uploading reference images · 42%')).toBeVisible()
      expect(
        screen.queryByRole('button', { name: 'Generate again' })
      ).not.toBeInTheDocument()
      await userEvent.click(screen.getByText('Creation details'))
      expect(screen.getByText('$0.04')).toBeVisible()
      view.rerender(
        <ImageCreationResults {...props} activity={{ phase: 'submitting' }} />
      )
      expect(
        screen.getByText('Your image request is being processed')
      ).toBeVisible()
      expect(screen.queryByText(/42%/)).not.toBeInTheDocument()
    } finally {
      useSystemConfigStore.getState().setConfig({ currency })
    }
  })

  test('respects reduced motion and opens the decoded image in a large view', async () => {
    media.reduced = true
    const props = propsFor()
    render(<ImageCreationResults {...props} />)
    const image = screen.getByRole('img', { name: props.generation.prompt })
    fireEvent.load(image)
    await waitFor(() => expect(image).toBeVisible())
    expect(image).not.toHaveClass('is-revealing')
    await userEvent.click(
      screen.getAllByRole('button', { name: 'View image details' })[0]
    )
    expect(screen.getByRole('dialog', { name: 'Image details' })).toBeVisible()
  })
})
