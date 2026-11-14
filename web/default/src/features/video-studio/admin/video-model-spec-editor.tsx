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
import { useFormContext, useWatch, type FieldPath } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
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
  applyVideoModelReferencePreset,
  getVideoModelPreset,
  VIDEO_PARAMETER_LABEL_KEYS,
  VIDEO_PARAMETER_OPTION_LABEL_KEYS,
  type VideoModelProfileFormValues,
} from '../schemas'
import type { VideoReferenceMediaPolicy } from '../types'
import {
  decodeVideoParameterOptionValue,
  encodeVideoParameterOptionValue,
  VIDEO_MODE_LABEL_KEYS,
} from '../video-domain'

function VideoReferenceMediaPolicyEditor(props: {
  kind: 'image' | 'video' | 'audio'
}) {
  const { t } = useTranslation()
  const form = useFormContext<VideoModelProfileFormValues>()
  const modelName = useWatch({ control: form.control, name: 'model' })
  const accepted =
    getVideoModelPreset(modelName)?.specification.reference_policy?.[props.kind]
  const numberFields = [
    { key: 'max_count', label: t('Maximum files'), step: '1' },
    { key: 'max_size_bytes', label: t('Maximum file size (bytes)'), step: '1' },
  ]
  if (props.kind === 'image') {
    numberFields.push(
      { key: 'min_width', label: t('Minimum width (px)'), step: '1' },
      { key: 'max_width', label: t('Maximum width (px)'), step: '1' },
      { key: 'min_height', label: t('Minimum height (px)'), step: '1' },
      { key: 'max_height', label: t('Maximum height (px)'), step: '1' },
      {
        key: 'min_aspect_ratio_exclusive',
        label: t('Minimum aspect ratio (exclusive)'),
        step: 'any',
      },
      {
        key: 'max_aspect_ratio_exclusive',
        label: t('Maximum aspect ratio (exclusive)'),
        step: 'any',
      }
    )
  } else {
    numberFields.push(
      {
        key: 'min_duration_seconds',
        label: t('Minimum duration (seconds)'),
        step: 'any',
      },
      {
        key: 'max_duration_seconds',
        label: t('Maximum duration (seconds)'),
        step: 'any',
      },
      {
        key: 'max_total_duration_seconds',
        label: t('Maximum total duration (seconds)'),
        step: 'any',
      }
    )
    if (props.kind === 'video') {
      numberFields.push(
        {
          key: 'min_frame_rate',
          label: t('Minimum frame rate (fps)'),
          step: 'any',
        },
        {
          key: 'max_frame_rate',
          label: t('Maximum frame rate (fps)'),
          step: 'any',
        }
      )
    }
  }
  const labels = {
    image: t('videoStudio.admin.referenceType.image'),
    video: t('videoStudio.admin.referenceType.video'),
    audio: t('videoStudio.admin.referenceType.audio'),
  }
  return (
    <fieldset className='space-y-3 rounded-md border p-3'>
      <legend className='px-1 text-sm font-medium'>{labels[props.kind]}</legend>
      <FormField
        control={form.control}
        name={`reference_policy.${props.kind}.allowed_mime_types`}
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Allowed MIME types')}</FormLabel>
            <FormControl>
              <Input
                {...field}
                value={field.value?.join(', ') ?? ''}
                onChange={(event) =>
                  field.onChange(
                    event.target.value
                      .split(',')
                      .map((value) => value.trim().toLowerCase())
                  )
                }
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <div className='grid gap-3 sm:grid-cols-2'>
        {numberFields.map((item) => (
          <FormField
            key={item.key}
            control={form.control}
            name={
              `reference_policy.${props.kind}.${item.key}` as FieldPath<VideoModelProfileFormValues>
            }
            render={({ field }) => (
              <FormItem>
                <FormLabel>{item.label}</FormLabel>
                <FormControl>
                  <Input
                    name={field.name}
                    ref={field.ref}
                    onBlur={field.onBlur}
                    type='number'
                    min={
                      item.key.startsWith('min_')
                        ? (accepted?.[
                            item.key as keyof VideoReferenceMediaPolicy
                          ] as number | undefined)
                        : 0
                    }
                    max={
                      item.key.startsWith('max_')
                        ? (accepted?.[
                            item.key as keyof VideoReferenceMediaPolicy
                          ] as number | undefined)
                        : undefined
                    }
                    step={item.step}
                    value={typeof field.value === 'number' ? field.value : ''}
                    onChange={(event) =>
                      field.onChange(
                        event.target.value === ''
                          ? undefined
                          : event.target.valueAsNumber
                      )
                    }
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        ))}
      </div>
    </fieldset>
  )
}

function VideoModelPresetParameterEditor(props: { index: number }) {
  const { t } = useTranslation()
  const form = useFormContext<VideoModelProfileFormValues>()
  const parameter = useWatch({
    control: form.control,
    name: `parameters.${props.index}`,
  })
  if (!parameter) return null
  const parameterLabelKey = VIDEO_PARAMETER_LABEL_KEYS[parameter.key]
  const parameterLabel = parameterLabelKey
    ? t(parameterLabelKey, { defaultValue: parameter.label })
    : parameter.label

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
                  {(() => {
                    const fallback = option.label || String(option.value)
                    const labelKey =
                      VIDEO_PARAMETER_OPTION_LABEL_KEYS[parameter.key]?.[
                        String(option.value)
                      ]
                    return labelKey
                      ? t(labelKey, { defaultValue: fallback })
                      : fallback
                  })()}
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
            <FormLabel>{parameterLabel}</FormLabel>
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
  const modes = useWatch({ control: form.control, name: 'modes' })
  const referencePolicy = useWatch({
    control: form.control,
    name: 'reference_policy',
  })

  if (!modelName) {
    return (
      <div className='border-t pt-4'>
        <p className='text-muted-foreground text-sm'>
          {t('videoStudio.admin.selectModelFirst')}
        </p>
      </div>
    )
  }

  const supportedReferences = [
    referencePolicy?.image && t('videoStudio.admin.referenceType.image'),
    referencePolicy?.video && t('videoStudio.admin.referenceType.video'),
    referencePolicy?.audio && t('videoStudio.admin.referenceType.audio'),
  ].filter((value): value is string => Boolean(value))
  const presetPolicy =
    getVideoModelPreset(modelName)?.specification.reference_policy

  return (
    <div className='space-y-5 border-t pt-4'>
      <div className='divide-y rounded-md border px-3'>
        <div className='flex items-center justify-between gap-3 py-3'>
          <span className='text-muted-foreground text-sm'>
            {t('videoStudio.admin.outputResolution')}
          </span>
          <span className='text-sm font-medium'>
            {t('videoStudio.admin.serviceDefined')}
          </span>
        </div>
        <div className='flex items-center justify-between gap-3 py-3'>
          <span className='text-muted-foreground text-sm'>
            {t('videoStudio.admin.modes')}
          </span>
          <span className='text-right text-sm font-medium'>
            {modes.map((mode) => t(VIDEO_MODE_LABEL_KEYS[mode])).join(' / ')}
          </span>
        </div>
        {supportedReferences.length > 0 && (
          <div className='flex items-center justify-between gap-3 py-3'>
            <span className='text-muted-foreground text-sm'>
              {t('videoStudio.admin.referenceType')}
            </span>
            <span className='text-right text-sm font-medium'>
              {supportedReferences.join(' / ')}
            </span>
          </div>
        )}
      </div>

      <div className='space-y-3'>
        <h4 className='text-sm font-semibold'>{t('Reference limits')}</h4>
        {!referencePolicy && presetPolicy && (
          <div className='space-y-2'>
            <p className='text-muted-foreground text-sm'>
              {t(
                'Apply the model’s documented reference limits. Existing defaults and enabled status are kept.'
              )}
            </p>
            <Button
              type='button'
              variant='outline'
              onClick={() => {
                const next = applyVideoModelReferencePreset(form.getValues())
                form.setValue('modes', next.modes, { shouldDirty: true })
                form.setValue('parameters', next.parameters, {
                  shouldDirty: true,
                })
                form.setValue('reference_policy', next.reference_policy, {
                  shouldDirty: true,
                  shouldValidate: true,
                })
              }}
            >
              {t('Apply reference preset')}
            </Button>
          </div>
        )}
        {referencePolicy && (
          <>
            {(
              [
                ['requires_visual', t('Require an image or video reference')],
                ['requires_video', t('Require a video reference')],
                [
                  'audio_requires_generate_audio',
                  t('Require generated audio for audio references'),
                ],
              ] as const
            ).map(([key, label]) => (
              <FormField
                key={key}
                control={form.control}
                name={`reference_policy.${key}`}
                render={({ field }) => (
                  <FormItem className='flex items-center justify-between gap-3'>
                    <FormLabel>{label}</FormLabel>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        disabled={
                          presetPolicy?.[key] === true && field.value === true
                        }
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            ))}
            {(['image', 'video', 'audio'] as const).map(
              (kind) =>
                referencePolicy[kind] && (
                  <VideoReferenceMediaPolicyEditor key={kind} kind={kind} />
                )
            )}
          </>
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
