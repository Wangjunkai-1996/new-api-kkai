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
import { useEffect, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import type { z } from 'zod'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { SecureVerificationDialog } from '@/features/auth/secure-verification/components/secure-verification-dialog'
import { useSecureVerification } from '@/features/auth/secure-verification/hooks/use-secure-verification'
import type {
  ShopSecurityProofScope,
  ShopVerificationContext,
} from '@/features/auth/secure-verification/types'
import { ShopError, shopIdentity } from '@/features/shop/api'
import { ShopErrorNotice } from '@/features/shop/components/common'
import { useShopAction, useShopIdentity } from '@/features/shop/hooks/use-shop'

export interface ActionField {
  name: string
  label: string
  type?: 'text' | 'textarea' | 'password' | 'datetime-local'
  optional?: boolean
  options?: { value: string; label: string }[]
  defaultValue?: string
  maxLength?: number
}
export interface AdminAction<T> {
  label: string
  description: string
  path: string
  schema: z.ZodType<T>
  fields?: ActionField[]
  disabled?: boolean
  body: (values: Record<string, string>) => unknown
  proof?: (
    values: Record<string, string>,
    key: string
  ) =>
    | Promise<{
        scope: ShopSecurityProofScope
        context: ShopVerificationContext
      }>
    | { scope: ShopSecurityProofScope; context: ShopVerificationContext }
  onSuccess?: (result: T) => void
  method?: 'POST' | 'PUT' | 'PATCH'
}

/** Deliberately contains only an operation form, never cached form variables. */
export function AdminActionDialog<T>(props: AdminAction<T>) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [validation, setValidation] = useState<ShopError | null>(null)
  const identity = useShopIdentity()
  const action = useShopAction()
  const verification = useSecureVerification()
  const generation = useRef(0)
  const binding = `${identity}:${props.path}`
  const currentBinding = useRef(binding)
  const form = useForm<Record<string, string>>()
  const close = () => {
    generation.current += 1
    setOpen(false)
    form.reset({})
    cancelVerification()
  }
  const cancelVerification = verification.cancel
  useEffect(() => {
    currentBinding.current = binding
    const clear = () => {
      generation.current += 1
      setOpen(false)
      form.reset({})
      cancelVerification()
    }
    const hide = () => {
      if (document.hidden) clear()
    }
    document.addEventListener('visibilitychange', hide)
    window.addEventListener('pagehide', clear)
    return () => {
      clear()
      document.removeEventListener('visibilitychange', hide)
      window.removeEventListener('pagehide', clear)
    }
  }, [binding, form, cancelVerification])
  return (
    <>
      <Button
        variant='outline'
        disabled={props.disabled || action.pending}
        onClick={() => {
          form.reset(
            Object.fromEntries(
              (props.fields ?? []).map((field) => [
                field.name,
                field.defaultValue ?? field.options?.[0]?.value ?? '',
              ])
            )
          )
          setOpen(true)
        }}
      >
        {props.label}
      </Button>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!next) close()
        }}
      >
        <DialogContent className='max-h-[85dvh] overflow-y-auto sm:max-w-xl'>
          <DialogHeader>
            <DialogTitle>{props.label}</DialogTitle>
            <DialogDescription>{props.description}</DialogDescription>
          </DialogHeader>
          <form
            onSubmit={(event) => {
              void form.handleSubmit(async (values) => {
                setValidation(null)
                try {
                  const body = props.body(values)
                  const mine = generation.current
                  const key = await action.prepare(
                    props.path,
                    body,
                    props.method
                  )
                  if (
                    mine !== generation.current ||
                    currentBinding.current !== binding ||
                    identity !== shopIdentity() ||
                    document.hidden
                  ) {
                    return
                  }
                  let proof: string | undefined
                  if (props.proof) {
                    const operation = await props.proof(values, key)
                    const result = await verification.requestVerification({
                      ...operation,
                      title: props.label,
                      description: props.description,
                    })
                    if (
                      !result ||
                      mine !== generation.current ||
                      currentBinding.current !== binding ||
                      identity !== shopIdentity() ||
                      document.hidden
                    ) {
                      return
                    }
                    proof = result.proof_token
                  }
                  const result = await action.run(
                    props.path,
                    props.schema,
                    body,
                    { key, proof, method: props.method }
                  )
                  if (result !== undefined) {
                    props.onSuccess?.(result)
                    close()
                  }
                } catch (failure) {
                  setValidation(
                    failure instanceof ShopError
                      ? failure
                      : new ShopError('SHOP_VALIDATION_FAILED')
                  )
                }
              })(event)
            }}
          >
            <FieldGroup>
              {props.fields?.map((field) => (
                <Field key={field.name}>
                  <FieldLabel htmlFor={`shop-action-${field.name}`}>
                    {field.label}
                  </FieldLabel>
                  {field.options && (
                    <NativeSelect
                      id={`shop-action-${field.name}`}
                      required={!field.optional}
                      {...form.register(field.name)}
                    >
                      {field.options.map((option) => (
                        <NativeSelectOption
                          key={option.value}
                          value={option.value}
                        >
                          {option.label}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  )}
                  {!field.options && field.type === 'textarea' && (
                    <Textarea
                      id={`shop-action-${field.name}`}
                      required={!field.optional}
                      maxLength={field.maxLength ?? 2000}
                      autoComplete='off'
                      {...form.register(field.name)}
                    />
                  )}
                  {!field.options && field.type !== 'textarea' && (
                    <Input
                      id={`shop-action-${field.name}`}
                      type={field.type ?? 'text'}
                      required={!field.optional}
                      maxLength={field.maxLength ?? 500}
                      autoComplete='off'
                      {...form.register(field.name)}
                    />
                  )}
                </Field>
              ))}
              <ShopErrorNotice error={validation ?? action.error} />
              <div className='flex justify-end gap-2'>
                <Button
                  type='button'
                  variant='outline'
                  disabled={action.pending}
                  onClick={close}
                >
                  {t('Cancel')}
                </Button>
                <Button
                  type='submit'
                  disabled={
                    action.pending ||
                    verification.isActive ||
                    form.formState.isSubmitting
                  }
                >
                  {props.label}
                </Button>
              </div>
            </FieldGroup>
          </form>
        </DialogContent>
      </Dialog>
      <SecureVerificationDialog {...verification.dialogProps} />
    </>
  )
}
