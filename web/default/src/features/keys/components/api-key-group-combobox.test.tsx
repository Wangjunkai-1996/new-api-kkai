/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import {
  ApiKeyGroupCombobox,
  type ApiKeyGroupOption,
} from './api-key-group-combobox'

const options: ApiKeyGroupOption[] = [
  {
    value: 'default',
    label: 'Default',
    desc: 'Balanced general purpose access',
    ratio: 1,
  },
  {
    value: 'premium',
    label: 'Premium',
    desc: 'Faster models for priority requests',
    ratio: 2,
  },
  {
    value: 'auto2',
    label: 'Balanced route',
    desc: 'Automatically selects an available group',
    ratio: 'auto',
    isAuto: true,
  },
]

describe('ApiKeyGroupCombobox', () => {
  test('filters options by display metadata and returns the canonical group key', async () => {
    const onValueChange = vi.fn()
    const user = userEvent.setup()

    render(
      <ApiKeyGroupCombobox
        options={options}
        value='default'
        onValueChange={onValueChange}
        triggerAriaLabel='Token group'
      />
    )

    await user.click(screen.getByRole('combobox', { name: 'Token group' }))
    await user.type(screen.getByPlaceholderText('Search...'), 'faster')

    expect(screen.getByRole('option', { name: /Premium/ })).toBeInTheDocument()
    expect(
      screen.queryByRole('option', { name: /Default/ })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('option', { name: /Balanced route/ })
    ).not.toBeInTheDocument()

    await user.click(screen.getByRole('option', { name: /Premium/ }))

    expect(onValueChange).toHaveBeenCalledTimes(1)
    expect(onValueChange).toHaveBeenCalledWith('premium')
    await waitFor(() =>
      expect(
        screen.getByRole('combobox', { name: 'Token group' })
      ).toHaveAttribute('aria-expanded', 'false')
    )
  })

  test('keeps unavailable current groups visible but prevents selecting them', async () => {
    const onValueChange = vi.fn()
    const user = userEvent.setup()
    const unavailable: ApiKeyGroupOption = {
      value: 'retired',
      label: 'retired',
      desc: 'Unavailable',
      disabled: true,
    }

    render(
      <ApiKeyGroupCombobox
        options={[unavailable, ...options]}
        value='retired'
        onValueChange={onValueChange}
        triggerAriaLabel='Token group'
      />
    )

    await user.click(screen.getByRole('combobox', { name: 'Token group' }))

    expect(screen.getByRole('option', { name: /retired/ })).toHaveAttribute(
      'aria-disabled',
      'true'
    )
    await user.click(screen.getByRole('option', { name: /retired/ }))
    expect(onValueChange).not.toHaveBeenCalled()
  })
})
