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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test } from 'vitest'

import type { ImageAsset, ImageGeneration } from '../types'
import { ImageGenerationCard } from './image-generation-card'

const asset = (position: number): ImageAsset => ({
  id: position + 10,
  position,
  state: 'ready',
  thumbnail_state: 'ready',
  mime_type: 'image/png',
  size_bytes: 128,
  width: 1024,
  height: 1024,
  content_url: `/content-${String(position)}.png`,
  thumbnail_url: `/thumbnail-${String(position)}.png`,
  download_url: `/download-${String(position)}.png`,
})

const generation = (assets: ImageAsset[]): ImageGeneration => ({
  id: 1,
  model_profile_id: 7,
  specification_version: 2,
  model: 'gpt-image-2',
  prompt: 'A batch of images',
  parameters: { variants: assets.length },
  request_id: 'request-1',
  status: 'succeeded',
  requested_count: assets.length,
  succeeded_count: assets.length,
  final_quota: 100,
  started_at: 1,
  finished_at: 2,
  created_at: 1,
  assets,
})

describe('image generation card batch layout', () => {
  test('sorts previews and downloads by asset position', () => {
    render(
      <ImageGenerationCard
        generation={generation([asset(2), asset(0), asset(1)])}
        onDelete={() => undefined}
      />
    )

    expect(
      screen.getAllByRole('img').map((image) => image.getAttribute('src'))
    ).toEqual(['/thumbnail-0.png', '/thumbnail-1.png', '/thumbnail-2.png'])
    expect(
      screen
        .getAllByRole('link', { name: /^Preview \d$/ })
        .map((link) => link.getAttribute('href'))
    ).toEqual(['/content-0.png', '/content-1.png', '/content-2.png'])
    expect(
      screen
        .getAllByRole('button', { name: /^imageStudio\.download \d$/ })
        .map((link) => link.getAttribute('href'))
    ).toEqual(['/download-0.png', '/download-1.png', '/download-2.png'])
  })

  test('names and describes each batch action by its sorted position', async () => {
    const user = userEvent.setup()
    render(
      <ImageGenerationCard
        generation={generation([asset(1), asset(0)])}
        onDelete={() => undefined}
      />
    )

    const firstPreview = screen.getByRole('link', { name: 'Preview 1' })
    await user.hover(firstPreview)
    await waitFor(() => {
      expect(firstPreview).toHaveAttribute('data-popup-open')
    })

    await user.unhover(firstPreview)
    const secondDownload = screen.getByRole('button', {
      name: 'imageStudio.download 2',
    })
    await user.hover(secondDownload)
    await waitFor(() => {
      expect(secondDownload).toHaveAttribute('data-popup-open')
    })
  })

  test.each([
    { count: 1, gridClass: 'grid-cols-1', rowsClass: 'grid-rows-1' },
    { count: 2, gridClass: 'grid-cols-2', rowsClass: 'grid-rows-1' },
    { count: 3, gridClass: 'grid-cols-2', rowsClass: 'grid-rows-2' },
    { count: 4, gridClass: 'grid-cols-2', rowsClass: 'grid-rows-2' },
  ])('keeps a stable $count-image grid', ({ count, gridClass, rowsClass }) => {
    const assets = Array.from({ length: count }, (_, position) =>
      asset(position)
    )
    const { container } = render(
      <ImageGenerationCard
        generation={generation(assets)}
        onDelete={() => undefined}
      />
    )

    const grid = container.querySelector('.aspect-square')
    expect(grid).toHaveClass(gridClass, rowsClass)
    const firstPreview = screen.getAllByRole('img')[0].closest('a')
    if (count === 3) expect(firstPreview).toHaveClass('row-span-2')
    else expect(firstPreview).not.toHaveClass('row-span-2')
  })
})
