import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { parseVideoSampleForm, videoSampleFormSchema } from './schemas'
import { isVideoSampleCategoryEnabledForContract } from './video-sample-categories'

const sampleFormInput = {
  model_profile_id: 1,
  title: 'Category sample',
  prompt: 'A catalog sample',
  mode: 'text_to_video',
  parameters: {},
  reference_asset_ids: [],
  video_asset_id: 2,
  category: 'people',
  status: 'draft',
  sort_order: 0,
}

describe('video sample category form contract', () => {
  test('accepts one fixed category and includes it in the API input', () => {
    const parsed = videoSampleFormSchema.parse(sampleFormInput)
    const input = parseVideoSampleForm(parsed)

    assert.equal(input.category, 'people')
  })

  test('rejects a free-form category', () => {
    const parsed = videoSampleFormSchema.safeParse({
      ...sampleFormInput,
      category: 'custom-tag',
    })

    assert.equal(parsed.success, false)
  })
})

test('video sample categories are hidden in the schema bridge build', () => {
  assert.equal(isVideoSampleCategoryEnabledForContract('bridge'), false)
  assert.equal(isVideoSampleCategoryEnabledForContract('feature'), true)
  assert.equal(isVideoSampleCategoryEnabledForContract(undefined), true)
})
