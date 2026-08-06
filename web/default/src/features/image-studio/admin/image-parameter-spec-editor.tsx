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
import { Plus, Trash2 } from 'lucide-react'
import { useFieldArray, useFormContext, useWatch } from 'react-hook-form'
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
import { Textarea } from '@/components/ui/textarea'

import type { ImageParameterControl } from '../types'
import {
  controlForImageRequestField,
  IMAGE_REQUEST_FIELDS,
  type ImageModelFormValues,
  type ImageParameterFormValues,
  type ImageRequestField,
} from './image-admin-forms'

const newParameter = (
  requestKey: ImageRequestField
): ImageParameterFormValues => {
  const control = controlForImageRequestField(requestKey)
  return {
    key: requestKey === 'n' ? 'count' : requestKey,
    label: requestKey,
    request_key: requestKey,
    required: false,
    has_default: false,
    default_value: control === 'boolean' ? false : '',
    options_text: '',
    min: requestKey === 'n' ? 1 : 0,
    max: requestKey === 'n' ? 4 : 100,
  }
}

export function ImageParameterSpecEditor() {
  const { t } = useTranslation()
  const form = useFormContext<ImageModelFormValues>()
  const parameters = useWatch({ control: form.control, name: 'parameters' })
  const fields = useFieldArray({ control: form.control, name: 'parameters' })
  const used = new Set(parameters.map((parameter) => parameter.request_key))
  const nextField = IMAGE_REQUEST_FIELDS.find((field) => !used.has(field))
  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between'>
        <div>
          <h4 className='text-sm font-medium'>
            {t('imageStudio.admin.parameters')}
          </h4>
          <p className='text-muted-foreground text-xs'>
            {t('imageStudio.admin.parametersDescription')}
          </p>
        </div>
        <Button
          type='button'
          size='sm'
          variant='outline'
          disabled={!nextField}
          onClick={() => nextField && fields.append(newParameter(nextField))}
        >
          <Plus aria-hidden='true' />
          {t('imageStudio.admin.addParameter')}
        </Button>
      </div>
      {fields.fields.map((field, index) => (
        <ParameterRow
          key={field.id}
          index={index}
          onRemove={() => fields.remove(index)}
        />
      ))}
    </div>
  )
}

function ParameterRow(props: { index: number; onRemove: () => void }) {
  const { t } = useTranslation()
  const form = useFormContext<ImageModelFormValues>()
  const requestKey = useWatch({
    control: form.control,
    name: `parameters.${props.index}.request_key`,
  })
  const hasDefault = useWatch({
    control: form.control,
    name: `parameters.${props.index}.has_default`,
  })
  const control = controlForImageRequestField(requestKey)
  const changeRequestKey = (next: ImageRequestField): void => {
    const nextControl = controlForImageRequestField(next)
    form.setValue(`parameters.${props.index}.request_key`, next)
    form.setValue(`parameters.${props.index}.options_text`, '')
    form.setValue(`parameters.${props.index}.min`, next === 'n' ? 1 : 0)
    form.setValue(`parameters.${props.index}.max`, next === 'n' ? 4 : 100)
    form.setValue(
      `parameters.${props.index}.default_value`,
      nextControl === 'boolean' ? false : ''
    )
    form.setValue(`parameters.${props.index}.has_default`, false)
  }
  return (
    <div className='space-y-3 rounded-lg border p-3'>
      <div className='flex justify-between gap-2'>
        <div className='grid min-w-0 flex-1 gap-3 sm:grid-cols-3'>
          <FormField
            control={form.control}
            name={`parameters.${props.index}.key`}
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('imageStudio.admin.parameterKey')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name={`parameters.${props.index}.label`}
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('imageStudio.admin.parameterLabel')}</FormLabel>
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
                <FormLabel>{t('imageStudio.admin.requestField')}</FormLabel>
                <FormControl>
                  <NativeSelect
                    value={field.value}
                    onChange={(event) =>
                      changeRequestKey(event.target.value as ImageRequestField)
                    }
                  >
                    {IMAGE_REQUEST_FIELDS.map((value) => (
                      <NativeSelectOption key={value} value={value}>
                        {value}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>
        <Button
          type='button'
          size='icon-sm'
          variant='ghost'
          onClick={props.onRemove}
          aria-label={t('Delete')}
        >
          <Trash2 aria-hidden='true' />
        </Button>
      </div>
      {control === 'select' && (
        <FormField
          control={form.control}
          name={`parameters.${props.index}.options_text`}
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('imageStudio.admin.options')}</FormLabel>
              <FormControl>
                <Textarea
                  {...field}
                  rows={3}
                  placeholder={t('imageStudio.admin.optionsPlaceholder')}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      )}
      {control === 'integer' && (
        <div className='grid gap-3 sm:grid-cols-2'>
          {(['min', 'max'] as const).map((name) => (
            <FormField
              key={name}
              control={form.control}
              name={`parameters.${props.index}.${name}`}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t(`imageStudio.admin.${name}`)}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      value={field.value}
                      onChange={(event) =>
                        field.onChange(Number(event.target.value))
                      }
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}
        </div>
      )}
      <div className='grid gap-3 sm:grid-cols-2'>
        {(['required', 'has_default'] as const).map((name) => (
          <FormField
            key={name}
            control={form.control}
            name={`parameters.${props.index}.${name}`}
            render={({ field }) => (
              <FormItem className='flex min-h-10 items-center justify-between rounded-md border px-3'>
                <FormLabel>{t(`imageStudio.admin.${name}`)}</FormLabel>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />
        ))}
      </div>
      {hasDefault && (
        <DefaultValueField index={props.index} control={control} />
      )}
    </div>
  )
}

function DefaultValueField(props: {
  index: number
  control: ImageParameterControl['control']
}) {
  const { t } = useTranslation()
  const form = useFormContext<ImageModelFormValues>()
  return (
    <FormField
      control={form.control}
      name={`parameters.${props.index}.default_value`}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('imageStudio.admin.defaultValue')}</FormLabel>
          <FormControl>
            {props.control === 'boolean' ? (
              <Switch
                checked={field.value === true}
                onCheckedChange={field.onChange}
              />
            ) : (
              <Input
                type={props.control === 'integer' ? 'number' : 'text'}
                value={String(field.value)}
                onChange={(event) =>
                  field.onChange(
                    props.control === 'integer'
                      ? Number(event.target.value)
                      : event.target.value
                  )
                }
              />
            )}
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
