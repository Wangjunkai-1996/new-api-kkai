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
import { useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'

import {
  getVideoModelPreset,
  type VideoModelProfileFormValues,
} from '../schemas'
import {
  decodeVideoParameterOptionValue,
  encodeVideoParameterOptionValue,
  VIDEO_MODE_LABEL_KEYS,
} from '../video-domain'

function VideoModelPresetParameterEditor(props: { index: number }) {
  const form = useFormContext<VideoModelProfileFormValues>()
  const parameter = useWatch({
    control: form.control,
    name: `parameters.${props.index}`,
  })
  if (!parameter) return null

  return (
    <FormField
      control={form.control}
      name={`parameters.${props.index}.default_value`}
      render={({ field }) => {
        let control
        if (
          parameter.control === 'segmented' ||
          parameter.control === 'select'
        ) {
          control = (
            <NativeSelect
              value={
                field.value === undefined
                  ? ''
                  : encodeVideoParameterOptionValue(field.value)
              }
              onChange={(event) => {
                const value = decodeVideoParameterOptionValue(
                  parameter.options,
                  event.target.value
                )
                if (value !== undefined) field.onChange(value)
              }}
            >
              {parameter.options.map((option) => (
                <NativeSelectOption
                  key={encodeVideoParameterOptionValue(option.value)}
                  value={encodeVideoParameterOptionValue(option.value)}
                >
                  {option.label || String(option.value)}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          )
        } else if (parameter.control === 'switch') {
          control = (
            <Switch
              checked={Boolean(field.value)}
              onCheckedChange={field.onChange}
            />
          )
        } else {
          control = (
            <Input
              type='number'
              min={parameter.min}
              max={parameter.max}
              step={parameter.step}
              value={typeof field.value === 'number' ? field.value : ''}
              onChange={(event) =>
                field.onChange(
                  event.target.value === ''
                    ? undefined
                    : event.target.valueAsNumber
                )
              }
            />
          )
        }

        return (
          <FormItem className='grid gap-3 py-3 sm:grid-cols-[minmax(0,1fr)_minmax(12rem,20rem)] sm:items-center'>
            <FormLabel>{parameter.label}</FormLabel>
            <div className='space-y-1.5'>
              <FormControl>{control}</FormControl>
              <FormMessage />
            </div>
          </FormItem>
        )
      }}
    />
  )
}

export function VideoModelSpecEditor() {
  const { t } = useTranslation()
  const form = useFormContext<VideoModelProfileFormValues>()
  const modelName = useWatch({ control: form.control, name: 'model' })
  const parameters = useWatch({ control: form.control, name: 'parameters' })
  const preset = getVideoModelPreset(modelName)

  if (!modelName) {
    return (
      <div className='border-t pt-4'>
        <p className='text-muted-foreground text-sm'>
          {t('videoStudio.admin.selectModelFirst')}
        </p>
      </div>
    )
  }

  if (!preset) {
    return (
      <div className='border-t pt-4'>
        <p className='text-destructive text-sm' role='alert'>
          {t('videoStudio.admin.modelPresetUnavailable')}
        </p>
      </div>
    )
  }

  const referenceRole = preset.specification.reference_inputs?.[0]?.role

  return (
    <div className='space-y-5 border-t pt-4'>
      <div className='divide-y rounded-md border px-3'>
        <div className='flex items-center justify-between gap-3 py-3'>
          <span className='text-muted-foreground text-sm'>
            {t('videoStudio.admin.outputResolution')}
          </span>
          <span className='text-sm font-medium'>{preset.resolution}</span>
        </div>
        <div className='flex items-center justify-between gap-3 py-3'>
          <span className='text-muted-foreground text-sm'>
            {t('videoStudio.admin.modes')}
          </span>
          <span className='text-right text-sm font-medium'>
            {preset.specification.modes
              .map((mode) => t(VIDEO_MODE_LABEL_KEYS[mode]))
              .join(' / ')}
          </span>
        </div>
        {referenceRole && (
          <div className='flex items-center justify-between gap-3 py-3'>
            <span className='text-muted-foreground text-sm'>
              {t('videoStudio.admin.referenceType')}
            </span>
            <span className='text-sm font-medium'>
              {t(
                referenceRole === 'reference_video'
                  ? 'videoStudio.admin.referenceType.video'
                  : 'videoStudio.admin.referenceType.image'
              )}
            </span>
          </div>
        )}
      </div>

      <div className='space-y-3'>
        <h4 className='text-sm font-semibold'>
          {t('videoStudio.admin.defaults')}
        </h4>
        <div className='divide-y rounded-md border px-3'>
          {parameters.map((parameter, index) => (
            <VideoModelPresetParameterEditor
              key={parameter.key}
              index={index}
            />
          ))}
        </div>
      </div>
    </div>
  )
}
