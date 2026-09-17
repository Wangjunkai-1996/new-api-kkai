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
*/
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, test } from 'vitest'

import { GroupRatioVisualEditor } from './group-ratio-visual-editor'

function EditorHarness() {
  const [profiles, setProfiles] = useState('{}')
  return (
    <>
      <GroupRatioVisualEditor
        groupRatio='{"default":1,"vip":2}'
        topupGroupRatio='{}'
        userUsableGroups='{"default":"Default","vip":"VIP","auto":"Auto","auto2":"Auto 2"}'
        groupDisplayNames='{}'
        groupGroupRatio='{}'
        autoGroups='["default"]'
        autoGroupProfiles={profiles}
        groupSpecialUsableGroup='{}'
        onChange={(field, value) => {
          if (field === 'AutoGroupProfiles') setProfiles(value)
        }}
      />
      <output aria-label='saved profiles'>{profiles}</output>
    </>
  )
}

describe('named auto group profile editor', () => {
  test('adds independent profiles and preserves candidate order while editing', async () => {
    const user = userEvent.setup()
    render(<EditorHarness />)
    const editor = within(screen.getByLabelText('Named auto group profiles'))
    await user.type(
      editor.getByRole('textbox', { name: 'Auto group name' }),
      'auto2'
    )
    await user.click(editor.getByRole('button', { name: 'Add' }))
    expect(
      JSON.parse(screen.getByLabelText('saved profiles').textContent ?? '{}')
    ).toEqual({ auto2: ['default'] })

    await user.click(editor.getAllByRole('combobox')[1])
    expect(
      screen.queryByRole('option', { name: 'auto' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('option', { name: 'auto2' })
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('option', { name: 'vip' }))
    await user.click(editor.getByRole('button', { name: 'Move up vip' }))
    expect(
      JSON.parse(screen.getByLabelText('saved profiles').textContent ?? '{}')
    ).toEqual({ auto2: ['vip', 'default'] })
    await user.click(editor.getByRole('button', { name: 'Delete default' }))
    expect(
      JSON.parse(screen.getByLabelText('saved profiles').textContent ?? '{}')
    ).toEqual({ auto2: ['vip'] })
    expect(editor.getByRole('button', { name: 'Delete vip' })).toBeDisabled()

    await user.type(
      editor.getByRole('textbox', { name: 'Auto group name' }),
      'auto10'
    )
    await user.click(editor.getByRole('button', { name: 'Add' }))
    expect(
      JSON.parse(screen.getByLabelText('saved profiles').textContent ?? '{}')
    ).toEqual({ auto2: ['vip'], auto10: ['default'] })
    await user.click(editor.getByRole('button', { name: 'Delete' }))
    expect(
      JSON.parse(screen.getByLabelText('saved profiles').textContent ?? '{}')
    ).toEqual({ auto2: ['vip'] })
  })
})
