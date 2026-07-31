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
  getVideoModelCandidateLabel,
  getVideoModelPreset,
  parseVideoModelProfileForm,
  parseVideoSampleForm,
  videoModelProfileFormSchema,
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
    const configured = {
      ...profile,
      model: 'sd_2.0_special_720p',
    }
    assert.deepEqual(
      filterVideoModelCandidates(
        [
          'sd_2.0_special_1080p',
          configured.model,
          'sd_2.0_special_1080p',
          'sd_2.0_special_4k_with_video_ref',
          'unsupported-video-model',
        ],
        [configured]
      ),
      ['sd_2.0_special_1080p', 'sd_2.0_special_4k_with_video_ref']
    )
  })

  test('builds verified presets for every supported Seedance special model', () => {
    const models = [
      ['sd_2.0_fast_special_720p', '720p', false],
      ['sd_2.0_special_720p', '720p', false],
      ['sd_2.0_special_1080p', '1080p', false],
      ['sd_2.0_special_2k', '2K', false],
      ['sd_2.0_special_4k', '4K', false],
      ['sd_2.0_fast_special_720p_with_video_ref', '720p', true],
      ['sd_2.0_special_720p_with_video_ref', '720p', true],
      ['sd_2.0_special_1080p_with_video_ref', '1080p', true],
      ['sd_2.0_special_2k_with_video_ref', '2K', true],
      ['sd_2.0_special_4k_with_video_ref', '4K', true],
    ] as const

    for (const [model, resolution, requiresVideoReference] of models) {
      const preset = getVideoModelPreset(model)
      assert.ok(preset)
      assert.equal(preset.resolution, resolution)
      assert.deepEqual(
        preset.specification.modes,
        requiresVideoReference
          ? ['image_to_video']
          : ['text_to_video', 'image_to_video']
      )
      assert.deepEqual(
        preset.specification.parameters.map((parameter) => parameter.key),
        ['duration', 'ratio', 'generate_audio']
      )
      assert.deepEqual(preset.default_parameters, {
        duration: 5,
        ratio: '16:9',
        generate_audio: true,
      })
      assert.deepEqual(preset.specification.reference_inputs, [
        {
          role: requiresVideoReference ? 'reference_video' : 'reference',
          request_key: requiresVideoReference
            ? 'reference_video'
            : 'reference_image',
          required: true,
        },
      ])

      const duration = preset.specification.parameters[0]
      assert.equal(duration?.control, 'number')
      if (duration?.control !== 'number') {
        assert.fail('duration preset is not numeric')
      }
      assert.equal(duration.request_key, 'seconds')
      assert.equal(duration.required, true)
      assert.deepEqual([duration.min, duration.max, duration.step], [4, 15, 1])

      const ratio = preset.specification.parameters[1]
      assert.equal(ratio?.control, 'select')
      if (ratio?.control !== 'select') {
        assert.fail('ratio preset is not a choice')
      }
      assert.deepEqual(
        ratio.options.map((option) => option.value),
        ['16:9', '9:16', '1:1', '4:3', '3:4', '21:9', 'adaptive']
      )

      const values = videoModelProfileFormSchema.parse(
        createVideoModelProfileFormValues(undefined, model)
      )
      const input = parseVideoModelProfileForm(values)
      assert.deepEqual(input.specification.modes, preset.specification.modes)
      assert.deepEqual(
        input.specification.parameters.map((parameter) => ({
          key: parameter.key,
          request_key: parameter.request_key,
          control: parameter.control,
          required: parameter.required,
        })),
        preset.specification.parameters.map((parameter) => ({
          key: parameter.key,
          request_key: parameter.request_key,
          control: parameter.control,
          required: parameter.required,
        }))
      )
      assert.deepEqual(
        input.specification.reference_inputs,
        preset.specification.reference_inputs
      )
      assert.deepEqual(input.default_parameters, preset.default_parameters)
    }
  })

  test('keeps unknown candidates clean instead of inventing a protocol', () => {
    const model = 'future-video-model_with_video_ref'
    assert.equal(getVideoModelPreset(model), undefined)
    assert.equal(getVideoModelCandidateLabel(model), model)

    const values = createVideoModelProfileFormValues(undefined, model)
    assert.deepEqual(values.parameters, [])
    assert.deepEqual(values.modes, ['text_to_video'])
    assert.equal(values.image_reference_request_key, 'reference_image')
    assert.doesNotThrow(() =>
      parseVideoModelProfileForm(videoModelProfileFormSchema.parse(values))
    )
  })

  test('normalizes an existing supported model to the fixed preset', () => {
    const existing: VideoModelProfile = {
      ...profile,
      id: 11,
      model: 'sd_2.0_special_1080p',
      display_name: 'Legacy 1080p',
      specification_version: 3,
      specification: {
        version: 3,
        modes: ['text_to_video'],
        parameters: [
          {
            control: 'number',
            key: 'duration',
            label: 'Duration',
            request_key: 'seconds',
            default: 8,
            min: 4,
            max: 15,
            step: 1,
          },
          {
            control: 'select',
            key: 'ratio',
            label: 'Ratio',
            default: '4:3',
            options: [
              { label: '4:3', value: '4:3' },
              { label: '9:16', value: '9:16' },
            ],
          },
          {
            control: 'switch',
            key: 'generate_audio',
            label: 'Generate audio',
            default: false,
          },
          {
            control: 'select',
            key: 'parameter_1',
            label: '参数 1',
            options: [{ label: 'Default', value: 'default' }],
            default: 'default',
          },
        ],
      },
      default_parameters: {
        ratio: '9:16',
        obsolete: true,
      },
    }

    const values = videoModelProfileFormSchema.parse(
      createVideoModelProfileFormValues(existing)
    )
    assert.deepEqual(values.modes, ['text_to_video', 'image_to_video'])
    assert.deepEqual(
      values.parameters.map((parameter) => [
        parameter.key,
        parameter.default_value,
      ]),
      [
        ['duration', 8],
        ['ratio', '9:16'],
        ['generate_audio', false],
      ]
    )

    const invalidOverride = createVideoModelProfileFormValues({
      ...existing,
      default_parameters: {
        ...existing.default_parameters,
        duration: 99,
      },
    })
    assert.equal(
      invalidOverride.parameters.find(
        (parameter) => parameter.key === 'duration'
      )?.default_value,
      5
    )

    const input = parseVideoModelProfileForm(values, existing)
    assert.equal(input.specification.version, 4)
    assert.deepEqual(input.specification.reference_inputs, [
      {
        role: 'reference',
        request_key: 'reference_image',
        required: true,
      },
    ])
    assert.deepEqual(input.default_parameters, {
      duration: 8,
      ratio: '9:16',
      generate_audio: false,
    })
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
      'sd_2.0_special_1080p'
    )
    values.enabled = true

    assert.equal(parseVideoModelProfileForm(values).enabled, false)
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
