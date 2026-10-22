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
import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useForm, useWatch } from 'react-hook-form'
import { describe, expect, test } from 'vitest'

import { Form } from '@/components/ui/form'

import type { ImageComposerValues, ImageModelProfile } from '../types'
import { ImageParameterFields } from './image-parameter-fields'
import { ImageResolutionField } from './image-resolution-field'

const profile: ImageModelProfile = {
  id: 7,
  model: 'gpt-image-2',
  display_name: 'GPT Image',
  description: '',
  provider_label: 'OpenAI',
  specification_version: 2,
  specification: {
    version: 2,
    custom_size: {
      max_edge: 3840,
      step: 16,
      min_pixels: 655360,
      max_pixels: 8294400,
      max_aspect_ratio: 3,
    },
    parameters: [
      {
        key: 'dimensions',
        label: 'Output size',
        request_key: 'size',
        control: 'select',
        required: true,
        options: [
          { value: '1024x1024', label: 'Square' },
          { value: '1536x1024', label: 'Landscape' },
        ],
      },
      {
        key: 'detail',
        label: 'Quality',
        request_key: 'quality',
        control: 'select',
        options: [{ value: 'high', label: 'High' }],
      },
      {
        key: 'watermark',
        label: 'Watermark',
        request_key: 'watermark',
        control: 'boolean',
      },
      {
        key: 'compression',
        label: 'Compression',
        request_key: 'compression',
        control: 'integer',
        min: 0,
        max: 100,
      },
    ],
  },
  default_parameters: {
    dimensions: '1024x1024',
    detail: 'high',
    watermark: false,
    compression: 90,
  },
  effective_max_outputs: 4,
  enabled: true,
  sort_order: 0,
  created_at: 1,
  updated_at: 1,
}

function Harness(props: { model?: ImageModelProfile; size?: string }) {
  const model = props.model ?? profile
  const form = useForm<ImageComposerValues>({
    defaultValues: {
      model_profile_id: model.id,
      prompt: 'A composition',
      parameters: {
        ...model.default_parameters,
        dimensions: props.size ?? '1024x1024',
      },
    },
  })
  const values = useWatch({ control: form.control })
  return (
    <Form {...form}>
      <ImageResolutionField control={form.control} profile={model} />
      <ImageParameterFields
        control={form.control}
        profile={model}
        group='quality'
      />
      <ImageParameterFields
        control={form.control}
        profile={model}
        group='advanced'
      />
      <output aria-label='Parameters'>
        {JSON.stringify(values.parameters)}
      </output>
    </Form>
  )
}

describe('resolution and parameter groups', () => {
  test('writes dimensions to the declared parameter and swaps width and height', async () => {
    const user = userEvent.setup()
    render(<Harness />)
    fireEvent.change(screen.getByLabelText('Custom width'), {
      target: { value: '2048' },
    })
    fireEvent.change(screen.getByLabelText('Custom height'), {
      target: { value: '1536' },
    })
    expect(
      JSON.parse(screen.getByLabelText('Parameters').textContent ?? '{}')
    ).toMatchObject({
      dimensions: '2048x1536',
      detail: 'high',
      watermark: false,
      compression: 90,
    })
    await user.click(
      screen.getByRole('button', { name: 'Swap width and height' })
    )
    expect(screen.getByLabelText('Custom width')).toHaveValue(1536)
    expect(screen.getByLabelText('Custom height')).toHaveValue(2048)
    await user.selectOptions(
      screen.getByLabelText('Resolution preset'),
      '1024x1024'
    )
    expect(screen.getByLabelText('Custom width')).toHaveValue(1024)
    expect(screen.getByLabelText('Custom height')).toHaveValue(1024)
    expect(screen.getAllByLabelText('Quality')).toHaveLength(1)
    expect(
      screen.getByRole('switch', { name: 'Watermark' })
    ).toBeInTheDocument()
    expect(screen.getByLabelText('Compression')).toBeInTheDocument()
  })

  test('keeps unsupported stored sizes visible for preset-only models', () => {
    const restricted = {
      ...profile,
      specification: { ...profile.specification, custom_size: undefined },
    }
    render(<Harness model={restricted} size='2048x1536' />)
    expect(screen.getByLabelText('Resolution preset')).toHaveValue('2048x1536')
    expect(screen.getByRole('option', { name: '2048 × 1536' })).toBeDisabled()
    expect(screen.queryByLabelText('Custom width')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Custom height')).not.toBeInTheDocument()
  })

  test('shows no resolution control for models without a size parameter', () => {
    const noSize = {
      ...profile,
      specification: {
        ...profile.specification,
        parameters: profile.specification.parameters.filter(
          (item) => item.request_key !== 'size'
        ),
      },
    }
    render(<Harness model={noSize} />)
    expect(screen.queryByLabelText('Resolution preset')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Quality')).toBeInTheDocument()
    expect(
      screen.getByRole('switch', { name: 'Watermark' })
    ).toBeInTheDocument()
  })
})
