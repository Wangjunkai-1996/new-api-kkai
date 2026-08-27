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

import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'

import {
  clampImageOutputCount,
  getImageOutputCount,
  getImageOutputParameter,
  getImageProfileMaxOutputs,
} from '../image-parameters'
import type { ImageComposerValues, ImageModelProfile } from '../types'

export function ImageOutputQuantityField(props: {
  control: Control<ImageComposerValues>
  profile: ImageModelProfile
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const parameter = getImageOutputParameter(props.profile)
  if (!parameter) return null

  const maxOutputs = getImageProfileMaxOutputs(props.profile)
  if (maxOutputs <= 1) return null
  const options = Array.from({ length: maxOutputs }, (_, index) => index + 1)
  const labelId = `image-output-quantity-${String(props.profile.id)}`

  return (
    <Controller
      control={props.control}
      name={`parameters.${parameter.key}`}
      render={({ field }) => {
        const fallback = getImageOutputCount(props.profile, {})
        const outputCount = clampImageOutputCount(
          props.profile,
          field.value,
          fallback
        )
        return (
          <div className='flex flex-col gap-2'>
            <span id={labelId} className='text-sm font-medium'>
              {t('imageStudio.outputQuantity')}
            </span>
            <ToggleGroup
              value={[String(outputCount)]}
              onValueChange={(next) => {
                const value = next.at(0)
                if (value) field.onChange(Number(value))
              }}
              onBlur={field.onBlur}
              variant='outline'
              className='w-full'
              aria-labelledby={labelId}
            >
              {options.map((value) => (
                <ToggleGroupItem
                  key={value}
                  value={String(value)}
                  disabled={props.disabled}
                  className='min-w-0 flex-1'
                >
                  {value}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </div>
        )
      }}
    />
  )
}
