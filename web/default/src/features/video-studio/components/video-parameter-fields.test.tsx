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
import { FormProvider, useForm, useWatch } from 'react-hook-form'
import { describe, expect, test } from 'vitest'

import type {
  VideoGenerationMode,
  VideoModelProfile,
  VideoParameters,
} from '../types'
import { VideoParameterFields } from './video-parameter-fields'

const profile: VideoModelProfile = {
  id: 1,
  model: 'custom-video',
  display_name: 'Custom video',
  description: '',
  provider_label: '',
  specification_version: 1,
  specification: {
    version: 1,
    modes: ['text_to_video'],
    parameters: [
      {
        key: 'aspect_ratio',
        label: 'Aspect ratio',
        control: 'select',
        options: [
          { label: 'Wide', value: '16:9' },
          { label: 'Portrait', value: '9:16' },
        ],
      },
      {
        key: 'seed',
        label: 'Seed',
        control: 'number',
        min: 0,
        max: 10,
        step: 1,
        required: true,
      },
      {
        key: 'audio',
        label: 'Audio',
        control: 'segmented',
        options: [
          { label: 'With audio', value: true },
          { label: 'No audio', value: false },
        ],
      },
    ],
  },
  default_parameters: {},
  enabled: true,
  sort_order: 0,
  created_at: 1,
  updated_at: 1,
}

function Fixture(props: {
  parameters: VideoParameters
  profile?: VideoModelProfile
}) {
  const form = useForm<{
    mode: VideoGenerationMode
    parameters: VideoParameters
  }>({ defaultValues: { mode: 'text_to_video', parameters: props.parameters } })
  const parameters = useWatch({ control: form.control, name: 'parameters' })
  return (
    <FormProvider {...form}>
      <VideoParameterFields profile={props.profile ?? profile} />
      <output aria-label='Current parameters'>
        {JSON.stringify(parameters)}
      </output>
    </FormProvider>
  )
}

describe('video parameter groups', () => {
  test('aspect ratio choices keep their values and collapsed advanced values preserve false and zero', () => {
    render(
      <Fixture parameters={{ aspect_ratio: '16:9', seed: 0, audio: false }} />
    )
    expect(
      screen.getByRole('button', { name: 'Advanced settings' })
    ).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(screen.getByRole('button', { name: 'Portrait' }))
    expect(screen.getByLabelText('Current parameters')).toHaveTextContent(
      '"aspect_ratio":"9:16"'
    )
    fireEvent.click(screen.getByRole('button', { name: 'Advanced settings' }))
    expect(screen.getByRole('spinbutton', { name: 'Seed' })).toHaveValue(0)
    expect(screen.getByRole('button', { name: 'No audio' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    expect(screen.getByLabelText('Current parameters')).toHaveTextContent(
      '"audio":false'
    )
  })

  test('missing and invalid required advanced parameters are visible without opening the section', () => {
    const { rerender } = render(
      <Fixture key='missing' parameters={{ aspect_ratio: '16:9' }} />
    )
    expect(
      screen.getByRole('button', { name: 'Advanced settings' })
    ).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('spinbutton', { name: 'Seed' })).toBeVisible()
    rerender(
      <Fixture key='invalid' parameters={{ aspect_ratio: '16:9', seed: 11 }} />
    )
    expect(
      screen.getByRole('button', { name: 'Advanced settings' })
    ).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('spinbutton', { name: 'Seed' })).toHaveValue(11)
  })
})

test('Seedance ratio remains a common choice and invalid advanced steps are explained', () => {
  const seedanceProfile = {
    ...profile,
    specification: {
      ...profile.specification,
      parameters: profile.specification.parameters.map((item) =>
        item.key === 'aspect_ratio' ? { ...item, key: 'ratio' } : item
      ),
    },
  }
  render(
    <Fixture
      profile={seedanceProfile}
      parameters={{ ratio: '16:9', seed: 0.5 }}
    />
  )
  const ratio = screen.getByRole('button', { name: 'Portrait' })
  expect(ratio.closest('.video-studio-common-parameters')).not.toBeNull()
  fireEvent.click(ratio)
  expect(screen.getByLabelText('Current parameters')).toHaveTextContent(
    '"ratio":"9:16"'
  )
  const seed = screen.getByRole('spinbutton', { name: 'Seed' })
  expect(seed).toHaveAttribute('aria-invalid', 'true')
  expect(seed).toHaveAccessibleDescription(
    'videoStudio.validation.parameterOutOfRange'
  )
  fireEvent.change(seed, { target: { value: '' } })
  expect(seed).toHaveDisplayValue('')
  expect(seed).toHaveAccessibleDescription(
    'videoStudio.validation.parameterRequired'
  )
})
