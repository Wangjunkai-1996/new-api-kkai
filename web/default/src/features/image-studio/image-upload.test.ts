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
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { uploadImageReferences } from './image-upload'
import {
  createImageUpload,
  getImageUpload,
  listImageUploadParts,
  completeImageUpload,
  signImageUploadPart,
} from './image-upload-api'

vi.mock('./image-upload-api', () => ({
  createImageUpload: vi.fn(),
  getImageUpload: vi.fn(),
  listImageUploadParts: vi.fn(),
  signImageUploadPart: vi.fn(),
  completeImageUpload: vi.fn(),
}))

const reservation = (
  id: number,
  state: 'pending_upload' | 'ready' | 'validating' | 'failed' = 'ready'
) => ({
  asset: {
    id,
    state,
    mime_type: 'image/png',
    size_bytes: 9,
    sha256: String(id).repeat(64),
  },
  upload_mode: 'multipart' as const,
  part_size: 5 * 1024 * 1024,
  expires_at: 2_000_000_000,
  max_size_bytes: 32 * 1024 * 1024,
})

const file = () =>
  new File(['reference'], 'reference.png', { type: 'image/png' })

describe('reference upload recovery', () => {
  beforeEach(() => localStorage.clear())

  test('reuses ready uploads and polls validation without sending the original files again', async () => {
    const pending = reservation(1, 'validating')
    const ready = reservation(2)
    vi.mocked(createImageUpload)
      .mockResolvedValueOnce(pending)
      .mockResolvedValueOnce(ready)
    vi.mocked(getImageUpload).mockResolvedValue(reservation(1))
    const progress = vi.fn()
    const result = await uploadImageReferences(
      11,
      [file(), file()],
      [
        { sha256: '1'.repeat(64), size_bytes: 9 },
        { sha256: '2'.repeat(64), size_bytes: 9 },
      ],
      progress,
      new AbortController().signal
    )
    expect(result.map((asset) => asset.id)).toEqual([1, 2])
    expect(getImageUpload).toHaveBeenCalledExactlyOnceWith(
      1,
      expect.any(AbortSignal)
    )
    expect(progress).toHaveBeenCalledWith({
      phase: 'validating',
      progress: 100,
    })
    expect(createImageUpload).toHaveBeenNthCalledWith(
      1,
      { sha256: '1'.repeat(64), size_bytes: 9, mime_type: 'image/png' },
      expect.any(String),
      expect.any(AbortSignal)
    )
  })

  test('resumes an uploaded multipart body and completes its manifest without uploading the image twice', async () => {
    vi.mocked(createImageUpload).mockResolvedValue(
      reservation(1, 'pending_upload')
    )
    vi.mocked(listImageUploadParts).mockResolvedValue([
      { part_number: 1, size_bytes: 9, etag: 'stored-etag' },
    ])
    vi.mocked(completeImageUpload).mockResolvedValue(
      reservation(1, 'validating').asset
    )
    vi.mocked(getImageUpload).mockResolvedValue(reservation(1))
    const result = await uploadImageReferences(
      11,
      [file()],
      [{ sha256: '1'.repeat(64), size_bytes: 9 }],
      vi.fn(),
      new AbortController().signal
    )
    expect(result[0].state).toBe('ready')
    expect(listImageUploadParts).toHaveBeenCalledWith(
      1,
      expect.any(AbortSignal)
    )
    expect(completeImageUpload).toHaveBeenCalledWith(
      1,
      [{ part_number: 1, etag: 'stored-etag' }],
      expect.any(AbortSignal)
    )
    expect(signImageUploadPart).not.toHaveBeenCalled()
  })

  test('keeps the same upload receipt across retries and isolates accounts', async () => {
    vi.mocked(createImageUpload).mockResolvedValue(reservation(1))
    const metadata = [{ sha256: '1'.repeat(64), size_bytes: 9 }]
    await uploadImageReferences(
      11,
      [file()],
      metadata,
      vi.fn(),
      new AbortController().signal
    )
    await uploadImageReferences(
      11,
      [file()],
      metadata,
      vi.fn(),
      new AbortController().signal
    )
    await uploadImageReferences(
      12,
      [file()],
      metadata,
      vi.fn(),
      new AbortController().signal
    )
    const calls = vi.mocked(createImageUpload).mock.calls
    expect(calls[0][1]).toBe(calls[1][1])
    expect(calls[2][1]).not.toBe(calls[0][1])
  })

  test('a terminal upload requires an explicit retry before allocating a new receipt', async () => {
    vi.mocked(createImageUpload)
      .mockResolvedValueOnce(reservation(1, 'failed'))
      .mockResolvedValueOnce(reservation(1))
    const metadata = [{ sha256: '1'.repeat(64), size_bytes: 9 }]
    await expect(
      uploadImageReferences(
        11,
        [file()],
        metadata,
        vi.fn(),
        new AbortController().signal
      )
    ).rejects.toThrow('failed')
    expect(createImageUpload).toHaveBeenCalledTimes(1)
    await uploadImageReferences(
      11,
      [file()],
      metadata,
      vi.fn(),
      new AbortController().signal
    )
    expect(vi.mocked(createImageUpload).mock.calls[0][1]).not.toBe(
      vi.mocked(createImageUpload).mock.calls[1][1]
    )
  })

  test('rejects mismatched server metadata and never quotes an unverified reference', async () => {
    vi.mocked(createImageUpload).mockResolvedValue(reservation(2))
    await expect(
      uploadImageReferences(
        11,
        [file()],
        [{ sha256: '1'.repeat(64), size_bytes: 9 }],
        vi.fn(),
        new AbortController().signal
      )
    ).rejects.toThrow('failed')
    expect(getImageUpload).not.toHaveBeenCalled()
  })
})
