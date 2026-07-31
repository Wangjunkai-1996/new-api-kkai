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
import { ChevronDown, Plus, Trash2 } from 'lucide-react'
import { useId, useState } from 'react'
import { useFieldArray, useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import {
  createVideoModelParameterFormValues,
  pruneVideoModelParametersForModes,
  VIDEO_GENERATION_MODES,
  type VideoModelParameterFormValues,
  type VideoModelProfileFormValues,
} from '../schemas'
import type { VideoGenerationMode, VideoParameterValue } from '../types'
import {
  decodeVideoParameterOptionValue,
  encodeVideoParameterOptionValue,
  VIDEO_MODE_LABEL_KEYS,
} from '../video-domain'

const CONTROL_LABEL_KEYS: Record<
  VideoModelParameterFormValues['control'],
  string
> = {
  segmented: 'videoStudio.admin.control.segmented',
  select: 'videoStudio.admin.control.select',
  slider: 'videoStudio.admin.control.slider',
  number: 'videoStudio.admin.control.number',
  switch: 'videoStudio.admin.control.switch',
}

const RANGE_LABEL_KEYS = {
  min: 'videoStudio.admin.parameterMin',
  max: 'videoStudio.admin.parameterMax',
  step: 'videoStudio.admin.parameterStep',
} as const

const defaultValueForParameter = (
  parameter: VideoModelParameterFormValues
): VideoParameterValue | undefined => {
  if (parameter.control === 'segmented' || parameter.control === 'select') {
    return parameter.options[0]?.value
  }
  if (parameter.control === 'slider' || parameter.control === 'number') {
    return parameter.min
  }
  return false
}

const optionValueForType = (
  type: VideoModelParameterFormValues['options'][number]['value_type']
): VideoParameterValue => {
  if (type === 'number') return 0
  if (type === 'boolean') return false
  return ''
}

function ParameterOptionEditor(props: {
  parameterIndex: number
  optionIndex: number
  onRemove: () => void
}) {
  const { t } = useTranslation()
  const form = useFormContext<VideoModelProfileFormValues>()
  const option = useWatch({
    control: form.control,
    name: `parameters.${props.parameterIndex}.options.${props.optionIndex}`,
  })
  const inputPrefix = useId()

  return (
    <div className='grid gap-2 border-t pt-3 sm:grid-cols-[minmax(0,1fr)_9rem_minmax(0,1fr)_2rem]'>
      <FormField
        control={form.control}
        name={`parameters.${props.parameterIndex}.options.${props.optionIndex}.label`}
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('videoStudio.admin.optionLabel')}</FormLabel>
            <FormControl>
              <Input {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name={`parameters.${props.parameterIndex}.options.${props.optionIndex}.value_type`}
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('videoStudio.admin.valueType')}</FormLabel>
            <FormControl>
              <NativeSelect
                value={field.value}
                onChange={(event) => {
                  const nextType = event.target.value as typeof field.value
                  field.onChange(nextType)
                  form.setValue(
                    `parameters.${props.parameterIndex}.options.${props.optionIndex}.value`,
                    optionValueForType(nextType),
                    { shouldDirty: true, shouldValidate: true }
                  )
                }}
              >
                <NativeSelectOption value='string'>
                  {t('videoStudio.admin.valueType.string')}
                </NativeSelectOption>
                <NativeSelectOption value='number'>
                  {t('videoStudio.admin.valueType.number')}
                </NativeSelectOption>
                <NativeSelectOption value='boolean'>
                  {t('videoStudio.admin.valueType.boolean')}
                </NativeSelectOption>
              </NativeSelect>
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name={`parameters.${props.parameterIndex}.options.${props.optionIndex}.value`}
        render={({ field }) => (
          <FormItem>
            <FormLabel htmlFor={`${inputPrefix}-value`}>
              {t('videoStudio.admin.optionValue')}
            </FormLabel>
            <FormControl>
              {option?.value_type === 'boolean' ? (
                <NativeSelect
                  id={`${inputPrefix}-value`}
                  value={String(Boolean(field.value))}
                  onChange={(event) =>
                    field.onChange(event.target.value === 'true')
                  }
                >
                  <NativeSelectOption value='false'>
                    {t('videoStudio.admin.boolean.false')}
                  </NativeSelectOption>
                  <NativeSelectOption value='true'>
                    {t('videoStudio.admin.boolean.true')}
                  </NativeSelectOption>
                </NativeSelect>
              ) : (
                <Input
                  id={`${inputPrefix}-value`}
                  type={option?.value_type === 'number' ? 'number' : 'text'}
                  value={
                    typeof field.value === 'boolean' ? '' : (field.value ?? '')
                  }
                  onChange={(event) => {
                    if (option?.value_type === 'number') {
                      if (event.target.value !== '') {
                        field.onChange(event.target.valueAsNumber)
                      }
                      return
                    }
                    field.onChange(event.target.value)
                  }}
                />
              )}
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <div className='flex items-end pb-0.5'>
        <Button
          type='button'
          size='icon-sm'
          variant='ghost'
          onClick={props.onRemove}
          aria-label={t('videoStudio.admin.removeOption')}
        >
          <Trash2 aria-hidden='true' />
        </Button>
      </div>
    </div>
  )
}

function ParameterDefaultEditor(props: { index: number }) {
  const { t } = useTranslation()
  const form = useFormContext<VideoModelProfileFormValues>()
  const parameter = useWatch({
    control: form.control,
    name: `parameters.${props.index}`,
  })
  if (!parameter?.has_default) return null

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
              <NativeSelectOption value=''>
                {t('videoStudio.admin.selectDefault')}
              </NativeSelectOption>
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
              onChange={(event) => {
                if (event.target.value !== '') {
                  field.onChange(event.target.valueAsNumber)
                }
              }}
            />
          )
        }

        return (
          <FormItem>
            <FormLabel>{t('videoStudio.admin.parameterDefault')}</FormLabel>
            <FormControl>{control}</FormControl>
            <FormMessage />
          </FormItem>
        )
      }}
    />
  )
}

function VideoModelParameterEditor(props: {
  index: number
  onRemove: () => void
}) {
  const { t } = useTranslation()
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const form = useFormContext<VideoModelProfileFormValues>()
  const parameter = useWatch({
    control: form.control,
    name: `parameters.${props.index}`,
  })
  const modes = useWatch({ control: form.control, name: 'modes' })
  const options = useFieldArray({
    control: form.control,
    name: `parameters.${props.index}.options`,
  })
  const isChoice =
    parameter?.control === 'segmented' || parameter?.control === 'select'
  const isNumeric =
    parameter?.control === 'slider' || parameter?.control === 'number'
  const parameterErrors = form.formState.errors.parameters?.[props.index]

  const changeControl = (control: VideoModelParameterFormValues['control']) => {
    form.setValue(`parameters.${props.index}.control`, control, {
      shouldDirty: true,
      shouldValidate: true,
    })
    if (control === 'segmented' || control === 'select') {
      if (options.fields.length === 0) {
        options.append({ label: '', value_type: 'string', value: '' })
      }
    } else if (options.fields.length > 0) {
      options.replace([])
    }
    if (parameter?.has_default) {
      let nextOptions: VideoModelParameterFormValues['options'] = []
      if (control === 'segmented' || control === 'select') {
        nextOptions =
          parameter.options.length > 0
            ? parameter.options
            : [{ label: '', value_type: 'string', value: '' }]
      }
      const nextParameter = {
        ...parameter,
        control,
        options: nextOptions,
      }
      form.setValue(
        `parameters.${props.index}.default_value`,
        defaultValueForParameter(nextParameter),
        { shouldDirty: true, shouldValidate: true }
      )
      form.setValue(
        `parameters.${props.index}.preserved_inline_default`,
        undefined,
        { shouldDirty: true }
      )
    }
  }

  const toggleParameterMode = (mode: VideoGenerationMode, checked: boolean) => {
    const current = parameter?.modes ?? []
    const next = checked
      ? [...current, mode]
      : current.filter((candidate) => candidate !== mode)
    form.setValue(`parameters.${props.index}.modes`, next, {
      shouldDirty: true,
      shouldValidate: true,
    })
    form.setValue(`parameters.${props.index}.modes_explicit`, true, {
      shouldDirty: true,
    })
  }

  return (
    <div className='space-y-4 rounded-md border p-3'>
      <div className='flex items-center justify-between gap-3'>
        <span className='text-sm font-semibold'>
          {parameter?.label ||
            t('videoStudio.admin.parameterNumber', {
              number: props.index + 1,
            })}
        </span>
        <Button
          type='button'
          size='icon-sm'
          variant='ghost'
          onClick={props.onRemove}
          aria-label={t('videoStudio.admin.removeParameter')}
        >
          <Trash2 aria-hidden='true' />
        </Button>
      </div>

      <div className='grid gap-3 sm:grid-cols-2'>
        <FormField
          control={form.control}
          name={`parameters.${props.index}.label`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('videoStudio.admin.parameterLabel')}</FormLabel>
              <FormControl>
                <Input {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name={`parameters.${props.index}.control`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('videoStudio.admin.parameterControl')}</FormLabel>
              <FormControl>
                <NativeSelect
                  value={field.value}
                  onChange={(event) =>
                    changeControl(
                      event.target
                        .value as VideoModelParameterFormValues['control']
                    )
                  }
                >
                  {Object.entries(CONTROL_LABEL_KEYS).map(([control, key]) => (
                    <NativeSelectOption key={control} value={control}>
                      {t(key)}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      </div>

      <div className='grid gap-3 sm:grid-cols-2'>
        <FormField
          control={form.control}
          name={`parameters.${props.index}.required`}
          render={({ field }) => (
            <FormItem className='flex items-center justify-between gap-3 border-t pt-3'>
              <FormLabel>{t('videoStudio.admin.parameterRequired')}</FormLabel>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={(checked) => {
                    field.onChange(checked)
                    if (!checked || !parameter || parameter.has_default) return
                    form.setValue(
                      `parameters.${props.index}.has_default`,
                      true,
                      { shouldDirty: true, shouldValidate: true }
                    )
                    form.setValue(
                      `parameters.${props.index}.default_value`,
                      defaultValueForParameter(parameter),
                      { shouldDirty: true, shouldValidate: true }
                    )
                  }}
                />
              </FormControl>
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name={`parameters.${props.index}.has_default`}
          render={({ field }) => (
            <FormItem className='flex items-center justify-between gap-3 border-t pt-3'>
              <FormLabel>
                {t('videoStudio.admin.parameterHasDefault')}
              </FormLabel>
              <FormControl>
                <Switch
                  checked={field.value}
                  disabled={parameter?.required}
                  onCheckedChange={(checked) => {
                    field.onChange(checked)
                    form.setValue(
                      `parameters.${props.index}.default_value`,
                      checked && parameter
                        ? defaultValueForParameter(parameter)
                        : undefined,
                      { shouldDirty: true, shouldValidate: true }
                    )
                  }}
                />
              </FormControl>
            </FormItem>
          )}
        />
      </div>

      <ParameterDefaultEditor index={props.index} />

      {isNumeric && (
        <div className='grid gap-3 sm:grid-cols-3'>
          {(['min', 'max', 'step'] as const).map((name) => (
            <FormField
              key={name}
              control={form.control}
              name={`parameters.${props.index}.${name}`}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t(RANGE_LABEL_KEYS[name])}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      value={field.value}
                      onChange={(event) => {
                        if (event.target.value !== '') {
                          field.onChange(event.target.valueAsNumber)
                        }
                      }}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}
        </div>
      )}

      {isChoice && (
        <div className='space-y-3'>
          <div className='flex items-center justify-between gap-3'>
            <span className='text-sm font-medium'>
              {t('videoStudio.admin.parameterOptions')}
            </span>
            <Button
              type='button'
              size='sm'
              variant='outline'
              onClick={() =>
                options.append({ label: '', value_type: 'string', value: '' })
              }
            >
              <Plus aria-hidden='true' />
              {t('videoStudio.admin.addOption')}
            </Button>
          </div>
          {options.fields.map((option, optionIndex) => (
            <ParameterOptionEditor
              key={option.id}
              parameterIndex={props.index}
              optionIndex={optionIndex}
              onRemove={() => options.remove(optionIndex)}
            />
          ))}
          {parameterErrors?.options?.message && (
            <p className='text-destructive text-sm' role='alert'>
              {t(String(parameterErrors.options.message))}
            </p>
          )}
        </div>
      )}

      <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
        <CollapsibleTrigger
          render={
            <button
              type='button'
              className='text-muted-foreground hover:text-foreground flex items-center gap-1.5 text-sm'
            />
          }
        >
          {t('videoStudio.admin.advancedParameterSettings')}
          <ChevronDown
            className={cn(
              'size-4 transition-transform',
              advancedOpen && 'rotate-180'
            )}
            aria-hidden='true'
          />
        </CollapsibleTrigger>
        <CollapsibleContent className='mt-3 space-y-3 border-t pt-3'>
          <div className='grid gap-3 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name={`parameters.${props.index}.key`}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('videoStudio.admin.parameterKey')}</FormLabel>
                  <FormControl>
                    <Input {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name={`parameters.${props.index}.request_key`}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('videoStudio.admin.requestKey')}</FormLabel>
                  <FormControl>
                    <Input {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
          <fieldset className='space-y-2'>
            <legend className='text-sm font-medium'>
              {t('videoStudio.admin.parameterModes')}
            </legend>
            <div className='flex flex-wrap gap-x-4 gap-y-2'>
              {modes.map((mode) => (
                <Label key={mode} className='gap-2'>
                  <Checkbox
                    checked={parameter?.modes.includes(mode) ?? false}
                    onCheckedChange={(checked) =>
                      toggleParameterMode(mode, Boolean(checked))
                    }
                  />
                  {t(VIDEO_MODE_LABEL_KEYS[mode])}
                </Label>
              ))}
            </div>
            {parameterErrors?.modes?.message && (
              <p className='text-destructive text-sm' role='alert'>
                {t(String(parameterErrors.modes.message))}
              </p>
            )}
          </fieldset>
        </CollapsibleContent>
      </Collapsible>
    </div>
  )
}

export function VideoModelSpecEditor() {
  const { t } = useTranslation()
  const [requestMappingOpen, setRequestMappingOpen] = useState(false)
  const form = useFormContext<VideoModelProfileFormValues>()
  const modes = useWatch({ control: form.control, name: 'modes' })
  const parameters = useFieldArray({
    control: form.control,
    name: 'parameters',
  })
  const modeError = form.formState.errors.modes?.message

  const appendParameter = () => {
    const usedKeys = new Set(
      form.getValues('parameters').map((parameter) => parameter.key)
    )
    let sequence = parameters.fields.length + 1
    let key = `parameter_${sequence}`
    while (usedKeys.has(key)) {
      sequence += 1
      key = `parameter_${sequence}`
    }
    parameters.append(
      createVideoModelParameterFormValues(
        modes,
        key,
        t('videoStudio.admin.newParameterLabel', { number: sequence })
      )
    )
  }

  const toggleMode = (mode: VideoGenerationMode, checked: boolean) => {
    const next = checked
      ? VIDEO_GENERATION_MODES.filter(
          (candidate) => modes.includes(candidate) || candidate === mode
        )
      : modes.filter((candidate) => candidate !== mode)
    if (next.length === 0) return
    form.setValue('modes', next, { shouldDirty: true, shouldValidate: true })
    const normalizedParameters = pruneVideoModelParametersForModes(
      form.getValues('parameters'),
      next
    )
    form.setValue('parameters', normalizedParameters, {
      shouldDirty: true,
      shouldValidate: true,
    })
  }

  return (
    <div className='space-y-5 border-t pt-4'>
      <fieldset className='space-y-3'>
        <legend className='text-sm font-semibold'>
          {t('videoStudio.admin.modes')}
        </legend>
        <div className='flex flex-wrap gap-x-5 gap-y-2'>
          {VIDEO_GENERATION_MODES.map((mode) => (
            <Label key={mode} className='gap-2'>
              <Checkbox
                checked={modes.includes(mode)}
                onCheckedChange={(checked) =>
                  toggleMode(mode, Boolean(checked))
                }
              />
              {t(VIDEO_MODE_LABEL_KEYS[mode])}
            </Label>
          ))}
        </div>
        {modeError && (
          <p className='text-destructive text-sm' role='alert'>
            {t(String(modeError))}
          </p>
        )}
      </fieldset>

      {modes.includes('image_to_video') && (
        <div className='space-y-3 border-t pt-4'>
          <h4 className='text-sm font-semibold'>
            {t('videoStudio.admin.imageReference')}
          </h4>
          <FormField
            control={form.control}
            name='image_reference_role'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('videoStudio.admin.referenceType')}</FormLabel>
                <FormControl>
                  <NativeSelect {...field}>
                    <NativeSelectOption value='reference'>
                      {t('videoStudio.admin.referenceType.image')}
                    </NativeSelectOption>
                    <NativeSelectOption value='reference_video'>
                      {t('videoStudio.admin.referenceType.video')}
                    </NativeSelectOption>
                  </NativeSelect>
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>
      )}

      {(modes.includes('image_to_video') ||
        modes.includes('first_last_frame')) && (
        <Collapsible
          open={requestMappingOpen}
          onOpenChange={setRequestMappingOpen}
        >
          <CollapsibleTrigger
            render={
              <button
                type='button'
                className='text-muted-foreground hover:text-foreground flex items-center gap-1.5 text-sm'
              />
            }
          >
            {t('videoStudio.admin.advancedRequestMapping')}
            <ChevronDown
              className={cn(
                'size-4 transition-transform',
                requestMappingOpen && 'rotate-180'
              )}
              aria-hidden='true'
            />
          </CollapsibleTrigger>
          <CollapsibleContent className='mt-3 grid gap-3 border-t pt-3 sm:grid-cols-2'>
            {modes.includes('image_to_video') && (
              <FormField
                control={form.control}
                name='image_reference_request_key'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('videoStudio.admin.imageReference')} ·{' '}
                      {t('videoStudio.admin.requestKey')}
                    </FormLabel>
                    <FormControl>
                      <Input {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}
            {modes.includes('first_last_frame') && (
              <>
                <FormField
                  control={form.control}
                  name='first_frame_request_key'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('videoStudio.firstFrame')} ·{' '}
                        {t('videoStudio.admin.requestKey')}
                      </FormLabel>
                      <FormControl>
                        <Input {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='last_frame_request_key'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('videoStudio.lastFrame')} ·{' '}
                        {t('videoStudio.admin.requestKey')}
                      </FormLabel>
                      <FormControl>
                        <Input {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </>
            )}
          </CollapsibleContent>
        </Collapsible>
      )}

      <div className='space-y-3 border-t pt-4'>
        <div className='flex items-center justify-between gap-3'>
          <h4 className='text-sm font-semibold'>
            {t('videoStudio.admin.parameters')}
          </h4>
          <Button
            type='button'
            size='sm'
            variant='outline'
            onClick={appendParameter}
          >
            <Plus aria-hidden='true' />
            {t('videoStudio.admin.addParameter')}
          </Button>
        </div>
        {parameters.fields.length === 0 && (
          <p className='text-muted-foreground text-sm'>
            {t('videoStudio.admin.noParameters')}
          </p>
        )}
        {parameters.fields.map((parameter, index) => (
          <VideoModelParameterEditor
            key={parameter.id}
            index={index}
            onRemove={() => parameters.remove(index)}
          />
        ))}
      </div>
    </div>
  )
}
