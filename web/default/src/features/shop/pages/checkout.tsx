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
import { Link, useNavigate } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'

import { shopPath } from '../api'
import {
  MoneyText,
  ShopErrorNotice,
  ShopHeading,
  ShopLoading,
} from '../components/common'
import { useShopAction, useShopIdentity, useShopQuery } from '../hooks/use-shop'
import { beginRecovery, finishRecovery, readRecovery } from '../recovery'
import {
  configSchema,
  orderSchema,
  productSchema,
  recoverySchema,
  type Product,
} from '../schemas'
import { ProductTerms } from './catalog'

const checkoutFormSchema = z.object({
  accepted: z.boolean().refine(Boolean),
  values: z.record(z.string(), z.string().max(500)),
})

export function CheckoutPage(props: { productId: string }) {
  const identity = useShopIdentity()
  const product = useShopQuery(
    `/api/shop/products/${shopPath(props.productId)}`,
    productSchema,
    { public: true }
  )
  const config = useShopQuery('/api/shop/config', configSchema, {
    public: true,
  })
  if (product.isPending || config.isPending) return <ShopLoading />
  if (!product.data || !config.data) {
    return (
      <ShopErrorNotice
        error={product.error ?? config.error}
        retry={() => {
          void product.refetch()
          void config.refetch()
        }}
      />
    )
  }
  return (
    <CheckoutForm
      key={`${identity}:${product.data.id}:${product.data.version}:${product.data.terms_version}`}
      product={product.data}
      enabled={
        config.data.new_orders_enabled && config.data.payment_methods.length > 0
      }
    />
  )
}

function CheckoutForm(props: { product: Product; enabled: boolean }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const action = useShopAction()
  const product = props.product
  const [marker, setMarker] = useState(() =>
    readRecovery('create-order', product.id)
  )
  const form = useForm({
    resolver: zodResolver(checkoutFormSchema),
    defaultValues: { accepted: false, values: {} as Record<string, string> },
  })
  useEffect(() => {
    const hide = () => {
      if (document.hidden) form.reset()
    }
    const clear = () => form.reset()
    document.addEventListener('visibilitychange', hide)
    window.addEventListener('pagehide', clear)
    return () => {
      form.reset()
      document.removeEventListener('visibilitychange', hide)
      window.removeEventListener('pagehide', clear)
    }
  }, [form])
  const openOrder = (orderNo: string) => {
    finishRecovery('create-order', product.id)
    void navigate({ to: '/shop/orders/$orderNo/pay', params: { orderNo } })
  }
  const recover = async () => {
    if (!marker) return
    const result = await action.run(
      `/api/shop/order-requests/${shopPath(marker.key)}`,
      recoverySchema,
      undefined,
      { read: true }
    )
    if (result?.status === 'recorded' && result.order_no) {
      openOrder(result.order_no)
    }
  }
  const submit = form.handleSubmit(async (values) => {
    if (!props.enabled || !product.can_purchase || !product.terms_version) {
      return
    }
    for (const field of product.buyer_input_schema) {
      if (field.required && !values.values[field.key]?.trim()) {
        form.setError(`values.${field.key}`, {
          message: t('This field is required'),
        })
        return
      }
    }
    const request = marker ?? beginRecovery('create-order', product.id)
    setMarker(request)
    const order = await action.run(
      '/api/shop/orders',
      orderSchema,
      {
        product_id: product.id,
        product_version: product.version,
        terms_version: product.terms_version,
        accepted_terms: true,
        buyer_input: values.values,
      },
      {
        key: request.key,
        onDefinitiveRejection: () => {
          // A stored marker may represent an earlier uncertain request.
          if (marker) return
          finishRecovery('create-order', product.id)
          setMarker(null)
        },
      }
    )
    if (order) {
      form.reset()
      openOrder(order.order_no)
    }
  })
  return (
    <>
      <ShopHeading
        eyebrow={t('Confirm your purchase')}
        title={t('One last look.')}
        description={t(
          'Review the product, delivery terms and price before creating your order.'
        )}
      />
      <div className='shop-columns'>
        <div className='shop-stack'>
          <ProductTerms product={product} />
          <form
            id='shop-checkout-form'
            onSubmit={submit}
            className='shop-panel'
          >
            <FieldGroup>
              {product.buyer_input_schema.map((field) => (
                <Field
                  key={field.key}
                  data-invalid={Boolean(
                    form.formState.errors.values?.[field.key]
                  )}
                >
                  <FieldLabel htmlFor={`buyer-${field.key}`}>
                    {field.label}
                  </FieldLabel>
                  <Input
                    id={`buyer-${field.key}`}
                    autoComplete='off'
                    maxLength={Math.min(field.max_length ?? 500, 500)}
                    type={field.key === 'contact_email' ? 'email' : 'text'}
                    {...form.register(`values.${field.key}`)}
                    aria-invalid={Boolean(
                      form.formState.errors.values?.[field.key]
                    )}
                  />
                  <FieldError>
                    {form.formState.errors.values?.[field.key]?.message}
                  </FieldError>
                </Field>
              ))}
              <Field
                orientation='horizontal'
                data-invalid={Boolean(form.formState.errors.accepted)}
              >
                <input
                  id='shop-accept'
                  type='checkbox'
                  className='accent-primary size-5'
                  {...form.register('accepted')}
                />
                <FieldLabel htmlFor='shop-accept'>
                  {t('I have read and agree to the product and refund terms.')}
                </FieldLabel>
              </Field>
              {form.formState.errors.accepted && (
                <FieldError>
                  {t('Please confirm the terms before continuing.')}
                </FieldError>
              )}
            </FieldGroup>
          </form>
          <ShopErrorNotice error={action.error} />
        </div>
        <aside className='shop-panel shop-receipt'>
          <h2>{t('Order summary')}</h2>
          <p className='font-medium'>{product.name}</p>
          <p className='shop-muted'>{product.specification}</p>
          <dl className='shop-detail-list'>
            <div>
              <dt>{t('Quantity')}</dt>
              <dd>1</dd>
            </div>
            <div>
              <dt>{t('Total')}</dt>
              <dd>
                <MoneyText money={product.price} />
              </dd>
            </div>
          </dl>
          {!props.enabled && (
            <Alert>
              <AlertDescription>
                {t('No payment method is currently available.')}
              </AlertDescription>
            </Alert>
          )}
          {marker && (
            <Alert>
              <AlertDescription>
                {t(
                  'The previous request is unresolved. Keep the original request and do not pay again.'
                )}
                <Button
                  variant='outline'
                  disabled={action.pending}
                  onClick={recover}
                >
                  {t('Check the previous request')}
                </Button>
              </AlertDescription>
            </Alert>
          )}
          <Button
            type='submit'
            form='shop-checkout-form'
            disabled={action.pending || !props.enabled || !product.can_purchase}
          >
            {action.pending ? t('Processing') : t('Confirm and create order')}
          </Button>
          <Link
            to='/shop/products/$productId'
            params={{ productId: product.id }}
          >
            {t('Back to product')}
          </Link>
        </aside>
      </div>
    </>
  )
}
