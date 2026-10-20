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
import { expect, test, vi } from 'vitest'

import type { ImageGeneration } from '../types'
import { ImageMasonry } from './image-masonry'

const generation = (
  id: number,
  width: number,
  height: number
): ImageGeneration => ({
  id,
  model_profile_id: 1,
  specification_version: 1,
  model: 'gpt-image-2',
  prompt: `Image ${String(id)}`,
  parameters: {},
  request_id: String(id),
  status: 'succeeded',
  requested_count: 1,
  succeeded_count: 1,
  final_quota: 0,
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
      width,
      height,
      content_url: `/image-${String(id)}.png`,
    },
  ],
})

test('appending a page keeps existing cards mounted in place and preserves the single list order', async () => {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue(
    new DOMRect(0, 0, 600, 1000)
  )
  const items = [
    generation(4, 768, 1024),
    generation(3, 1536, 1024),
    generation(2, 1024, 1024),
  ]
  const renderItem = (item: ImageGeneration) => (
    <button type='button'>{item.prompt}</button>
  )
  const { rerender } = render(
    <ImageMasonry items={items} renderItem={renderItem} />
  )
  await waitFor(() =>
    expect(screen.getAllByRole('listitem')[1].style.transform).not.toBe('')
  )
  const originalCards = screen.getAllByRole('listitem')
  const positions = originalCards.map((card) => card.style.transform)

  rerender(
    <ImageMasonry
      items={[...items, generation(1, 768, 1024)]}
      renderItem={renderItem}
    />
  )
  await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(4))
  for (const [index, card] of originalCards.entries()) {
    expect(screen.getAllByRole('listitem')[index]).toBe(card)
    expect(card.style.transform).toBe(positions[index])
  }
  expect(
    screen.getAllByRole('button').map((button) => button.textContent)
  ).toEqual(['Image 4', 'Image 3', 'Image 2', 'Image 1'])
})
