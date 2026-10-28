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
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import type { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'

import { shopPath, shopRequest } from '../api'
import { usePrivateResult, useShopAction } from '../hooks/use-shop'
import { buyerInputSchema, orderSchema, type Order } from '../schemas'
import { ShopErrorNotice } from './common'

export function BuyerInput(props: { order: Order; admin?: boolean }) {
  const { t } = useTranslation()
  const order = props.order
  const action = useShopAction()
  const privateData = usePrivateResult<z.infer<typeof buyerInputSchema>>(
    `${order.order_no}:${order.version}`
  )
  const base = `${props.admin ? '/api/admin/shop' : '/api/shop'}/orders/${shopPath(order.order_no)}/buyer-input`
  const form = useForm<{ values: Record<string, string> }>({
    defaultValues: { values: {} },
  })
  useEffect(() => {
    form.reset({ values: privateData.value?.values ?? {} })
  }, [privateData.value, form])
  const editable =
    !props.admin && order.allowed_actions.includes('provide_input')
  return (
    <section className='shop-panel'>
      <h2>{t('Required information')}</h2>
      <p className='shop-muted'>
        {t(
          'Only provide the information requested for this order. Never send passwords or verification codes here.'
        )}
      </p>
      <Button
        variant='outline'
        disabled={privateData.pending}
        onClick={() =>
          privateData.read((signal) =>
            shopRequest(base, buyerInputSchema, { signal })
          )
        }
      >
        {t('View required information')}
      </Button>
      {privateData.value && (
        <form
          onSubmit={form.handleSubmit(async (values) => {
            const response = await action.run(
              base,
              orderSchema,
              { expected_version: order.version, values: values.values },
              { method: 'PUT' }
            )
            if (response) {
              privateData.clear()
              form.reset()
            }
          })}
        >
          <FieldGroup>
            {privateData.value.fields.map((field) => (
              <Field key={field.key}>
                <FieldLabel htmlFor={`input-${field.key}`}>
                  {field.label}
                </FieldLabel>
                <Input
                  id={`input-${field.key}`}
                  readOnly={!editable}
                  required={field.required}
                  maxLength={Math.min(field.max_length ?? 500, 500)}
                  autoComplete='off'
                  {...form.register(`values.${field.key}`)}
                />
              </Field>
            ))}
            <div className='shop-actions'>
              {editable && (
                <Button type='submit' disabled={action.pending}>
                  {t('Save information')}
                </Button>
              )}
              <Button
                type='button'
                variant='outline'
                onClick={privateData.clear}
              >
                {t('Hide details')}
              </Button>
            </div>
          </FieldGroup>
        </form>
      )}
      <ShopErrorNotice error={privateData.error ?? action.error} />
    </section>
  )
}
