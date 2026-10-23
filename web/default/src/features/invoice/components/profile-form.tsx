import { useEffect } from 'react'
import { useWatch, type UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'

import { applyInvoiceProfileErrors } from '../lib/profile'
import type { InvoicePolicy, InvoiceProfile } from '../types'

const fields = [
  { name: 'title', label: 'Invoice title', max: 200 },
  { name: 'tax_id', label: 'Tax ID', max: 128 },
  { name: 'email', label: 'Invoice email', max: 254 },
  { name: 'address', label: 'Address', max: 500 },
  { name: 'phone', label: 'Phone', max: 64 },
  { name: 'bank_name', label: 'Bank name', max: 200 },
  { name: 'bank_account', label: 'Bank account', max: 128 },
] as const
export function InvoiceProfileFields(props: {
  form: UseFormReturn<InvoiceProfile>
  policy?: InvoicePolicy
  invoiceType?: string
  error?: unknown
}) {
  const { t } = useTranslation()
  const kind = useWatch({ control: props.form.control, name: 'kind' })
  useEffect(() => {
    applyInvoiceProfileErrors(props.form, props.error)
  }, [props.form, props.error])
  const required =
    props.policy?.invoice_types.find((type) => type.id === props.invoiceType)
      ?.required_fields || []
  return (
    <div className='grid min-w-0 gap-4'>
      <Field>
        <FieldLabel htmlFor='invoice-profile-kind'>
          {t('Title type')}
        </FieldLabel>
        <NativeSelect
          id='invoice-profile-kind'
          {...props.form.register('kind')}
        >
          <option value='company'>{t('Company')}</option>
          <option value='person'>{t('Individual')}</option>
        </NativeSelect>
      </Field>
      {fields
        .filter(
          (field) =>
            field.name === 'title' ||
            field.name === 'email' ||
            (field.name === 'tax_id' && kind === 'company') ||
            required.includes(field.name)
        )
        .map((field) => {
          const error = props.form.formState.errors[field.name]
          return (
            <Field key={field.name} data-invalid={!!error}>
              <FieldLabel htmlFor={`invoice-profile-${field.name}`}>
                {t(field.label)}
                {(field.name !== 'tax_id' || kind === 'company') && (
                  <span aria-hidden='true' className='text-destructive'>
                    {' '}
                    *
                  </span>
                )}
              </FieldLabel>
              <Input
                id={`invoice-profile-${field.name}`}
                type={field.name === 'email' ? 'email' : 'text'}
                maxLength={field.max}
                {...props.form.register(field.name)}
                aria-invalid={!!error}
                aria-describedby={
                  error ? `invoice-profile-${field.name}-error` : undefined
                }
                className='text-base md:text-sm'
              />
              {error && (
                <FieldError id={`invoice-profile-${field.name}-error`}>
                  {t(
                    field.name === 'email'
                      ? 'Enter a valid invoice email.'
                      : 'Check this invoice field.'
                  )}
                </FieldError>
              )}
            </Field>
          )
        })}
    </div>
  )
}
