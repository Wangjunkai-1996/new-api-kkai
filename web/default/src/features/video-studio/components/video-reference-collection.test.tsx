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
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, expect, test, vi } from 'vitest'

import type {
  VideoAsset,
  VideoComposerReference,
  VideoModelProfile,
} from '../types'
import { VideoReferenceCollection } from './video-reference-collection'

const media = (
  id: number,
  state: VideoAsset['state'] = 'ready'
): VideoAsset => ({
  id,
  scope: 'user',
  kind: 'reference',
  state,
  original_filename: `image-${id}.png`,
  mime_type: 'image/png',
  size_bytes: 1000,
  width: 500,
  height: 500,
  duration_seconds: 0,
  codec: '',
  created_at: 1,
  updated_at: 1,
})

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, unknown>) =>
      key.replaceAll(/{{(\w+)}}/g, (_match: string, name: string) =>
        String(values?.[name] ?? name)
      ),
    i18n: { language: 'en' },
  }),
}))
vi.mock('@/lib/studio-media', () => ({
  useStudioMediaUrls: () => ({
    data: { urls: {}, deniedPaths: [] },
    identityDenied: false,
    isFetching: false,
    refetch: async () => undefined,
  }),
  signedStudioMediaUrl: (url?: string) => url,
}))
const calls = vi.hoisted(() => ({ deleteAsset: vi.fn() }))
vi.mock('../api', () => ({
  getVideoAssetContentUrl: (id: number) => `/assets/${id}`,
  deleteVideoAsset: calls.deleteAsset,
}))
vi.mock('./video-asset-uploader', () => ({
  VideoAssetUploader: (props: {
    purpose: string
    maxSizeBytes: number
    onAssetsChange: (assets: VideoAsset[]) => void
  }) => (
    <div>
      <p>
        {props.purpose}:{props.maxSizeBytes}
      </p>
      <button
        type='button'
        onClick={() => props.onAssetsChange([media(10, 'processing')])}
      >
        inspect replacement
      </button>
      <button
        type='button'
        onClick={() => props.onAssetsChange([media(10, 'failed')])}
      >
        fail replacement
      </button>
      <button type='button' onClick={() => props.onAssetsChange([media(10)])}>
        ready replacement
      </button>
    </div>
  ),
}))

const profile = {
  id: 1,
  model: 'verified-video',
  display_name: 'Verified video',
  description: '',
  provider_label: '',
  specification_version: 2,
  specification: {
    version: 2,
    modes: ['text_to_video', 'reference_to_video'],
    parameters: [],
    reference_policy: {
      max_total_count: null,
      requires_visual: true,
      requires_video: false,
      audio_requires_generate_audio: true,
      image: {
        allowed_mime_types: ['image/png'],
        max_count: 3,
        max_size_bytes: 20_971_520,
        min_width: 301,
        max_width: 5999,
        min_height: 301,
        max_height: 5999,
      },
      audio: {
        allowed_mime_types: ['audio/mpeg'],
        max_count: 2,
        max_size_bytes: 14_999_999,
        min_duration_seconds: 2,
        max_duration_seconds: 15,
        max_total_duration_seconds: 15,
      },
    },
  },
  default_parameters: {},
  enabled: true,
  sort_order: 0,
  created_at: 1,
  updated_at: 1,
} satisfies VideoModelProfile
const reference = (id: number, position = 0): VideoComposerReference => ({
  client_id: `ref-${id}`,
  asset_id: id,
  role: 'reference_image',
  position,
  included: true,
  asset: media(id),
})
function Collection(props: {
  initial: VideoComposerReference[]
  onChange?: (references: VideoComposerReference[]) => void
  profile?: VideoModelProfile
}) {
  const [references, setReferences] = useState(props.initial)
  return (
    <VideoReferenceCollection
      references={references}
      profile={props.profile ?? profile}
      onChange={(next) => {
        setReferences(next)
        props.onChange?.(next)
      }}
    />
  )
}
afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

test('reordering preserves role positions instead of sorting by asset ID', () => {
  const onChange = vi.fn()
  render(
    <Collection initial={[reference(9), reference(3, 1)]} onChange={onChange} />
  )
  fireEvent.click(
    screen.getByRole('button', { name: 'Move image-3.png earlier' })
  )
  expect(onChange).toHaveBeenLastCalledWith([
    expect.objectContaining({ asset_id: 3, position: 0 }),
    expect.objectContaining({ asset_id: 9, position: 1 }),
  ])
})

test('replacement keeps the original through inspection and failure, and applies only ready media', () => {
  const onChange = vi.fn()
  render(<Collection initial={[reference(9)]} onChange={onChange} />)
  fireEvent.click(
    screen.getByRole('button', { name: 'Options for image-9.png' })
  )
  fireEvent.click(screen.getByRole('menuitem', { name: 'Replace reference' }))
  fireEvent.click(screen.getByRole('button', { name: 'inspect replacement' }))
  expect(
    screen.getByRole('button', { name: 'Replace reference' })
  ).toBeDisabled()
  expect(onChange).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: 'fail replacement' }))
  expect(
    screen.getByRole('button', { name: 'Replace reference' })
  ).toBeDisabled()
  expect(onChange).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: 'ready replacement' }))
  fireEvent.click(screen.getByRole('button', { name: 'Replace reference' }))
  expect(onChange).toHaveBeenLastCalledWith([
    expect.objectContaining({ client_id: 'ref-9', asset_id: 10, position: 0 }),
  ])
})

test('excluding and restoring preserves assets, and removal only unbinds the draft', () => {
  const onChange = vi.fn()
  render(<Collection initial={[reference(9)]} onChange={onChange} />)
  fireEvent.click(
    screen.getByRole('button', { name: 'Options for image-9.png' })
  )
  fireEvent.click(
    screen.getByRole('menuitem', { name: 'Exclude from this generation' })
  )
  expect(onChange).toHaveBeenLastCalledWith([
    expect.objectContaining({ asset_id: 9, included: false }),
  ])
  fireEvent.click(screen.getByText('Not used in this generation (1)'))
  fireEvent.click(screen.getByRole('button', { name: 'Restore reference' }))
  expect(onChange).toHaveBeenLastCalledWith([
    expect.objectContaining({ asset_id: 9, included: true }),
  ])
  fireEvent.click(
    screen.getByRole('button', { name: 'Options for image-9.png' })
  )
  fireEvent.click(screen.getByRole('menuitem', { name: 'Remove from draft' }))
  expect(onChange).toHaveBeenLastCalledWith([])
  expect(calls.deleteAsset).not.toHaveBeenCalled()
})

test('audio selection uses the audio purpose and effective inclusive byte limit', () => {
  render(<Collection initial={[reference(9)]} />)
  fireEvent.click(screen.getByRole('button', { name: 'Add references' }))
  fireEvent.click(screen.getByRole('button', { name: 'Reference audio' }))
  expect(screen.getByText('reference_audio:14999999')).toBeInTheDocument()
})

test('empty collections allow adding media without a combined count limit', () => {
  render(<Collection initial={[]} />)
  expect(screen.getByRole('button', { name: 'Add references' })).toBeEnabled()
  fireEvent.click(screen.getByRole('button', { name: 'Add reference media' }))
  fireEvent.click(
    screen.getByRole('button', { name: 'videoStudio.referenceImage' })
  )
  expect(screen.getByText(/^Up to 3 files,/)).toBeInTheDocument()
})

test.each([
  ['videoStudio.referenceImage', 1],
  ['videoStudio.referenceVideo', 2],
  ['Reference audio', 1],
] as const)(
  'remaining upload capacity is specific to %s',
  (label, remaining) => {
    const mixedProfile: VideoModelProfile = {
      ...profile,
      specification: {
        ...profile.specification,
        reference_policy: {
          ...profile.specification.reference_policy,
          video: {
            allowed_mime_types: ['video/mp4'],
            max_count: 3,
            max_size_bytes: 49_999_999,
            min_duration_seconds: 2,
            max_duration_seconds: 15,
            max_total_duration_seconds: 15,
          },
        },
      },
    }
    const references: VideoComposerReference[] = [
      reference(1),
      { ...reference(2), role: 'first_frame' },
      {
        ...reference(3),
        role: 'reference_video',
        asset: { ...media(3), mime_type: 'video/mp4', duration_seconds: 3 },
      },
      {
        ...reference(4),
        role: 'reference_audio',
        asset: { ...media(4), mime_type: 'audio/mpeg', duration_seconds: 3 },
      },
      { ...reference(5), included: false },
    ]
    render(<Collection initial={references} profile={mixedProfile} />)
    fireEvent.click(screen.getByRole('button', { name: 'Add references' }))
    fireEvent.click(screen.getByRole('button', { name: label }))
    expect(
      screen.getByText(new RegExp(`^Up to ${remaining} files,`))
    ).toBeInTheDocument()
  }
)

test('a stale combined limit cannot block adding a reference within its media count', () => {
  const onChange = vi.fn()
  render(
    <Collection
      initial={[reference(1), reference(2, 1)]}
      profile={{
        ...profile,
        specification: {
          ...profile.specification,
          reference_policy: {
            ...profile.specification.reference_policy,
            max_total_count: 1,
          },
        },
      }}
      onChange={onChange}
    />
  )
  fireEvent.click(screen.getByRole('button', { name: 'Add references' }))
  fireEvent.click(
    screen.getByRole('button', { name: 'videoStudio.referenceImage' })
  )
  fireEvent.click(screen.getByRole('button', { name: 'ready replacement' }))
  fireEvent.click(screen.getByRole('button', { name: 'Add to draft' }))
  expect(onChange).toHaveBeenLastCalledWith([
    expect.objectContaining({ asset_id: 1, position: 0 }),
    expect.objectContaining({ asset_id: 2, position: 1 }),
    expect.objectContaining({ asset_id: 10, position: 2 }),
  ])
})

test('exhausting one media count leaves the other supported media available', () => {
  render(
    <Collection initial={[reference(1), reference(2, 1), reference(3, 2)]} />
  )
  fireEvent.click(screen.getByRole('button', { name: 'Add references' }))
  expect(
    screen.getByRole('button', { name: 'videoStudio.referenceImage' })
  ).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Reference audio' })).toBeEnabled()
  expect(
    screen.getByRole('button', { name: 'videoStudio.referenceVideo' })
  ).toBeDisabled()
})

test('unavailable bindings remain visible even when the model no longer supports their role', () => {
  const refs: VideoComposerReference[] = [
    { ...reference(9), role: 'reference_video', asset: null },
  ]
  render(
    <VideoReferenceCollection
      references={refs}
      profile={profile}
      onChange={vi.fn()}
    />
  )
  expect(screen.getByText('Unavailable reference 9')).toBeInTheDocument()
  expect(
    screen.getByText('Reference unavailable. Replace or remove it to continue.')
  ).toBeInTheDocument()
})
