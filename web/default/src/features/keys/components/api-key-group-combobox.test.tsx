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
import { render, screen, waitFor, within } from '@testing-library/react'
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
  test('searches provider aliases across sections and commits the group key from the keyboard', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(
      <>
        <label htmlFor='group'>Key group</label>
        <ApiKeyGroupCombobox
          byModelFamily
          id='group'
          options={[
            { value: 'cc', label: 'Claude–Cursor', ratio: 0.2 },
            { value: 'gpt-basic', label: 'GPT basic' },
            { value: 'vip', label: 'GPT – 企业 Fast', ratio: 0.25 },
            { value: 'other', label: '普通专线' },
          ]}
          value='vip'
          onValueChange={onChange}
        />
      </>
    )
    const trigger = screen.getByRole('combobox', { name: 'Key group' })
    await user.tab()
    await user.keyboard('{Enter}')
    const search = screen.getByPlaceholderText('Search by provider or group')
    await waitFor(() => expect(search).toHaveFocus())
    expect(
      within(screen.getByRole('group', { name: 'OpenAI' })).getByText(
        'selected'
      )
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'OpenAI' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    expect(screen.getByRole('option', { name: /企业 Fast/ })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    await user.keyboard('{ArrowDown}{Enter}')
    expect(onChange).toHaveBeenCalledWith('gpt-basic')
    await waitFor(() => expect(trigger).toHaveFocus())

    await user.keyboard('{Enter}')
    const input = screen.getByPlaceholderText('Search by provider or group')
    await user.click(screen.getByRole('button', { name: 'OpenAI' }))
    expect(screen.getAllByRole('option')).toHaveLength(2)
    expect(screen.getByRole('option', { name: /企业 Fast/ })).toBeVisible()
    await user.type(input, 'anthropic 0.2x')
    expect(screen.getAllByRole('option')).toHaveLength(1)
    expect(
      within(screen.getByRole('group', { name: 'Claude' })).getByText(
        'Claude–Cursor'
      )
    ).toBeInTheDocument()
    expect(screen.queryByRole('group', { name: 'OpenAI' })).toBeNull()
    await user.type(input, ' missing')
    expect(screen.getByText('No group found.')).toBeInTheDocument()
    await user.keyboard('{Escape}')
    await waitFor(() => expect(trigger).toHaveFocus())
    expect(onChange).toHaveBeenCalledTimes(1)
    await user.keyboard('{Enter}')
    expect(
      screen.getByPlaceholderText('Search by provider or group')
    ).toHaveValue('')
  })

  test('anchors late options and preserves manual browsing across refresh, then restores the saved group on reopen', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    const available = [
      { value: 'gpt-one', label: 'GPT one' },
      { value: 'gpt-two', label: 'GPT two' },
      { value: 'cc-one', label: 'Claude one' },
      { value: 'cc-two', label: 'Claude two' },
    ]
    const picker = (options: ApiKeyGroupOption[], value = 'cc-two') => (
      <ApiKeyGroupCombobox
        byModelFamily
        triggerAriaLabel='Token group'
        options={options}
        value={value}
        onValueChange={onChange}
      />
    )
    const { rerender } = render(picker(available.slice(0, 2)))
    const trigger = screen.getByRole('combobox', { name: 'Token group' })
    await user.click(trigger)
    expect(screen.getByRole('button', { name: 'All' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    rerender(picker(available))
    expect(screen.getByRole('button', { name: 'Claude' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    expect(screen.getByRole('option', { name: /Claude two/ })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    await user.click(screen.getByRole('button', { name: 'OpenAI' }))
    const input = screen.getByPlaceholderText('Search by provider or group')
    await user.click(input)
    await user.keyboard('{ArrowDown}')
    rerender(picker(available.map((item) => ({ ...item, ratio: 0.5 }))))
    expect(screen.getByRole('button', { name: 'OpenAI' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    expect(screen.getByRole('option', { name: /GPT two/ })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    await user.keyboard('{Escape}')
    await user.click(trigger)
    expect(screen.getByRole('button', { name: 'Claude' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    expect(screen.getByRole('option', { name: /Claude two/ })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    await user.keyboard('{Escape}')
    rerender(
      picker(
        [
          ...available,
          { value: 'retired', label: 'Claude Legacy', disabled: true },
        ],
        'retired'
      )
    )
    await user.click(trigger)
    expect(screen.getByRole('button', { name: 'All' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    expect(
      screen.getByRole('option', { name: /Claude Legacy/ })
    ).toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByRole('option', { name: /GPT one/ })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    expect(onChange).not.toHaveBeenCalled()
  })

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
