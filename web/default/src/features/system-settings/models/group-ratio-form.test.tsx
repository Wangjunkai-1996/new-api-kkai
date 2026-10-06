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
import { useForm, type UseFormReturn } from 'react-hook-form'
import { describe, expect, test, vi } from 'vitest'

import { GroupRatioForm } from './group-ratio-form'

vi.mock('./group-ratio-visual-editor', () => ({
  GroupRatioVisualEditor: () => null,
}))

vi.mock('./group-special-usable-editor', () => ({
  GroupSpecialUsableRulesEditor: () => null,
}))

type GroupFormValues = {
  GroupRatio: string
  TopupGroupRatio: string
  UserUsableGroups: string
  GroupDisplayNames: string
  GroupGroupRatio: string
  AutoGroups: string
  MaxTokenAutoGroups: number
  AutoGroupProfiles: string
  DefaultUseAutoGroup: boolean
  GroupSpecialUsableGroup: string
}

const defaultValues: GroupFormValues = {
  GroupRatio: '{}',
  TopupGroupRatio: '{}',
  UserUsableGroups: '{}',
  GroupDisplayNames: '{}',
  GroupGroupRatio: '{}',
  AutoGroups: '[]',
  MaxTokenAutoGroups: 5,
  AutoGroupProfiles: '{}',
  DefaultUseAutoGroup: false,
  GroupSpecialUsableGroup: '{}',
}

function Fixture(props: { form: UseFormReturn<GroupFormValues> }) {
  return (
    <GroupRatioForm
      form={props.form}
      onSave={async () => undefined}
      isSaving={false}
    />
  )
}

describe('GroupRatioForm', () => {
  test('renders the auto-group limit field inside its form provider', () => {
    function Wrapper() {
      const form = useForm<GroupFormValues>({ defaultValues })
      return <Fixture form={form} />
    }

    render(<Wrapper />)

    expect(screen.getByLabelText('Per-token Auto group limit')).toHaveValue(5)
  })
})
