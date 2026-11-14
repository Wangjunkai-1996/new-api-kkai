/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import assert from 'node:assert/strict'

import { describe, test } from 'vitest'

import {
  buildVideoSampleProfileState,
  createVideoModelProfileFormValues,
  createVideoSampleFormValues,
  filterVideoModelCandidates,
  getVideoModelPreset,
  parseVideoModelProfileForm,
  parseVideoSampleForm,
  videoModelProfileFormSchema,
  videoSampleFormSchema,
} from './schemas'
import type { VideoModelProfile, VideoSample } from './types'

const policy = {
  max_total_count: 6,
  requires_visual: true,
  requires_video: false,
  audio_requires_generate_audio: true,
  image: {
    allowed_mime_types: ['image/jpeg', 'image/png'],
    max_count: 3,
    max_size_bytes: 20_971_520,
  },
  video: {
    allowed_mime_types: ['video/mp4'],
    max_count: 2,
    max_size_bytes: 49_999_999,
    min_duration_seconds: 2,
    max_duration_seconds: 15,
    max_total_duration_seconds: 15,
    min_frame_rate: 24,
    max_frame_rate: 60,
  },
  audio: {
    allowed_mime_types: ['audio/mpeg', 'audio/wav'],
    max_count: 1,
    max_size_bytes: 14_999_999,
    min_duration_seconds: 2,
    max_duration_seconds: 15,
    max_total_duration_seconds: 15,
  },
}

const profile: VideoModelProfile = {
  id: 7,
  model: 'sd_2.5_special_720p',
  display_name: 'Seedance 2.5 720p',
  description: '',
  provider_label: 'Seedance',
  specification_version: 3,
  specification: {
    version: 3,
    modes: ['text_to_video', 'reference_to_video'],
    parameters: [
      {
        control: 'select',
        key: 'resolution',
        label: 'Resolution',
        default: '720p',
        modes: ['text_to_video', 'reference_to_video'],
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
    reference_policy: policy,
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
  mode: 'reference_to_video',
  model_version: profile.specification_version,
  parameters: { resolution: '1080p', duration: 8, removed_parameter: true },
  reference_asset_ids: [21, 22],
  reference_content_urls: ['/assets/21', '/assets/22'],
  reference_assets: [
    { asset_id: 21, role: 'reference_image', position: 0 },
    { asset_id: 22, role: 'reference_audio', position: 0 },
  ],
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

describe('video model admin form', () => {
  test('offers only known, unconfigured server candidates', () => {
    assert.deepEqual(
      filterVideoModelCandidates(
        ['sd_2.0_special_1080p', 'sd_2.0_special_1080p', 'unknown-model'],
        [profile]
      ),
      ['sd_2.0_special_1080p']
    )
  })

  test('candidate presets expose multimodal policy and remain disabled until verified', () => {
    const preset = getVideoModelPreset('sd_2.0_special_720p')
    assert.ok(preset)
    assert.deepEqual(preset.specification.modes, ['text_to_video', 'reference_to_video'])
    assert.equal(preset.specification.reference_policy?.max_total_count, null)
    assert.equal(preset.enabled, false)
    assert.deepEqual(preset.specification.reference_policy?.audio?.allowed_mime_types, ['audio/mpeg', 'audio/wav'])
  })

  test('profile editing uses the server specification rather than replacing it with a preset', () => {
    const values = createVideoModelProfileFormValues(profile)
    assert.deepEqual(values.modes, profile.specification.modes)
    assert.deepEqual(values.reference_policy, profile.specification.reference_policy)
  })

  test('does not allow enabling an unverified aggregate reference limit', () => {
    const values = createVideoModelProfileFormValues(undefined, 'sd_2.0_special_720p')
    values.enabled = true
    assert.equal(videoModelProfileFormSchema.safeParse(values).success, false)
  })

  test('bumps specification version only when policy changes', () => {
    const values = videoModelProfileFormSchema.parse(createVideoModelProfileFormValues(profile))
    assert.equal(parseVideoModelProfileForm(values, profile).specification.version, 3)
    assert.ok(values.reference_policy)
    values.reference_policy = { ...values.reference_policy, max_total_count: 5 }
    assert.equal(parseVideoModelProfileForm(values, profile).specification.version, 4)
  })
})

describe('video sample admin form', () => {
  test('restores ordered multimodal reference bindings', () => {
    const values = createVideoSampleFormValues(sample, profile)
    assert.deepEqual(values.reference_assets, [
      { asset_id: 21, role: 'reference_image', position: 0 },
      { asset_id: 22, role: 'reference_audio', position: 0 },
    ])
    assert.deepEqual(values.parameters, { resolution: '1080p' })
  })

  test('preserves explicit ordered bindings when changing mode', () => {
    const values = buildVideoSampleProfileState(profile, 'text_to_video', {
      resolution: '1080p',
      duration: 99,
      removed_parameter: true,
    }, [{ asset_id: 21, role: 'reference_image', position: 0 }])
    assert.deepEqual(values.parameters, { resolution: '1080p', duration: 5 })
    assert.deepEqual(values.reference_assets, [{ asset_id: 21, role: 'reference_image', position: 0 }])
  })

  test('requires structured reference bindings and rejects the removed JSON field', () => {
    const values = createVideoSampleFormValues(sample, profile)
    const structured = videoSampleFormSchema.parse({ ...values, video_asset_id: 30 })
    assert.deepEqual(parseVideoSampleForm(structured).reference_assets, values.reference_assets)
    assert.equal(videoSampleFormSchema.safeParse({
      ...values,
      reference_assets: [21],
      parameters: undefined,
      parameters_json: '{}',
    }).success, false)
  })

  test('keeps sample title and sort-order bounds', () => {
    const values = { ...createVideoSampleFormValues(undefined, profile), prompt: 'sample', video_asset_id: 1 }
    assert.equal(videoSampleFormSchema.safeParse({ ...values, title: 't'.repeat(191), sort_order: -100000 }).success, true)
    assert.equal(videoSampleFormSchema.safeParse({ ...values, title: 't'.repeat(192) }).success, false)
    assert.equal(videoSampleFormSchema.safeParse({ ...values, sort_order: 100001 }).success, false)
  })
})
