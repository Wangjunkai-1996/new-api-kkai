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
import { CircleAlert, LoaderCircle, RotateCw } from 'lucide-react'
import { useEffect, useMemo, useRef } from 'react'
import { useForm, type Control } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

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
import { Switch } from '@/components/ui/switch'
import { useSystemOptions } from '@/features/system-settings/hooks/use-system-options'
import { useUpdateOption } from '@/features/system-settings/hooks/use-update-option'
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

type PriceFieldProps = {
  control: Control<ImagePricingFormValues>
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

function ImagePricingForm(props: { policy: ImagePricingPolicy }) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const form = useForm<ImagePricingFormValues>({
    resolver: zodResolver(imagePricingFormSchema),
    defaultValues: getImagePricingFormValues(props.policy),
  })
  const isDirty = form.formState.isDirty
  const isDirtyRef = useRef(isDirty)
  isDirtyRef.current = isDirty

  // A background refetch must not replace edits that have not been saved.
  useEffect(() => {
    if (!isDirtyRef.current) {
      form.reset(getImagePricingFormValues(props.policy))
    }
  }, [form, props.policy])

  const submit = form.handleSubmit(async (values) => {
    const nextPolicy = updateImagePricingPolicy(props.policy, values)
    try {
      const response = await updateOption.mutateAsync({
        key: IMAGE_PRICING_OPTION_KEY,
        value: JSON.stringify(nextPolicy),
      })
      if (response.success) {
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
                  disabled={updateOption.isPending}
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
            disabled={updateOption.isPending}
          />
          <PriceField
            control={form.control}
            name='price2k'
            label={t('imageStudio.admin.pricing.price2k')}
            disabled={updateOption.isPending}
          />
          <PriceField
            control={form.control}
            name='price4k'
            label={t('imageStudio.admin.pricing.price4k')}
            disabled={updateOption.isPending}
          />
        </div>

        <div className='flex justify-end border-t pt-4'>
          <Button type='submit' disabled={!isDirty || updateOption.isPending}>
            {updateOption.isPending && (
              <LoaderCircle className='animate-spin' aria-hidden='true' />
            )}
            {t(updateOption.isPending ? 'Saving...' : 'Save')}
          </Button>
        </div>
      </form>
    </Form>
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

  return <ImagePricingForm policy={policy} />
}
