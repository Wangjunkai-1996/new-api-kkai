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
import { Link } from '@tanstack/react-router'
import { QRCodeSVG } from 'qrcode.react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { z } from 'zod'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'

import { ShopError, shopIdentity, shopPath, shopRequest } from '../api'
import {
  MoneyText,
  OrderStatus,
  ShopErrorNotice,
  ShopHeading,
  ShopLoading,
  ShopTime,
} from '../components/common'
import { ShopConfirm } from '../components/confirm'
import {
  usePrivateResult,
  useShopAction,
  useShopIdentity,
  useShopQuery,
} from '../hooks/use-shop'
import { safeCheckoutUrl } from '../lib'
import { checkoutSchema, configSchema, orderSchema } from '../schemas'

export function PaymentPage(props: { orderNo: string }) {
  const identity = useShopIdentity()
  return (
    <PaymentSession
      key={`${identity}:${props.orderNo}`}
      orderNo={props.orderNo}
    />
  )
}

function PaymentSession(props: { orderNo: string }) {
  const { t } = useTranslation()
  const action = useShopAction()
  const identity = useShopIdentity()
  const [method, setMethod] = useState('')
  const [poll, setPoll] = useState(true)
  const base = `/api/shop/orders/${shopPath(props.orderNo)}`
  const order = useShopQuery(base, orderSchema, {
    poll: (data) =>
      poll &&
      data?.payment_state !== 'paid' &&
      data?.purchase_state !== 'closed'
        ? 5000
        : false,
  })
  const config = useShopQuery('/api/shop/config', configSchema, {
    public: true,
  })
  const checkout = usePrivateResult<z.infer<typeof checkoutSchema>>(
    props.orderNo
  )
  const intent = useRef<{
    key: string
    body: { payment_method: string; expected_version: string }
    committedOrUnknown: boolean
  } | null>(null)
  const paying = useRef(false)
  const [methodLocked, setMethodLocked] = useState(false)
  useEffect(() => {
    const timer = setTimeout(() => setPoll(false), 120_000)
    return () => clearTimeout(timer)
  }, [props.orderNo, identity])
  const clearCheckout = checkout.clear
  useEffect(() => {
    if (
      order.data?.payment_state === 'paid' ||
      order.data?.purchase_state === 'closed'
    ) {
      clearCheckout()
      intent.current = null
    }
  }, [order.data?.payment_state, order.data?.purchase_state, clearCheckout])
  useEffect(() => {
    const expiry = checkout.value?.checkout?.expires_at
    if (!expiry) return
    const server = checkout.value?.order.server_time
    const remaining =
      Date.parse(expiry) - Date.parse(server ?? new Date().toISOString())
    const timer = setTimeout(
      clearCheckout,
      Math.max(0, Math.min(remaining, 2147483647))
    )
    return () => clearTimeout(timer)
  }, [checkout.value, clearCheckout])
  if (order.isPending) return <ShopLoading />
  if (!order.data) {
    return (
      <ShopErrorNotice error={order.error} retry={() => void order.refetch()} />
    )
  }
  const data = order.data
  const canPay = data.allowed_actions.includes('pay')
  const pay = async () => {
    if (!method || !canPay || paying.current) return
    intent.current ??= {
      key: crypto.randomUUID(),
      body: { payment_method: method, expected_version: data.version },
      committedOrUnknown: false,
    }
    const request = intent.current
    paying.current = true
    setMethodLocked(true)
    const result = await checkout.read(async (signal) => {
      try {
        const response = await shopRequest(
          `${base}/payment-attempts`,
          checkoutSchema,
          {
            method: 'POST',
            body: request.body,
            key: request.key,
            signal,
          }
        )
        request.committedOrUnknown = true
        return response
      } catch (failure) {
        if (
          !request.committedOrUnknown &&
          failure instanceof ShopError &&
          failure.requestOutcome === 'not_committed' &&
          !signal.aborted &&
          identity === shopIdentity() &&
          intent.current === request
        ) {
          intent.current = null
          setMethodLocked(false)
        } else {
          request.committedOrUnknown = true
        }
        throw failure
      }
    })
    paying.current = false
    if (result) {
      if (['closed', 'failed'].includes(result.attempt.state)) {
        intent.current = null
        setMethodLocked(false)
      }
      void order.refetch()
    }
  }
  const entrance =
    data.payment_state === 'paid' || data.purchase_state === 'closed'
      ? null
      : checkout.value?.checkout
  const url = safeCheckoutUrl(entrance?.url)
  return (
    <>
      <ShopHeading
        eyebrow={t('Secure checkout')}
        title={t('Your order is ready.')}
        description={t(
          'Payment is confirmed by the payment provider. Returning to this page does not confirm payment.'
        )}
      />
      <div className='shop-columns'>
        <section className='shop-panel'>
          <OrderStatus order={data} />
          <p className='break-all'>{data.order_no}</p>
          <h2>{data.product_snapshot.name}</h2>
          <MoneyText money={data.total} className='shop-money text-3xl' />
          {data.payment_state === 'verifying' && !canPay && (
            <Alert>
              <AlertDescription>
                {t('Payment is being verified. Do not pay again.')}
              </AlertDescription>
            </Alert>
          )}
          {data.payment_state === 'paid' && (
            <Button
              nativeButton={false}
              render={
                <Link
                  to='/shop/orders/$orderNo'
                  params={{ orderNo: data.order_no }}
                />
              }
            >
              {t('View delivery details')}
            </Button>
          )}
          {canPay && (
            <>
              <Field>
                <FieldLabel htmlFor='shop-payment-method'>
                  {t('Payment method')}
                </FieldLabel>
                <NativeSelect
                  id='shop-payment-method'
                  disabled={methodLocked}
                  value={method}
                  onChange={(event) => {
                    clearCheckout()
                    setMethod(event.target.value)
                  }}
                >
                  <NativeSelectOption value=''>
                    {t('Select payment method')}
                  </NativeSelectOption>
                  {config.data?.payment_methods.map((item) => (
                    <NativeSelectOption key={item.id} value={item.id}>
                      {item.name}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
              </Field>
              <Button disabled={checkout.pending || !method} onClick={pay}>
                {t('Create or resume payment')}
              </Button>
            </>
          )}
          {entrance?.kind === 'qr' && entrance.qr_payload && (
            <div className='w-fit rounded-xl bg-white p-4 text-black'>
              <QRCodeSVG
                value={entrance.qr_payload}
                size={224}
                title={t('Payment QR code')}
              />
            </div>
          )}
          {entrance?.kind === 'redirect' && url && (
            <Button
              nativeButton={false}
              render={
                <a
                  href={url}
                  target='_blank'
                  rel='noopener noreferrer'
                  referrerPolicy='no-referrer'
                />
              }
            >
              {t('Open payment provider')}
            </Button>
          )}
          {entrance && (
            <p className='shop-muted'>
              {t('Payment link expires at')}{' '}
              <ShopTime value={entrance.expires_at} />
            </p>
          )}
          <ShopErrorNotice
            error={checkout.error ?? action.error ?? config.error}
          />
          <div className='shop-actions'>
            {data.allowed_actions.includes('reconcile') && (
              <Button
                variant='outline'
                disabled={action.pending}
                onClick={() =>
                  action.run(`${base}/reconcile`, orderSchema, {
                    expected_version: data.version,
                  })
                }
              >
                {t('Verify payment')}
              </Button>
            )}
            <Button variant='outline' onClick={() => void order.refetch()}>
              {t('Refresh')}
            </Button>
          </div>
        </section>
        <aside className='shop-panel shop-receipt'>
          <h2>{t('Keep this order')}</h2>
          <p className='shop-muted'>
            {t('If a request times out, check this order before paying again.')}
          </p>
          <p>
            {t('Reservation expires at')}{' '}
            <ShopTime value={data.reservation_expires_at} />
          </p>
          <Link to='/shop/orders/$orderNo' params={{ orderNo: data.order_no }}>
            {t('View order')}
          </Link>
          {data.allowed_actions.includes('close') && (
            <ShopConfirm
              label={t('End this purchase')}
              description={t(
                'This ends the local purchase. A payment already sent can still arrive and will be reviewed.'
              )}
              pending={action.pending}
              onConfirm={() =>
                action.run(`${base}/close`, orderSchema, {
                  expected_version: data.version,
                })
              }
            />
          )}
        </aside>
      </div>
    </>
  )
}
