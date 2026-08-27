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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useForm, useWatch } from 'react-hook-form'
import { describe, expect, test } from 'vitest'

import { Form } from '@/components/ui/form'

import type { ImageComposerValues, ImageModelProfile } from '../types'
import { ImageOutputQuantityField } from './image-output-quantity-field'
import { ImageParameterFields } from './image-parameter-fields'

const profile: ImageModelProfile = {
  id: 7,
  model: 'gpt-image-2',
  display_name: 'GPT Image',
  description: '',
  provider_label: 'OpenAI',
  specification_version: 2,
  specification: {
    version: 2,
    parameters: [
      {
        key: 'variants',
        label: 'Server count label',
        request_key: 'n',
        control: 'integer',
        min: 1,
        max: 128,
      },
      {
        key: 'quality',
        label: 'Quality',
        request_key: 'quality',
        control: 'select',
        options: [{ label: 'High', value: 'high' }],
      },
    ],
  },
  default_parameters: { variants: 1, quality: 'high' },
  effective_max_outputs: 4,
  enabled: true,
  sort_order: 0,
  created_at: 1,
  updated_at: 1,
}

function QuantityHarness(props: { model?: ImageModelProfile }) {
  const model = props.model ?? profile
  const form = useForm<ImageComposerValues>({
    defaultValues: {
      model_profile_id: model.id,
      prompt: 'test prompt',
      parameters: model.default_parameters,
    },
  })
  const parameters = useWatch({ control: form.control, name: 'parameters' })
  const outputParameter = model.specification.parameters.find(
    (parameter) => parameter.request_key === 'n'
  )

  return (
    <Form {...form}>
      <ImageOutputQuantityField control={form.control} profile={model} />
      <ImageParameterFields
        control={form.control}
        profile={model}
        hideOutputCount
      />
      <output aria-label='selected output count'>
        {outputParameter ? String(parameters[outputParameter.key]) : '1'}
      </output>
    </Form>
  )
}

describe('image output quantity field', () => {
  test('writes the selected count through an arbitrary n parameter key', async () => {
    const user = userEvent.setup()
    render(<QuantityHarness />)

    expect(
      screen.getByRole('group', { name: 'imageStudio.outputQuantity' })
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '4' })).toBeInTheDocument()
    expect(
      screen.queryByLabelText('Server count label')
    ).not.toBeInTheDocument()
    expect(screen.getByLabelText('Quality')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '4' }))

    expect(screen.getByLabelText('selected output count')).toHaveTextContent(
      '4'
    )
  })

  test('limits the available choices to the effective maximum', () => {
    render(<QuantityHarness model={{ ...profile, effective_max_outputs: 2 }} />)

    expect(screen.getByRole('button', { name: '1' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '2' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '3' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '4' })).not.toBeInTheDocument()
  })

  test('hides the quantity control when only one output is effective', () => {
    render(<QuantityHarness model={{ ...profile, effective_max_outputs: 1 }} />)

    expect(
      screen.queryByRole('group', { name: 'imageStudio.outputQuantity' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByLabelText('Server count label')
    ).not.toBeInTheDocument()
    expect(screen.getByLabelText('selected output count')).toHaveTextContent(
      '1'
    )
  })
})
