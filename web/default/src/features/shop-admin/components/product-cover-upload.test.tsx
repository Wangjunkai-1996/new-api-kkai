/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import '@testing-library/jest-dom/vitest'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ShopError } from '@/features/shop/api'

import { ProductCoverUpload } from './product-cover-upload'

const request = vi.hoisted(() => vi.fn())
vi.mock('@/features/shop/api', async (original) => ({
  ...(await original<typeof import('@/features/shop/api')>()),
  shopRequest: request,
}))
vi.mock('@/features/shop/hooks/use-shop', () => ({
  useShopIdentity: () => 'admin-session',
}))

const oldURL = `/api/shop/covers/${'b'.repeat(64)}.jpg`
const newURL = `/api/shop/covers/${'a'.repeat(64)}.jpg`

beforeEach(() => {
  request.mockReset()
})

describe('product cover upload', () => {
  it('uploads one image and only changes the cover after the server returns its public URL', async () => {
    let resolveUpload: (value: {
      url: string
      width: number
      height: number
    }) => void = () => {}
    request.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveUpload = resolve
        })
    )
    const onChange = vi.fn()
    const onBusy = vi.fn()
    const view = render(
      <ProductCoverUpload value={oldURL} onChange={onChange} onBusy={onBusy} />
    )
    const file = new File(['png-bytes'], 'cover.png', { type: 'image/png' })
    fireEvent.change(screen.getByLabelText('Cover image'), {
      target: { files: [file] },
    })
    expect(request).toHaveBeenCalledOnce()
    const [path, , options] = request.mock.calls[0]
    expect(path).toBe('/api/admin/shop/covers')
    expect(options).toMatchObject({ method: 'POST' })
    expect((options.body as FormData).get('file')).toBe(file)
    expect(onBusy).toHaveBeenCalledWith(true)
    expect(onChange).not.toHaveBeenCalled()
    expect(screen.getByRole('status')).toHaveTextContent('Uploading')
    expect(screen.getByRole('button', { name: 'Remove cover' })).toBeDisabled()

    await act(async () =>
      resolveUpload({ url: newURL, width: 1600, height: 900 })
    )
    expect(onChange).toHaveBeenCalledWith(newURL)
    expect(onBusy).toHaveBeenLastCalledWith(false)
    view.rerender(
      <ProductCoverUpload value={newURL} onChange={onChange} onBusy={onBusy} />
    )
    expect(screen.getByAltText('Cover preview')).toHaveAttribute(
      'src',
      new URL(newURL, window.location.origin).href
    )
  })

  it('keeps the prior cover when upload fails and permits a retry', async () => {
    request.mockRejectedValueOnce(
      new ShopError('SHOP_COVER_STORAGE_UNAVAILABLE', 503)
    )
    request.mockResolvedValueOnce({ url: newURL, width: 800, height: 800 })
    const onChange = vi.fn()
    render(
      <ProductCoverUpload value={oldURL} onChange={onChange} onBusy={vi.fn()} />
    )
    const chooser = screen.getByLabelText('Cover image')
    fireEvent.change(chooser, {
      target: {
        files: [new File(['first'], 'first.png', { type: 'image/png' })],
      },
    })
    await screen.findByRole('alert')
    expect(onChange).not.toHaveBeenCalled()
    expect(screen.getByAltText('Cover preview')).toHaveAttribute(
      'src',
      new URL(oldURL, window.location.origin).href
    )
    fireEvent.change(chooser, {
      target: {
        files: [new File(['second'], 'second.png', { type: 'image/png' })],
      },
    })
    await waitFor(() => expect(onChange).toHaveBeenCalledWith(newURL))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('rejects a non-image locally and allows removing the previous cover', () => {
    const onChange = vi.fn()
    render(
      <ProductCoverUpload value={oldURL} onChange={onChange} onBusy={vi.fn()} />
    )
    fireEvent.change(screen.getByLabelText('Cover image'), {
      target: {
        files: [new File(['<svg/>'], 'cover.svg', { type: 'image/svg+xml' })],
      },
    })
    expect(request).not.toHaveBeenCalled()
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Choose a JPEG, PNG or WebP image up to 5 MiB.'
    )
    fireEvent.click(screen.getByRole('button', { name: 'Remove cover' }))
    expect(onChange).toHaveBeenCalledWith('')
  })
})
