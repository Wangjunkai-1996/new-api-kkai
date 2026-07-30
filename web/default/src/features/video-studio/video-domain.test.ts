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
import test from 'node:test'

import { validateComposerForProfile } from './schemas'
import type {
  VideoAsset,
  VideoGeneration,
  VideoModelProfile,
  VideoSample,
} from './types'
import {
  buildCreateVideoRequest,
  buildClearedVideoComposerValues,
  buildVideoComposerValues,
  buildVideoQuoteRequest,
  canAccessVideoStudio,
  decodeVideoParameterOptionValue,
  encodeVideoParameterOptionValue,
  getVideoQuoteRefreshDelay,
  getVideoReferenceRoles,
  getVideoAssetInspectionPollInterval,
  getVideoSubmissionRequestKey,
  getVideoSubmissionLock,
  getVideoParametersForMode,
  getVideoTaskPollInterval,
  isVideoAssetInspectionPending,
  isVideoAssetInspectionTakingLong,
  isVideoQuoteStaleError,
  normalizeVideoStudioAccessMode,
  restoreVideoComposerDraft,
  shouldRenderVideoAssetMedia,
} from './video-domain'

const profile: VideoModelProfile = {
  id: 1,
  model: 'video-model',
  display_name: 'Video Model',
  description: '',
  provider_label: 'Provider',
  specification_version: 1,
  specification: {
    version: 1,
    modes: ['text_to_video', 'image_to_video', 'first_last_frame'],
    parameters: [
      {
        control: 'select',
        key: 'quality',
        label: 'Quality',
        default: 'standard',
        options: [{ label: 'Standard', value: 'standard' }],
      },
      {
        control: 'number',
        key: 'duration',
        label: 'Duration',
        default: 5,
        min: 1,
        max: 15,
        step: 1,
      },
    ],
    reference_inputs: [
      { role: 'reference', request_key: 'image', required: true },
      { role: 'first_frame', request_key: 'first_image', required: true },
      { role: 'last_frame', request_key: 'last_image', required: true },
    ],
  },
  default_parameters: { duration: 5 },
  enabled: true,
  sort_order: 0,
  created_at: 100,
  updated_at: 100,
}

const generation = (
  overrides: Partial<VideoGeneration> = {}
): VideoGeneration => ({
  id: 1,
  task_id: 'task-1',
  model_profile_id: profile.id,
  model: profile.model,
  prompt: 'Prompt',
  mode: 'text_to_video',
  parameters: {},
  status: 'queued',
  progress: '0%',
  quota: 100,
  created_at: 100,
  updated_at: 100,
  ...overrides,
})

const asset = (state: VideoAsset['state']): VideoAsset => ({
  id: 1,
  scope: 'user',
  kind: 'reference',
  state,
  original_filename: 'reference.jpg',
  mime_type: 'image/jpeg',
  size_bytes: 10,
  width: 1,
  height: 1,
  duration_seconds: 0,
  codec: '',
  created_at: 100,
  updated_at: 100,
})

test('sample reuse keeps profile defaults and overlays sample parameters', () => {
  const sample: VideoSample = {
    id: 2,
    model_profile_id: profile.id,
    model: profile.model,
    model_display_name: profile.display_name,
    title: 'Storm lighthouse',
    prompt: 'A lighthouse in a storm',
    mode: 'image_to_video',
    model_version: 1,
    parameters: { duration: 8 },
    reference_asset_ids: [3],
    reference_content_urls: ['/api/video-studio/assets/3/content'],
    video_asset_id: 4,
    video_url: '/api/video-studio/assets/4/content',
    poster_url: '/api/video-studio/assets/4/content?variant=poster',
    preview_url: '/api/video-studio/assets/4/content?variant=preview',
    aspect_ratio: 16 / 9,
    category: 'nature',
    status: 'published',
    sort_order: 0,
    created_at: 100,
    updated_at: 100,
  }

  assert.deepEqual(buildVideoComposerValues(profile, sample), {
    model_profile_id: profile.id,
    mode: 'image_to_video',
    prompt: sample.prompt,
    reference_asset_ids: [3],
    parameters: { duration: 8, quality: 'standard' },
  })
})

test('draft restore follows the persisted profile across model reordering', () => {
  const selectedProfile: VideoModelProfile = {
    ...profile,
    id: 2,
    model: 'selected-video-model',
    display_name: 'Selected Video Model',
  }
  const draft = {
    model_profile_id: selectedProfile.id,
    mode: 'image_to_video' as const,
    prompt: 'Persist this draft',
    reference_asset_ids: [42],
    parameters: { duration: 8, quality: 'standard' },
  }

  assert.deepEqual(
    restoreVideoComposerDraft([profile, selectedProfile], draft),
    restoreVideoComposerDraft([selectedProfile, profile], draft)
  )
  assert.deepEqual(
    restoreVideoComposerDraft([profile, selectedProfile], draft),
    {
      ...draft,
      parameters: { duration: 8, quality: 'standard' },
    }
  )
})

test('draft restore drops a model that is outside the bound key catalog', () => {
  const unavailableProfile: VideoModelProfile = {
    ...profile,
    id: 2,
    model: 'other-key-only-model',
  }
  const restored = restoreVideoComposerDraft([profile], {
    model_profile_id: unavailableProfile.id,
    mode: 'image_to_video',
    prompt: 'Keep only the prompt',
    reference_asset_ids: [42],
    parameters: { duration: 12 },
  })

  assert.deepEqual(restored, {
    model_profile_id: profile.id,
    mode: 'text_to_video',
    prompt: '',
    reference_asset_ids: [],
    parameters: { duration: 5, quality: 'standard' },
  })
})

test('draft refresh preserves reference IDs and revalidates updated model parameters', () => {
  const updatedProfile: VideoModelProfile = {
    ...profile,
    specification_version: 2,
    specification: {
      ...profile.specification,
      parameters: profile.specification.parameters.map((parameter) =>
        parameter.key === 'duration' && parameter.control === 'number'
          ? { ...parameter, max: 6 }
          : parameter
      ),
    },
  }
  const restored = restoreVideoComposerDraft([updatedProfile], {
    model_profile_id: updatedProfile.id,
    mode: 'image_to_video',
    prompt: 'Restore the uploaded image',
    reference_asset_ids: [77],
    parameters: { duration: 12, quality: 'removed-option', obsolete: true },
  })

  assert.deepEqual(restored, {
    model_profile_id: updatedProfile.id,
    mode: 'image_to_video',
    prompt: 'Restore the uploaded image',
    reference_asset_ids: [77],
    parameters: { duration: 5, quality: 'standard' },
  })
  assert.equal(
    restored && validateComposerForProfile(restored, updatedProfile),
    null
  )
})

test('clearing a broken draft preserves its current reference mode with empty inputs', () => {
  assert.deepEqual(
    buildClearedVideoComposerValues(profile, 'first_last_frame'),
    {
      model_profile_id: profile.id,
      mode: 'first_last_frame',
      prompt: '',
      reference_asset_ids: [],
      parameters: { duration: 5, quality: 'standard' },
    }
  )
})

test('quote request maps first and last frame assets by canonical role order', () => {
  const reversedReferenceProfile: VideoModelProfile = {
    ...profile,
    specification: {
      ...profile.specification,
      reference_inputs: [
        { role: 'last_frame', request_key: 'last_image', required: true },
        { role: 'first_frame', request_key: 'first_image', required: true },
        { role: 'first_frame', request_key: 'duplicate_first', required: true },
      ],
    },
  }

  assert.deepEqual(
    getVideoReferenceRoles(reversedReferenceProfile, 'first_last_frame'),
    ['first_frame', 'last_frame']
  )
  assert.deepEqual(
    buildVideoQuoteRequest(
      {
        model_profile_id: profile.id,
        mode: 'first_last_frame',
        prompt: 'Day becomes night',
        reference_asset_ids: [11, 12],
        parameters: { duration: 5, quality: 'standard' },
      },
      reversedReferenceProfile,
      42,
      9
    ),
    {
      token_id: 42,
      model: profile.model,
      mode: 'first_last_frame',
      prompt: 'Day becomes night',
      parameters: { duration: 5, quality: 'standard' },
      reference_assets: [
        { asset_id: 11, role: 'first_frame' },
        { asset_id: 12, role: 'last_frame' },
      ],
      sample_id: 9,
    }
  )
})

test('video-reference runtime profiles submit the reference_video role', () => {
  const videoReferenceProfile: VideoModelProfile = {
    ...profile,
    id: -42,
    model: 'sd_2.0_special_1080p_with_video_ref',
    specification: {
      version: 1,
      modes: ['image_to_video'],
      parameters: [],
      reference_inputs: [
        {
          role: 'reference_video',
          request_key: 'reference_video',
          required: true,
        },
      ],
    },
  }

  assert.deepEqual(
    getVideoReferenceRoles(videoReferenceProfile, 'image_to_video'),
    ['reference_video']
  )
  assert.equal(
    validateComposerForProfile(
      {
        model_profile_id: videoReferenceProfile.id,
        mode: 'image_to_video',
        prompt: 'Extend this video',
        reference_asset_ids: [77],
        parameters: {},
      },
      videoReferenceProfile
    ),
    null
  )
  assert.deepEqual(
    buildVideoQuoteRequest(
      {
        model_profile_id: videoReferenceProfile.id,
        mode: 'image_to_video',
        prompt: 'Extend this video',
        reference_asset_ids: [77],
        parameters: {},
      },
      videoReferenceProfile,
      42
    ).reference_assets,
    [{ asset_id: 77, role: 'reference_video' }]
  )
})

test('create request carries the complete quote contract', () => {
  const request = buildVideoQuoteRequest(
    {
      model_profile_id: profile.id,
      mode: 'text_to_video',
      prompt: 'Ocean at sunrise',
      reference_asset_ids: [],
      parameters: { duration: 5, quality: 'standard' },
    },
    profile,
    42
  )

  assert.deepEqual(
    buildCreateVideoRequest(request, {
      quota: 120,
      request_hash: 'signed-quote',
      expires_at: 500,
    }),
    {
      ...request,
      max_quota: 120,
      quote_hash: 'signed-quote',
      quote_expires_at: 500,
    }
  )
})

test('submission request key is stable across parameter insertion order', () => {
  const request = {
    token_id: 42,
    model: 'video-model',
    mode: 'text_to_video' as const,
    prompt: 'Ocean at sunrise',
    reference_assets: [],
    parameters: { duration: 5, quality: 'standard' },
  }

  assert.equal(
    getVideoSubmissionRequestKey(request),
    getVideoSubmissionRequestKey({
      ...request,
      parameters: { quality: 'standard', duration: 5 },
    })
  )
  assert.notEqual(
    getVideoSubmissionRequestKey(request),
    getVideoSubmissionRequestKey({ ...request, token_id: 43 })
  )
})

test('choice values preserve scalar types without encoded collisions', () => {
  const options = [
    { label: 'String one', value: '1' },
    { label: 'Number one', value: 1 },
    { label: 'String false', value: 'false' },
    { label: 'Boolean false', value: false },
  ]
  const encoded = options.map((option) =>
    encodeVideoParameterOptionValue(option.value)
  )

  assert.equal(new Set(encoded).size, options.length)
  for (const [index, option] of options.entries()) {
    assert.equal(
      decodeVideoParameterOptionValue(options, encoded[index]),
      option.value
    )
  }
  assert.equal(
    decodeVideoParameterOptionValue(options, 'string:missing'),
    undefined
  )
})

test('quote expiry delay closes the safety window deterministically', () => {
  assert.equal(getVideoQuoteRefreshDelay(10, 7_000), 1_000)
  assert.equal(getVideoQuoteRefreshDelay(10, 8_000), 0)
  assert.equal(isVideoQuoteStaleError(409, { code: 'quote_stale' }), true)
  assert.equal(isVideoQuoteStaleError(400, { code: 'quote_stale' }), false)
  assert.equal(isVideoQuoteStaleError(409, { code: 'other' }), false)
})

test('mode changes prune stale parameters and restore applicable defaults', () => {
  const modeProfile: VideoModelProfile = {
    ...profile,
    specification: {
      ...profile.specification,
      parameters: [
        ...profile.specification.parameters,
        {
          control: 'switch',
          key: 'enhance_prompt',
          label: 'Enhance prompt',
          modes: ['text_to_video'],
          default: true,
        },
      ],
    },
  }
  const staleParameters = {
    duration: 8,
    quality: 'standard',
    enhance_prompt: false,
  }

  assert.deepEqual(
    getVideoParametersForMode(modeProfile, 'image_to_video', staleParameters),
    { duration: 8, quality: 'standard' }
  )
  assert.equal(
    validateComposerForProfile(
      {
        model_profile_id: modeProfile.id,
        mode: 'image_to_video',
        prompt: 'Ocean waves',
        reference_asset_ids: [3],
        parameters: staleParameters,
      },
      modeProfile
    ),
    'videoStudio.validation.parameterInvalid'
  )
})

test('polling is adaptive and stops for hidden or terminal work', () => {
  assert.equal(getVideoTaskPollInterval([generation()], 130, true), 3_000)
  assert.equal(getVideoTaskPollInterval([generation()], 170, true), 5_000)
  assert.equal(getVideoTaskPollInterval([generation()], 130, false), false)
  assert.equal(
    getVideoTaskPollInterval([generation({ status: 'failed' })], 130, true),
    false
  )
})

test('asset inspection polling slows after 30 seconds and stops after 60', () => {
  assert.equal(isVideoAssetInspectionPending(asset('uploaded')), true)
  assert.equal(isVideoAssetInspectionPending(asset('processing')), true)
  assert.equal(isVideoAssetInspectionPending(asset('ready')), false)
  assert.equal(isVideoAssetInspectionPending(asset('failed')), false)
  assert.equal(getVideoAssetInspectionPollInterval(asset('uploaded'), 0), 2_000)
  assert.equal(
    getVideoAssetInspectionPollInterval(asset('processing'), 29_999),
    2_000
  )
  assert.equal(
    getVideoAssetInspectionPollInterval(asset('processing'), 30_000),
    5_000
  )
  assert.equal(
    getVideoAssetInspectionPollInterval(asset('processing'), 59_999),
    1
  )
  assert.equal(
    getVideoAssetInspectionPollInterval(asset('processing'), 60_000),
    false
  )
  assert.equal(getVideoAssetInspectionPollInterval(asset('ready'), 1), false)
  assert.equal(
    isVideoAssetInspectionTakingLong(asset('processing'), 60_000),
    true
  )
  assert.equal(isVideoAssetInspectionTakingLong(asset('ready'), 60_000), false)
})

test('only ready assets may mount media content', () => {
  assert.equal(shouldRenderVideoAssetMedia(asset('uploaded')), false)
  assert.equal(shouldRenderVideoAssetMedia(asset('processing')), false)
  assert.equal(shouldRenderVideoAssetMedia(asset('failed')), false)
  assert.equal(shouldRenderVideoAssetMedia(asset('ready')), true)
})

test('video studio access fails closed and respects admin mode', () => {
  assert.equal(normalizeVideoStudioAccessMode('unexpected'), 'off')
  assert.equal(canAccessVideoStudio('off', true), false)
  assert.equal(canAccessVideoStudio('admin', false), false)
  assert.equal(canAccessVideoStudio('admin', true), true)
  assert.equal(canAccessVideoStudio('all', false), true)
})

test('unknown submission creates a lock without inventing a task id', () => {
  assert.deepEqual(
    getVideoSubmissionLock({
      code: 'task_submission_unknown',
      data: 'task_local_123',
    }),
    { taskId: 'task_local_123' }
  )
  assert.deepEqual(
    getVideoSubmissionLock({ code: 'task_submission_unknown' }),
    { taskId: null }
  )
  assert.equal(getVideoSubmissionLock({ code: 'quote_stale' }), null)
})
