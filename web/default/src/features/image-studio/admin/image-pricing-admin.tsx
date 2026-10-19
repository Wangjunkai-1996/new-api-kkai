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
import { zodResolver } from '@hookform/resolvers/zod'
import { useQueryClient } from '@tanstack/react-query'
import { CircleAlert, LoaderCircle, RotateCw } from 'lucide-react'
import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { useForm, type Control } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import type { z } from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { useSystemOptions } from '@/features/system-settings/hooks/use-system-options'
import { useUpdateOption } from '@/features/system-settings/hooks/use-update-option'
import type { SystemOptionsResponse } from '@/features/system-settings/types'
import { safeNumberFieldProps } from '@/features/system-settings/utils/numeric-field'

import {
  getImagePricingFormValues,
  IMAGE_PRICING_OPTION_KEY,
  imagePricingFormSchema,
  parseImagePricingPolicy,
  updateImagePricingPolicy,
  type ImagePricingFormValues,
  type ImagePricingPolicy,
} from './image-pricing-policy'

type ImagePricingFormInput = z.input<typeof imagePricingFormSchema>

type PriceFieldProps = {
  control: Control<ImagePricingFormInput, unknown, ImagePricingFormValues>
  name: 'price1k' | 'price2k' | 'price4k'
  label: string
  disabled: boolean
}

function PriceField(props: PriceFieldProps) {
  return (
    <FormField
      control={props.control}
      name={props.name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{props.label}</FormLabel>
          <InputGroup>
            <InputGroupAddon>$</InputGroupAddon>
            <FormControl>
              <InputGroupInput
                type='number'
                min='0'
                step='0.01'
                disabled={props.disabled}
                {...safeNumberFieldProps(field)}
              />
            </FormControl>
          </InputGroup>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

type ModelSelectionProps = {
  policy: ImagePricingPolicy
  modelName: string
  onModelChange: (modelName: string) => void
  disabled?: boolean
}

function ImagePricingModelSelect(props: ModelSelectionProps) {
  const { t } = useTranslation()
  const id = useId()
  return (
    <div className='space-y-2'>
      <Label htmlFor={id}>{t('Model')}</Label>
      <NativeSelect
        id={id}
        className='w-full'
        value={props.modelName}
        disabled={props.disabled}
        onChange={(event) => props.onModelChange(event.target.value)}
      >
        {Object.keys(props.policy.models).map((modelName) => (
          <NativeSelectOption key={modelName} value={modelName}>
            {modelName}
          </NativeSelectOption>
        ))}
      </NativeSelect>
    </div>
  )
}

function ImagePricingForm(
  props: ModelSelectionProps & { values: ImagePricingFormValues }
) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const queryClient = useQueryClient()
  const [pendingModel, setPendingModel] = useState<string | null>(null)
  const form = useForm<ImagePricingFormInput, unknown, ImagePricingFormValues>({
    resolver: zodResolver(imagePricingFormSchema),
    defaultValues: props.values,
  })
  const isDirty = form.formState.isDirty
  const isSaving = updateOption.isPending || form.formState.isSubmitting
  const isDirtyRef = useRef(isDirty)
  useEffect(() => {
    isDirtyRef.current = isDirty
  }, [isDirty])

  // A background refetch must not replace edits that have not been saved.
  useEffect(() => {
    if (!isDirtyRef.current) {
      form.reset(props.values)
    }
  }, [form, props.values])

  const submit = form.handleSubmit(async (values) => {
    const nextPolicy = updateImagePricingPolicy(
      props.policy,
      props.modelName,
      values
    )
    try {
      const response = await updateOption.mutateAsync({
        key: IMAGE_PRICING_OPTION_KEY,
        value: JSON.stringify(nextPolicy),
      })
      if (response.success) {
        // Keep the next model's save based on this accepted policy even when
        // the shared mutation hook's background refetch has not finished.
        await queryClient.cancelQueries({ queryKey: ['system-options'] })
        queryClient.setQueryData<SystemOptionsResponse>(
          ['system-options'],
          (previous) =>
            previous && {
              ...previous,
              data: previous.data.map((option) =>
                option.key === IMAGE_PRICING_OPTION_KEY
                  ? { ...option, value: JSON.stringify(nextPolicy) }
                  : option
              ),
            }
        )
        form.reset(values)
      }
    } catch {
      // The shared mutation hook displays the request error.
    }
  })

  return (
    <Form {...form}>
      <form
        className='max-w-2xl space-y-5 rounded-lg border p-4 sm:p-5'
        onSubmit={submit}
        noValidate
      >
        <ImagePricingModelSelect
          policy={props.policy}
          modelName={props.modelName}
          disabled={isSaving}
          onModelChange={(modelName) => {
            if (isDirty) {
              setPendingModel(modelName)
            } else {
              props.onModelChange(modelName)
            }
          }}
        />
        <FormField
          control={form.control}
          name='enabled'
          render={({ field }) => (
            <FormItem className='flex min-h-10 items-center justify-between gap-4 border-b pb-4'>
              <FormLabel>{t('imageStudio.admin.pricing.enable')}</FormLabel>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                  disabled={isSaving}
                />
              </FormControl>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='allow_custom_size'
          render={({ field }) => (
            <FormItem className='flex min-h-10 items-center justify-between gap-4 border-b pb-4'>
              <div>
                <FormLabel>{t('imageStudio.admin.customSize')}</FormLabel>
                <p className='text-muted-foreground text-xs'>
                  {t('imageStudio.admin.customSizeDescription')}
                </p>
              </div>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                  disabled={isSaving}
                />
              </FormControl>
            </FormItem>
          )}
        />

        <div className='grid gap-4 sm:grid-cols-3'>
          <PriceField
            control={form.control}
            name='price1k'
            label={t('imageStudio.admin.pricing.price1k')}
            disabled={isSaving}
          />
          <PriceField
            control={form.control}
            name='price2k'
            label={t('imageStudio.admin.pricing.price2k')}
            disabled={isSaving}
          />
          <PriceField
            control={form.control}
            name='price4k'
            label={t('imageStudio.admin.pricing.price4k')}
            disabled={isSaving}
          />
        </div>

        <div className='flex justify-end border-t pt-4'>
          <Button type='submit' disabled={!isDirty || isSaving}>
            {isSaving && (
              <LoaderCircle className='animate-spin' aria-hidden='true' />
            )}
            {t(isSaving ? 'Saving...' : 'Save')}
          </Button>
        </div>
      </form>
      <ConfirmDialog
        open={pendingModel !== null}
        onOpenChange={(open) => {
          if (!open) setPendingModel(null)
        }}
        title={t('Unsaved changes')}
        desc={t('Discard unsaved changes?')}
        confirmText={t('Discard changes')}
        handleConfirm={() => {
          if (pendingModel !== null) props.onModelChange(pendingModel)
        }}
      />
    </Form>
  )
}

function ImagePricingEditor(props: { policy: ImagePricingPolicy }) {
  const { t } = useTranslation()
  const [selection, setSelection] = useState('')
  const modelName = Object.hasOwn(props.policy.models, selection)
    ? selection
    : (Object.keys(props.policy.models)[0] ?? '')
  const values = useMemo(() => {
    try {
      return getImagePricingFormValues(props.policy, modelName)
    } catch {
      return null
    }
  }, [props.policy, modelName])

  if (!values) {
    return (
      <div className='max-w-2xl space-y-5 rounded-lg border p-4 sm:p-5'>
        <ImagePricingModelSelect
          policy={props.policy}
          modelName={modelName}
          onModelChange={setSelection}
        />
        <Alert variant='destructive'>
          <CircleAlert aria-hidden='true' />
          <AlertDescription>
            {t('imageStudio.admin.pricing.invalidPolicy')}
          </AlertDescription>
        </Alert>
      </div>
    )
  }

  return (
    <ImagePricingForm
      key={modelName}
      policy={props.policy}
      modelName={modelName}
      onModelChange={setSelection}
      values={values}
    />
  )
}

export function ImagePricingAdmin() {
  const { t } = useTranslation()
  const optionsQuery = useSystemOptions()
  const policy = useMemo(() => {
    const option = optionsQuery.data?.data.find(
      (candidate) => candidate.key === IMAGE_PRICING_OPTION_KEY
    )
    if (!option) return null

    try {
      return parseImagePricingPolicy(option.value)
    } catch {
      return null
    }
  }, [optionsQuery.data?.data])

  if (optionsQuery.isLoading) {
    return (
      <div className='text-muted-foreground flex min-h-40 items-center justify-center gap-2 text-sm'>
        <LoaderCircle className='animate-spin' aria-hidden='true' />
        {t('Loading...')}
      </div>
    )
  }

  if (optionsQuery.isError || !policy) {
    return (
      <Alert variant='destructive' className='max-w-2xl'>
        <CircleAlert aria-hidden='true' />
        <div className='space-y-2'>
          <AlertDescription>
            {t('imageStudio.admin.pricing.invalidPolicy')}
          </AlertDescription>
          <Button
            type='button'
            size='sm'
            variant='outline'
            onClick={() => void optionsQuery.refetch()}
          >
            <RotateCw aria-hidden='true' />
            {t('Retry')}
          </Button>
        </div>
      </Alert>
    )
  }

  return <ImagePricingEditor policy={policy} />
}
