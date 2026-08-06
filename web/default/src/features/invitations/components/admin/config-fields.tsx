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
import {
  Controller,
  type Control,
  type FieldPath,
  type UseFormRegister,
} from 'react-hook-form'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

import type { SystemConfig } from '../../types'

export const ConfigSwitch = (props: {
  control: Control<SystemConfig>
  name: FieldPath<SystemConfig>
  label: string
}) => (
  <Controller
    control={props.control}
    name={props.name}
    render={({ field }) => (
      <div className='flex min-h-11 items-center justify-between gap-4 border-b py-2 last:border-b-0'>
        <Label htmlFor={props.name}>{props.label}</Label>
        <Switch
          id={props.name}
          checked={Boolean(field.value)}
          onCheckedChange={field.onChange}
        />
      </div>
    )}
  />
)

export const ConfigNumberField = (props: {
  name: FieldPath<SystemConfig>
  label: string
  register: UseFormRegister<SystemConfig>
  error?: string
}) => (
  <div className='space-y-2'>
    <Label htmlFor={props.name}>{props.label}</Label>
    <Input
      id={props.name}
      type='number'
      min='0'
      step='1'
      {...props.register(props.name, { valueAsNumber: true })}
    />
    {props.error && <p className='text-destructive text-sm'>{props.error}</p>}
  </div>
)
