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
import { ArrowLeftRight } from 'lucide-react'
import { useState } from 'react'
import { Controller, type Control } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'

import { getImageSizeParameter } from '../image-parameters'
import type { ImageComposerValues, ImageModelProfile } from '../types'

export function ImageResolutionField(props: {
  control: Control<ImageComposerValues>
  profile: ImageModelProfile
  disabled?: boolean
  appliedSize?: string
}) {
  const { t } = useTranslation()
  const [customSelection, setCustomSelection] = useState<string | null>(null)
  const parameter = getImageSizeParameter(props.profile)
  if (!parameter) return null
  const constraints = props.profile.specification.custom_size
  const id = `image-resolution-${props.profile.id}`
  const errorId = `${id}-error`

  return (
    <Controller
      control={props.control}
      name={`parameters.${parameter.key}`}
      render={({ field, fieldState }) => {
        const value = typeof field.value === 'string' ? field.value : ''
        const [width = '', height = ''] = value.split('x')
        const preset = parameter.options.some(
          (option) => option.value === value
        )
        const custom = Boolean(
          constraints &&
          value &&
          (!preset || customSelection === `${props.profile.id}:${value}`)
        )
        const unsupported = Boolean(value && !preset && !constraints)
        const error = fieldState.error?.message
          ? t(String(fieldState.error.message))
          : undefined
        let ratio = ''
        if (
          /^\d+x\d+$/.test(value) &&
          Number.isSafeInteger(Number(width)) &&
          Number.isSafeInteger(Number(height)) &&
          Number(width) > 0 &&
          Number(height) > 0
        ) {
          let divisor = Number(width)
          let remainder = Number(height)
          while (remainder) {
            ;[divisor, remainder] = [remainder, divisor % remainder]
          }
          ratio = `${Number(width) / divisor}:${Number(height) / divisor}`
        }
        return (
          <fieldset
            className='image-create-size relative min-w-0 space-y-3'
            disabled={props.disabled}
          >
            {props.appliedSize && (
              <span
                key={props.appliedSize}
                className='image-create-size-flash pointer-events-none absolute inset-0 rounded-xl'
                aria-hidden='true'
              />
            )}
            <div className='space-y-2'>
              <Label htmlFor={id}>{t('Resolution preset')}</Label>
              <NativeSelect
                id={id}
                name={field.name}
                ref={field.ref}
                value={custom ? '__custom__' : value}
                onBlur={field.onBlur}
                onChange={(event) => {
                  if (event.target.value !== '__custom__') {
                    setCustomSelection(null)
                    field.onChange(event.target.value)
                    return
                  }
                  setCustomSelection(`${props.profile.id}:${value || 'x'}`)
                  if (!value) field.onChange('x')
                }}
                className='w-full'
                aria-invalid={Boolean(error)}
                aria-describedby={error ? errorId : undefined}
              >
                <NativeSelectOption value=''>
                  {t('imageStudio.parameter.select')}
                </NativeSelectOption>
                {unsupported && (
                  <NativeSelectOption value={value} disabled>
                    {value.replace('x', ' × ')}
                  </NativeSelectOption>
                )}
                {parameter.options.map((option) => (
                  <NativeSelectOption key={option.value} value={option.value}>
                    {option.value.replace('x', ' × ')} · {option.label}
                  </NativeSelectOption>
                ))}
                {constraints && (
                  <NativeSelectOption value='__custom__'>
                    {t('Custom pixel size')}
                  </NativeSelectOption>
                )}
              </NativeSelect>
            </div>
            {constraints && (
              <div>
                <div className='image-create-custom-label text-muted-foreground mb-2 flex justify-between text-xs'>
                  <span>{t('Custom pixels')}</span>
                  <span className='tabular-nums'>{ratio}</span>
                </div>
                <div className='image-create-size-inputs grid grid-cols-[minmax(0,1fr)_28px_minmax(0,1fr)] items-center gap-1'>
                  <Label className='min-w-0' htmlFor={`${id}-width`}>
                    <span>{t('Width')}</span>
                    <Input
                      id={`${id}-width`}
                      aria-label={t('Custom width')}
                      type='number'
                      min={constraints.step}
                      max={constraints.max_edge}
                      step={constraints.step}
                      value={width}
                      onBlur={field.onBlur}
                      onChange={(event) =>
                        field.onChange(`${event.target.value}x${height}`)
                      }
                      aria-invalid={Boolean(error)}
                      aria-describedby={error ? errorId : undefined}
                    />
                    <span>px</span>
                  </Label>
                  <Button
                    type='button'
                    size='icon-sm'
                    variant='ghost'
                    aria-label={t('Swap width and height')}
                    onClick={() => field.onChange(`${height}x${width}`)}
                    disabled={!width || !height}
                  >
                    <ArrowLeftRight aria-hidden='true' />
                  </Button>
                  <Label className='min-w-0' htmlFor={`${id}-height`}>
                    <span>{t('Height')}</span>
                    <Input
                      id={`${id}-height`}
                      aria-label={t('Custom height')}
                      type='number'
                      min={constraints.step}
                      max={constraints.max_edge}
                      step={constraints.step}
                      value={height}
                      onBlur={field.onBlur}
                      onChange={(event) =>
                        field.onChange(`${width}x${event.target.value}`)
                      }
                      aria-invalid={Boolean(error)}
                      aria-describedby={error ? errorId : undefined}
                    />
                    <span>px</span>
                  </Label>
                </div>
                <p className='image-create-size-limit text-muted-foreground mt-2 text-xs'>
                  {t(
                    'Size limits: multiples of {{step}} px; each edge up to {{maxEdge}} px; {{minPixels}}–{{maxPixels}} total pixels; aspect ratio up to {{maxRatio}}:1.',
                    {
                      step: constraints.step,
                      maxEdge: constraints.max_edge,
                      minPixels: constraints.min_pixels,
                      maxPixels: constraints.max_pixels,
                      maxRatio: constraints.max_aspect_ratio,
                    }
                  )}
                </p>
              </div>
            )}
            {error && (
              <p id={errorId} className='text-destructive text-sm' role='alert'>
                {error}
              </p>
            )}
          </fieldset>
        )
      }}
    />
  )
}
