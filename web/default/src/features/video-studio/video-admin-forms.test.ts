/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import assert from 'node:assert/strict'

import { describe, expect, test } from 'vitest'

import {
  buildVideoSampleProfileState,
  applyVideoModelReferencePreset,
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

const videoPolicy = {
  max_total_count: 6,
  requires_visual: true,
  requires_video: true,
  audio_requires_generate_audio: true,
  image: {
    allowed_mime_types: ['image/jpeg', 'image/png'],
    max_count: 3,
    max_size_bytes: 20_971_520,
    min_width: 301,
    max_width: 5999,
    min_height: 301,
    max_height: 5999,
    min_aspect_ratio_exclusive: 0.4,
    max_aspect_ratio_exclusive: 2.5,
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

const policy = { ...videoPolicy, requires_video: false, video: undefined }

const profile: VideoModelProfile = {
  id: 7,
  model: 'seedance-2.5',
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

const videoPreset = getVideoModelPreset('sd_2.0_special_720p_with_video_ref')
assert.ok(videoPreset)
const videoProfile: VideoModelProfile = {
  ...profile,
  ...videoPreset,
  specification: {
    ...videoPreset.specification,
    version: 3,
    reference_policy: videoPolicy,
  },
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

  test('candidate presets expose per-kind limits and default to disabled', () => {
    const preset = getVideoModelPreset('sd_2.0_special_720p')
    assert.ok(preset)
    assert.deepEqual(preset.specification.modes, [
      'text_to_video',
      'reference_to_video',
    ])
    assert.equal(preset.specification.reference_policy?.max_total_count, null)
    assert.equal(preset.enabled, false)
    assert.deepEqual(
      preset.specification.reference_policy?.audio?.allowed_mime_types,
      ['audio/mpeg', 'audio/wav']
    )
  })

  test.each([
    ['sd_2.0_fast_special_720p', false],
    ['sd_2.0_special_720p', false],
    ['sd_2.0_special_1080p', false],
    ['sd_2.0_special_2k', false],
    ['sd_2.0_special_4k', false],
    ['seedance-2.5', false],
    ['sd_2.0_fast_special_720p_with_video_ref', true],
    ['sd_2.0_special_720p_with_video_ref', true],
    ['sd_2.0_special_1080p_with_video_ref', true],
    ['sd_2.0_special_2k_with_video_ref', true],
    ['sd_2.0_special_4k_with_video_ref', true],
  ] as const)(
    'limits %s to its supported reference media',
    (model, requiresVideo) => {
      const preset = getVideoModelPreset(model)
      assert.ok(preset)
      const referencePolicy = preset.specification.reference_policy
      assert.ok(referencePolicy)
      expect(
        (['image', 'video', 'audio'] as const).filter(
          (kind) => referencePolicy[kind]
        )
      ).toEqual(
        requiresVideo ? ['image', 'video', 'audio'] : ['image', 'audio']
      )
      expect(referencePolicy.requires_video).toBe(requiresVideo)
      const values = createVideoModelProfileFormValues(undefined, model)
      expect(videoModelProfileFormSchema.safeParse(values).success).toBe(true)
      if (!requiresVideo) {
        values.reference_policy = {
          ...referencePolicy,
          video: videoPolicy.video,
        }
        expect(videoModelProfileFormSchema.safeParse(values).success).toBe(
          false
        )
      }
    }
  )

  test.each([
    'sd_2.5_special_720p',
    'sd_2.5_special_1080p',
    'sd_2.5_special_720p_with_video_ref',
    'sd_2.5_special_1080p_with_video_ref',
  ])('does not offer an unreviewed alias %s as a preset', (model) => {
    expect(getVideoModelPreset(model)).toBeUndefined()
    expect(filterVideoModelCandidates([model], [])).toEqual([])
  })

  test.each(['seedance-2.5', 'sd_2.5_special_720p'])(
    'profile editing preserves the server specification for %s',
    (model) => {
      const values = createVideoModelProfileFormValues({ ...profile, model })
      assert.deepEqual(values.modes, profile.specification.modes)
      assert.deepEqual(
        values.reference_policy,
        profile.specification.reference_policy
      )
    }
  )

  test.each([true, false])(
    'explicit reference upgrade preserves enabled=%s and existing settings',
    (enabled) => {
      const legacy: VideoModelProfile = {
        ...profile,
        enabled,
        specification: {
          ...profile.specification,
          modes: ['text_to_video', 'image_to_video'],
          reference_policy: undefined,
          parameters: profile.specification.parameters.map((parameter) => ({
            ...parameter,
            modes: ['text_to_video', 'image_to_video'],
          })),
        },
      }
      const initial = createVideoModelProfileFormValues(legacy)
      expect(initial.reference_policy).toBeUndefined()
      const upgraded = applyVideoModelReferencePreset(initial)
      const saved = parseVideoModelProfileForm(
        videoModelProfileFormSchema.parse(upgraded),
        legacy
      )
      expect(saved.enabled).toBe(enabled)
      expect(saved.display_name).toBe(legacy.display_name)
      expect(saved.sort_order).toBe(legacy.sort_order)
      expect(saved.default_parameters).toMatchObject(legacy.default_parameters)
      expect(saved.specification.version).toBe(legacy.specification_version + 1)
      expect(saved.specification.modes).toEqual([
        'text_to_video',
        'reference_to_video',
      ])
      expect(saved.specification.parameters.slice(0, 2)).toEqual(
        legacy.specification.parameters.map((parameter) => ({
          ...parameter,
          modes: ['text_to_video', 'reference_to_video'],
        }))
      )
      expect(
        saved.specification.parameters.some(
          (parameter) => parameter.key === 'generate_audio'
        )
      ).toBe(true)
      expect(saved.specification.reference_policy?.image?.max_count).toBe(9)
      expect(initial.reference_policy).toBeUndefined()
      expect(applyVideoModelReferencePreset(upgraded)).toBe(upgraded)
    }
  )

  test.each([null, 0, -1, 1])(
    'stored aggregate value %s does not prevent policy editing',
    (max_total_count) => {
      const values = createVideoModelProfileFormValues(profile)
      values.reference_policy = { ...policy, max_total_count }
      expect(videoModelProfileFormSchema.safeParse(values).success).toBe(true)
    }
  )

  test('saves per-kind limits and rejects inverted durations or MIME type mismatch', () => {
    const values = createVideoModelProfileFormValues(videoProfile)
    const edited = structuredClone(videoPolicy)
    values.reference_policy = edited
    edited.image.max_count = 9
    edited.video.max_count = 3
    edited.audio.max_count = 3
    edited.video.max_duration_seconds = 10
    edited.video.max_total_duration_seconds = 14
    edited.audio.max_duration_seconds = 11
    edited.audio.max_total_duration_seconds = 13
    const saved = parseVideoModelProfileForm(
      videoModelProfileFormSchema.parse(values),
      videoProfile
    )
    expect(saved.specification.reference_policy).toEqual(
      values.reference_policy
    )
    edited.video.max_total_duration_seconds = 9
    expect(videoModelProfileFormSchema.safeParse(values).success).toBe(false)
    edited.video.max_total_duration_seconds = 14
    edited.audio.allowed_mime_types = ['image/png']
    expect(videoModelProfileFormSchema.safeParse(values).success).toBe(false)
  })

  test.each([
    ['image', 'max_count', 10],
    ['image', 'max_size_bytes', 20_971_521],
    ['image', 'min_width', 300],
    ['image', 'max_width', 6000],
    ['image', 'min_height', 300],
    ['image', 'max_height', 6000],
    ['image', 'min_aspect_ratio_exclusive', 0.3],
    ['image', 'max_aspect_ratio_exclusive', 2.6],
    ['video', 'max_count', 4],
    ['video', 'max_size_bytes', 50_000_000],
    ['video', 'min_duration_seconds', 1],
    ['video', 'max_duration_seconds', 16],
    ['video', 'max_total_duration_seconds', 16],
    ['video', 'min_frame_rate', 23],
    ['video', 'max_frame_rate', 61],
    ['audio', 'max_count', 4],
    ['audio', 'max_size_bytes', 15_000_000],
    ['audio', 'min_duration_seconds', 1],
    ['audio', 'max_duration_seconds', 16],
    ['audio', 'max_total_duration_seconds', 16],
  ] as const)(
    'rejects widening the documented %s %s bound',
    (kind, key, value) => {
      const values = createVideoModelProfileFormValues(videoProfile)
      const edited = structuredClone(videoPolicy)
      Object.assign(edited[kind], { [key]: value })
      values.reference_policy = edited
      expect(videoModelProfileFormSchema.safeParse(values).success).toBe(false)
    }
  )

  test.each([
    'requires_visual',
    'requires_video',
    'audio_requires_generate_audio',
  ] as const)('does not disable the required %s rule', (key) => {
    const values = createVideoModelProfileFormValues(
      undefined,
      'sd_2.0_special_720p_with_video_ref'
    )
    expect(values.reference_policy).toBeDefined()
    if (!values.reference_policy) throw new Error('Expected reference policy')
    values.reference_policy[key] = false
    expect(videoModelProfileFormSchema.safeParse(values).success).toBe(false)
  })

  test('restricts known model MIME types while allowing custom model capabilities', () => {
    const values = createVideoModelProfileFormValues(profile)
    const edited = structuredClone(policy)
    edited.image.allowed_mime_types = ['image/tiff']
    values.reference_policy = edited
    expect(videoModelProfileFormSchema.safeParse(values).success).toBe(false)
    values.model = 'custom-video'
    edited.image.max_count = 20
    expect(videoModelProfileFormSchema.safeParse(values).success).toBe(true)
  })

  test('allows enabling a model with per-kind limits and no aggregate reference limit', () => {
    const values = createVideoModelProfileFormValues(
      undefined,
      'sd_2.0_special_720p'
    )
    values.enabled = true
    const parsed = videoModelProfileFormSchema.parse(values)
    assert.equal(parsed.reference_policy?.max_total_count, null)
    assert.equal(parseVideoModelProfileForm(parsed).enabled, true)
  })

  test('still requires a reference policy for reference mode', () => {
    const values = createVideoModelProfileFormValues(
      undefined,
      'sd_2.0_special_720p'
    )
    values.enabled = true
    values.reference_policy = undefined
    assert.equal(videoModelProfileFormSchema.safeParse(values).success, false)
  })

  test('bumps specification version only when policy changes', () => {
    const values = videoModelProfileFormSchema.parse(
      createVideoModelProfileFormValues(profile)
    )
    assert.equal(
      parseVideoModelProfileForm(values, profile).specification.version,
      3
    )
    assert.ok(values.reference_policy)
    values.reference_policy = {
      ...values.reference_policy,
      image: { ...policy.image, max_count: 4 },
    }
    assert.equal(
      parseVideoModelProfileForm(values, profile).specification.version,
      4
    )
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
    const values = buildVideoSampleProfileState(
      profile,
      'text_to_video',
      {
        resolution: '1080p',
        duration: 99,
        removed_parameter: true,
      },
      [{ asset_id: 21, role: 'reference_image', position: 0 }]
    )
    assert.deepEqual(values.parameters, { resolution: '1080p', duration: 5 })
    assert.deepEqual(values.reference_assets, [
      { asset_id: 21, role: 'reference_image', position: 0 },
    ])
  })

  test('requires structured reference bindings and rejects the removed JSON field', () => {
    const values = createVideoSampleFormValues(sample, profile)
    const structured = videoSampleFormSchema.parse({
      ...values,
      video_asset_id: 30,
    })
    assert.deepEqual(
      parseVideoSampleForm(structured).reference_assets,
      values.reference_assets
    )
    assert.equal(
      videoSampleFormSchema.safeParse({
        ...values,
        reference_assets: [21],
        parameters: undefined,
        parameters_json: '{}',
      }).success,
      false
    )
  })

  test('keeps sample title and sort-order bounds', () => {
    const values = {
      ...createVideoSampleFormValues(undefined, profile),
      prompt: 'sample',
      video_asset_id: 1,
    }
    assert.equal(
      videoSampleFormSchema.safeParse({
        ...values,
        title: 't'.repeat(191),
        sort_order: -100000,
      }).success,
      true
    )
    assert.equal(
      videoSampleFormSchema.safeParse({ ...values, title: 't'.repeat(192) })
        .success,
      false
    )
    assert.equal(
      videoSampleFormSchema.safeParse({ ...values, sort_order: 100001 })
        .success,
      false
    )
  })
})
