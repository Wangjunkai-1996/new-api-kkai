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

import type { ImageModelProfile } from '../types'
import {
  createImageModelFormValues,
  imageModelFormSchema,
  parseImageModelForm,
  type ImageModelFormValues,
} from './image-admin-forms'

const profile: ImageModelProfile = {
  id: 11,
  model: 'gpt-image-1',
  display_name: 'GPT Image',
  description: 'Production profile',
  provider_label: 'OpenAI',
  specification_version: 3,
  specification: {
    version: 3,
    max_reference_images: 4,
    parameters: [
      {
        key: 'count',
        label: 'Count',
        request_key: 'n',
        control: 'integer',
        min: 1,
        max: 128,
        required: true,
      },
      {
        key: 'quality',
        label: 'Quality',
        request_key: 'quality',
        control: 'select',
        options: [
          { label: 'Standard', value: 'standard' },
          { label: 'High', value: 'high' },
        ],
      },
    ],
  },
  default_parameters: { count: 1, quality: 'standard' },
  enabled: true,
  sort_order: 10,
  created_at: 1,
  updated_at: 1,
}

const issueMessages = (values: ImageModelFormValues): string[] => {
  const parsed = imageModelFormSchema.safeParse(values)
  return parsed.success ? [] : parsed.error.issues.map((issue) => issue.message)
}

describe('image model admin form', () => {
  test('supports the backend count range without weakening the user limit', () => {
    const values = createImageModelFormValues(profile)
    assert.equal(imageModelFormSchema.safeParse(values).success, true)

    values.parameters[0].max = 129
    assert.ok(
      issueMessages(values).includes('imageStudio.validation.rangeInvalid')
    )

    values.max_reference_images = 5
    assert.ok(
      issueMessages(values).includes(
        'imageStudio.validation.referenceCountRange'
      )
    )
  })

  test('rejects a default count above the image studio output limit', () => {
    const values = createImageModelFormValues(profile)
    values.parameters[0].default_value = 5

    assert.ok(
      issueMessages(values).includes('imageStudio.validation.defaultInvalid')
    )
  })

  test('rejects a count range whose minimum exceeds the output limit', () => {
    const values = createImageModelFormValues(profile)
    values.parameters[0].required = false
    values.parameters[0].has_default = false
    values.parameters[0].min = 5
    values.parameters[0].max = 128

    assert.ok(
      issueMessages(values).includes('imageStudio.validation.rangeInvalid')
    )
  })

  test('rejects duplicate fields and required parameters without defaults', () => {
    const values = createImageModelFormValues(profile)
    values.parameters[1].key = values.parameters[0].key
    values.parameters[1].request_key = values.parameters[0].request_key
    values.parameters[0].has_default = false

    const messages = issueMessages(values)
    assert.ok(messages.includes('imageStudio.validation.parameterDuplicate'))
    assert.ok(messages.includes('imageStudio.validation.requestFieldDuplicate'))
    assert.ok(messages.includes('imageStudio.validation.requiredDefault'))
  })

  test('matches backend limits for descriptions and select options', () => {
    const values = createImageModelFormValues(profile)
    values.description = 'x'.repeat(4001)
    values.parameters[1].options_text = 'One=same\nTwo=same'

    const messages = issueMessages(values)
    assert.ok(messages.includes('imageStudio.validation.descriptionTooLong'))
    assert.ok(messages.includes('imageStudio.validation.optionsRequired'))
  })

  test('bumps the specification version only when parameter structure changes', () => {
    const unchanged = createImageModelFormValues(profile)
    unchanged.display_name = 'Renamed profile'
    assert.equal(
      parseImageModelForm(unchanged, profile).specification.version,
      profile.specification_version
    )

    const changed = createImageModelFormValues(profile)
    changed.parameters[0].max = 4
    assert.equal(
      parseImageModelForm(changed, profile).specification.version,
      profile.specification_version + 1
    )

    const referenceLimitChanged = createImageModelFormValues(profile)
    referenceLimitChanged.max_reference_images = 3
    assert.equal(
      parseImageModelForm(referenceLimitChanged, profile).specification.version,
      profile.specification_version + 1
    )
  })

  test('versions the explicit reference limit added to a legacy profile', () => {
    const legacyProfile: ImageModelProfile = {
      ...profile,
      specification: {
        version: profile.specification.version,
        parameters: profile.specification.parameters,
      },
    }
    const values = createImageModelFormValues(legacyProfile)

    assert.equal(values.max_reference_images, 1)
    assert.equal(
      parseImageModelForm(values, legacyProfile).specification.version,
      profile.specification_version + 1
    )
  })

  test('round-trips added size options as a versioned profile change', () => {
    const sizeProfile: ImageModelProfile = {
      ...profile,
      specification: {
        ...profile.specification,
        parameters: [
          profile.specification.parameters[0],
          {
            key: 'size',
            label: 'Size',
            request_key: 'size',
            required: true,
            control: 'select',
            options: [{ label: 'Default', value: '1024x1024' }],
          },
        ],
      },
      default_parameters: { count: 1, size: '1024x1024' },
    }
    const values = createImageModelFormValues(sizeProfile)
    values.parameters[1].options_text = [
      '1K | 1:1=1024x1024',
      '2K | 3:2 landscape=1536x1024',
      '2K | 2:3 portrait=1024x1536',
      '2K | 1:1=2048x2048',
      '2K | 16:9 landscape=2048x1152',
      '2K | 9:16 portrait=1152x2048',
      '4K | 16:9 landscape=3840x2160',
      '4K | 9:16 portrait=2160x3840',
    ].join('\n')

    const parsed = parseImageModelForm(values, sizeProfile)
    assert.equal(
      parsed.specification.version,
      profile.specification_version + 1
    )
    assert.deepEqual(parsed.specification.parameters[1], {
      key: 'size',
      label: 'Size',
      request_key: 'size',
      required: true,
      control: 'select',
      options: [
        { label: '1K | 1:1', value: '1024x1024' },
        { label: '2K | 3:2 landscape', value: '1536x1024' },
        { label: '2K | 2:3 portrait', value: '1024x1536' },
        { label: '2K | 1:1', value: '2048x2048' },
        { label: '2K | 16:9 landscape', value: '2048x1152' },
        { label: '2K | 9:16 portrait', value: '1152x2048' },
        { label: '4K | 16:9 landscape', value: '3840x2160' },
        { label: '4K | 9:16 portrait', value: '2160x3840' },
      ],
    })
  })
})
