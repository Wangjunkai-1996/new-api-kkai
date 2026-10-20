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

import { useSystemConfigStore } from '@/stores/system-config-store'

import type {
  ImageAsset,
  ImageGeneration,
  ImageGenerationStatus,
} from '../types'
import { ImageGenerationCard } from './image-generation-card'
import { ImageGenerationDetails } from './image-generation-details'

const asset = (position: number): ImageAsset => ({
  id: position + 10,
  position,
  state: 'ready',
  thumbnail_state: 'ready',
  mime_type: 'image/png',
  size_bytes: 128,
  width: 768,
  height: 1024,
  content_url: `/content-${String(position)}.png`,
  thumbnail_url: `/thumbnail-${String(position)}.png`,
  download_url: `/download-${String(position)}.png`,
})

const generation = (
  status: ImageGenerationStatus = 'succeeded'
): ImageGeneration => ({
  id: 1,
  model_profile_id: 7,
  specification_version: 2,
  model: 'gpt-image-2',
  prompt: 'A batch of images',
  parameters: { variants: 3 },
  request_id: 'request-1',
  status,
  requested_count: 3,
  succeeded_count: 3,
  final_quota: 20_000,
  started_at: 1,
  finished_at: 2,
  created_at: 1,
  assets: [asset(2), asset(0), asset(1)],
})

describe('image generation gallery actions', () => {
  test('shows the first original-proportion cover and opens the entire batch in place', async () => {
    const onDetails = vi.fn()
    const item = generation()
    render(
      <ImageGenerationCard
        generation={item}
        onDetails={onDetails}
        onRegenerate={vi.fn()}
      />
    )

    const cover = screen.getByRole('img')
    expect(cover).toHaveAttribute('src', '/thumbnail-0.png')
    expect(cover).toHaveAttribute('width', '768')
    expect(cover).toHaveAttribute('height', '1024')
    expect(cover).not.toBeVisible()
    fireEvent.load(cover)
    await waitFor(() => expect(cover).toBeVisible())
    expect(screen.getByLabelText('Images in this batch: 3')).toBeVisible()
    await userEvent.click(
      screen.getByRole('button', { name: 'View image details' })
    )
    expect(onDetails).toHaveBeenCalledWith(item)
  })

  test.each([
    { status: 'succeeded', label: 'Generate again' },
    { status: 'partial', label: 'Generate again' },
    { status: 'failed', label: 'Retry generation' },
  ] as const)(
    'offers the correct explicit new-generation action for $status',
    async ({ status, label }) => {
      const item = generation(status)
      const onRegenerate = vi.fn()
      render(
        <ImageGenerationCard
          generation={item}
          onDetails={vi.fn()}
          onRegenerate={onRegenerate}
        />
      )
      await userEvent.click(screen.getByRole('button', { name: label }))
      expect(onRegenerate).toHaveBeenCalledExactlyOnceWith(item)
    }
  )

  test.each(['submitting', 'unknown', 'archive_failed'] as const)(
    'never offers a billable retry for $status',
    async (status) => {
      const item = { ...generation(status), assets: [] }
      const onDetails = vi.fn()
      render(
        <ImageGenerationCard
          generation={item}
          onDetails={onDetails}
          onRegenerate={vi.fn()}
        />
      )
      expect(
        screen.queryByRole('button', { name: 'Generate again' })
      ).not.toBeInTheDocument()
      expect(
        screen.queryByRole('button', { name: 'Retry generation' })
      ).not.toBeInTheDocument()
      await userEvent.click(
        screen.getByRole('button', { name: 'View details' })
      )
      expect(onDetails).toHaveBeenCalledWith(item)
    }
  )
})

describe('image generation batch details', () => {
  test.each(['zhCN', 'zhTW'])(
    'renders dates and quota without crashing in %s',
    async (language) => {
      const originalCurrency = useSystemConfigStore.getState().config.currency
      useSystemConfigStore.getState().setConfig({
        currency: {
          ...originalCurrency,
          quotaDisplayType: 'USD',
          quotaPerUnit: 500_000,
          usdExchangeRate: 1,
        },
      })
      await i18next.changeLanguage(language)
      try {
        render(
          <ImageGenerationDetails
            generation={generation()}
            refreshing={false}
            onClose={vi.fn()}
            onRegenerate={vi.fn()}
            onDelete={vi.fn()}
            onRefresh={vi.fn()}
          />
        )
        expect(
          screen.getByRole('dialog', { name: 'Image details' })
        ).toBeVisible()
        expect(screen.getByText('$0.04')).toBeVisible()
      } finally {
        await i18next.changeLanguage('en')
        useSystemConfigStore.getState().setConfig({
          currency: originalCurrency,
        })
      }
    }
  )

  test('formats quota units as the configured billing currency', async () => {
    const originalCurrency = useSystemConfigStore.getState().config.currency
    try {
      useSystemConfigStore.getState().setConfig({
        currency: {
          ...originalCurrency,
          quotaDisplayType: 'CNY',
          quotaPerUnit: 500_000,
          usdExchangeRate: 7,
        },
      })
      render(
        <ImageGenerationDetails
          generation={generation()}
          refreshing={false}
          onClose={vi.fn()}
          onRegenerate={vi.fn()}
          onDelete={vi.fn()}
          onRefresh={vi.fn()}
        />
      )
      expect(screen.getByText(/0\.28/)).toBeVisible()

      useSystemConfigStore.getState().setConfig({
        currency: {
          ...originalCurrency,
          quotaDisplayType: 'TOKENS',
          quotaPerUnit: 500_000,
        },
      })
      await waitFor(() => expect(screen.getByText('$0.04')).toBeVisible())
    } finally {
      useSystemConfigStore.getState().setConfig({
        currency: originalCurrency,
      })
    }
  })

  test('preserves every preview and download in asset order and clearly deletes a whole batch', async () => {
    const item = generation()
    const onDelete = vi.fn()
    render(
      <ImageGenerationDetails
        generation={item}
        refreshing={false}
        onClose={vi.fn()}
        onRegenerate={vi.fn()}
        onDelete={onDelete}
        onRefresh={vi.fn()}
      />
    )

    expect(
      screen
        .getAllByRole('link', { name: /^Preview / })
        .map((link) => link.getAttribute('href'))
    ).toEqual(['/content-0.png', '/content-1.png', '/content-2.png'])
    expect(
      screen
        .getAllByRole('button', { name: /^imageStudio.download / })
        .map((link) => link.getAttribute('href'))
    ).toEqual(['/download-0.png', '/download-1.png', '/download-2.png'])
    expect(screen.getByText('Quota used')).toBeVisible()
    expect(screen.getByText('$0.04')).toBeVisible()
    await userEvent.click(
      screen.getByRole('button', { name: 'Delete this batch' })
    )
    expect(onDelete).toHaveBeenCalledExactlyOnceWith(item)
  })

  test('checks uncertain submissions without generating again', async () => {
    const onRefresh = vi.fn()
    render(
      <ImageGenerationDetails
        generation={{ ...generation('unknown'), assets: [] }}
        refreshing={false}
        onClose={vi.fn()}
        onRegenerate={vi.fn()}
        onDelete={vi.fn()}
        onRefresh={onRefresh}
      />
    )
    await userEvent.click(screen.getByRole('button', { name: 'Check status' }))
    expect(onRefresh).toHaveBeenCalledOnce()
    expect(
      screen.queryByRole('button', { name: 'Generate again' })
    ).not.toBeInTheDocument()
  })
})
