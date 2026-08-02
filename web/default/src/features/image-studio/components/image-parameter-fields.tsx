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
import { Controller, type Control } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'

import { IMAGE_STUDIO_MAX_OUTPUTS } from '../image-domain'
import type {
  ImageComposerValues,
  ImageModelProfile,
  ImageParameterValue,
} from '../types'

export function ImageParameterFields(props: {
  control: Control<ImageComposerValues>
  profile: ImageModelProfile
}) {
  const { t } = useTranslation()
  if (props.profile.specification.parameters.length === 0) return null
  return (
    <div className='grid gap-4 sm:grid-cols-2'>
      {props.profile.specification.parameters.map((parameter) => (
        <Controller
          key={parameter.key}
          control={props.control}
          name={`parameters.${parameter.key}`}
          render={({ field }) => {
            const id = `image-parameter-${parameter.key}`
            if (parameter.control === 'boolean') {
              return (
                <div className='flex min-h-10 items-center justify-between gap-3 rounded-md border px-3'>
                  <Label htmlFor={id}>{parameter.label}</Label>
                  <Switch
                    id={id}
                    checked={field.value === true}
                    onCheckedChange={field.onChange}
                  />
                </div>
              )
            }
            return (
              <div className='space-y-1.5'>
                <Label htmlFor={id}>{parameter.label}</Label>
                {parameter.control === 'select' ? (
                  <NativeSelect
                    id={id}
                    value={typeof field.value === 'string' ? field.value : ''}
                    onChange={(event) => field.onChange(event.target.value)}
                  >
                    <NativeSelectOption value=''>
                      {t('imageStudio.parameter.select')}
                    </NativeSelectOption>
                    {parameter.options.map((option) => (
                      <NativeSelectOption
                        key={option.value}
                        value={option.value}
                      >
                        {option.label}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                ) : (
                  <Input
                    id={id}
                    type='number'
                    min={parameter.min}
                    max={
                      parameter.request_key === 'n'
                        ? Math.min(parameter.max, IMAGE_STUDIO_MAX_OUTPUTS)
                        : parameter.max
                    }
                    value={typeof field.value === 'number' ? field.value : ''}
                    onChange={(event) => {
                      const value: ImageParameterValue | undefined =
                        event.target.value === ''
                          ? undefined
                          : Number(event.target.value)
                      field.onChange(value)
                    }}
                  />
                )}
              </div>
            )
          }}
        />
      ))}
    </div>
  )
}
