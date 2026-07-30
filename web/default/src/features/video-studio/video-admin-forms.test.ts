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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  buildVideoSampleProfileState,
  createVideoModelProfileFormValues,
  createVideoSampleFormValues,
  filterVideoModelCandidates,
  parseVideoModelProfileForm,
  parseVideoSampleForm,
  pruneVideoModelParametersForModes,
  videoSampleFormSchema,
} from './schemas'
import type { VideoModelProfile, VideoSample } from './types'

const profile: VideoModelProfile = {
  id: 7,
  model: 'seedance-2.0-720p',
  display_name: 'Seedance 2.0 720p',
  description: '',
  provider_label: 'Seedance',
  specification_version: 3,
  specification: {
    version: 3,
    modes: ['text_to_video', 'image_to_video'],
    parameters: [
      {
        control: 'select',
        key: 'resolution',
        label: 'Resolution',
        default: '720p',
        modes: ['text_to_video', 'image_to_video'],
        options: [
          { label: '720p', value: '720p' },
          { label: '1080p', value: '1080p' },
        ],
      },
      {
        control: 'number',
        key: 'duration',
        label: 'Duration',
        modes: ['text_to_video'],
        min: 1,
        max: 12,
        step: 1,
      },
    ],
    reference_inputs: [
      { role: 'reference', request_key: 'image', required: true },
    ],
  },
  default_parameters: { resolution: '1080p', duration: 5 },
  enabled: true,
  sort_order: 0,
  created_at: 100,
  updated_at: 100,
}

const sample: VideoSample = {
  id: 9,
  model_profile_id: profile.id,
  model: profile.model,
  model_display_name: profile.display_name,
  title: 'Sample',
  prompt: 'A sample prompt',
  mode: 'image_to_video',
  model_version: profile.specification_version,
  parameters: {
    resolution: '1080p',
    duration: 8,
    removed_parameter: true,
  },
  reference_asset_ids: [21, 22],
  reference_content_urls: ['/assets/21', '/assets/22'],
  video_asset_id: 30,
  video_url: '/assets/30',
  poster_url: '/assets/30?variant=poster',
  preview_url: '/assets/30?variant=preview',
  aspect_ratio: 16 / 9,
  category: 'other',
  status: 'draft',
  sort_order: 0,
  created_at: 100,
  updated_at: 100,
}

const alternateProfile: VideoModelProfile = {
  ...profile,
  id: 8,
  model: 'seedance-2.0-pro',
  display_name: 'Seedance 2.0 Pro',
  specification: {
    version: 1,
    modes: ['first_last_frame'],
    parameters: [
      {
        control: 'select',
        key: 'quality',
        label: 'Quality',
        options: [{ label: 'High', value: 'high' }],
      },
    ],
    reference_inputs: [
      { role: 'first_frame', request_key: 'first_frame', required: true },
      { role: 'last_frame', request_key: 'last_frame', required: true },
    ],
  },
  specification_version: 1,
  default_parameters: { quality: 'high' },
}

describe('video model admin form', () => {
  test('offers every unique unconfigured model candidate', () => {
    assert.deepEqual(
      filterVideoModelCandidates(
        [
          'seedance-2.0-1080p',
          profile.model,
          'seedance-2.0-1080p',
          'seedance-2.0-pro',
        ],
        [profile]
      ),
      ['seedance-2.0-1080p', 'seedance-2.0-pro']
    )
  })

  test('keeps an unchanged specification version and bumps a changed one', () => {
    const values = createVideoModelProfileFormValues(profile)
    const unchanged = parseVideoModelProfileForm(values, profile)
    assert.equal(unchanged.specification.version, profile.specification_version)
    assert.deepEqual(
      JSON.parse(JSON.stringify(unchanged.specification)),
      JSON.parse(JSON.stringify(profile.specification))
    )
    assert.deepEqual(unchanged.default_parameters, profile.default_parameters)

    const profileDefaultOnly = createVideoModelProfileFormValues(profile)
    const resolution = profileDefaultOnly.parameters.find(
      (parameter) => parameter.key === 'resolution'
    )
    assert.ok(resolution)
    resolution.default_value = '720p'
    const changedProfileDefault = parseVideoModelProfileForm(
      profileDefaultOnly,
      profile
    )
    assert.equal(
      changedProfileDefault.specification.version,
      profile.specification_version
    )
    assert.deepEqual(
      JSON.parse(JSON.stringify(changedProfileDefault.specification)),
      JSON.parse(JSON.stringify(profile.specification))
    )

    values.display_name = 'Renamed profile'
    assert.equal(
      parseVideoModelProfileForm(values, profile).specification.version,
      profile.specification_version
    )

    values.modes.push('first_last_frame')
    const changed = parseVideoModelProfileForm(values, profile)
    assert.equal(
      changed.specification.version,
      profile.specification_version + 1
    )
  })

  test('forces a newly created profile to remain disabled', () => {
    const values = createVideoModelProfileFormValues(
      undefined,
      'seedance-2.0-1080p'
    )
    values.enabled = true

    assert.equal(parseVideoModelProfileForm(values).enabled, false)
  })

  test('removing a mode deletes parameters that only belonged to that mode', () => {
    const values = createVideoModelProfileFormValues(profile)
    const parameters = pruneVideoModelParametersForModes(values.parameters, [
      'image_to_video',
    ])

    assert.deepEqual(
      parameters.map((parameter) => ({
        key: parameter.key,
        modes: parameter.modes,
      })),
      [{ key: 'resolution', modes: ['image_to_video'] }]
    )
  })
})

describe('video sample admin form', () => {
  test('normalizes saved sample parameters and references to its profile', () => {
    const values = createVideoSampleFormValues(sample, profile)

    assert.deepEqual(values.parameters, { resolution: '1080p' })
    assert.deepEqual(values.reference_asset_ids, [21])
  })

  test('normalizes parameters and references when the mode changes', () => {
    const values = buildVideoSampleProfileState(
      profile,
      'text_to_video',
      { resolution: '1080p', duration: 99, removed_parameter: true },
      [21, 22]
    )

    assert.deepEqual(values.parameters, {
      resolution: '1080p',
      duration: 5,
    })
    assert.deepEqual(values.reference_asset_ids, [])
  })

  test('starts a newly selected model from its own defaults and no references', () => {
    assert.deepEqual(buildVideoSampleProfileState(alternateProfile), {
      model_profile_id: alternateProfile.id,
      mode: 'first_last_frame',
      parameters: { quality: 'high' },
      reference_asset_ids: [],
    })
  })

  test('accepts structured parameters and rejects the removed JSON-only field', () => {
    const structured = videoSampleFormSchema.parse(
      createVideoSampleFormValues(sample, profile)
    )
    assert.deepEqual(parseVideoSampleForm(structured).parameters, {
      resolution: '1080p',
    })

    assert.equal(
      videoSampleFormSchema.safeParse({
        ...createVideoSampleFormValues(sample, profile),
        parameters: undefined,
        parameters_json: '{"resolution":"720p"}',
      }).success,
      false
    )
  })
})
