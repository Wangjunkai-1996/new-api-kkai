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
import { Link } from '@tanstack/react-router'
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'

import { shopPath } from '../api'
import {
  ShopEmpty,
  ShopErrorNotice,
  ShopHeading,
  ShopLoading,
  ShopTime,
} from '../components/common'
import { useShopAction, useShopIdentity, useShopQuery } from '../hooks/use-shop'
import {
  aftersaleSchema,
  orderSchema,
  pageSchema,
  type Aftersale,
} from '../schemas'

const requestSchema = z.object({
  issue_type: z.enum(['unusable', 'mismatch', 'not_delivered', 'other']),
  desired_resolution: z.enum(['investigate', 'replace', 'refund']),
  description: z.string().trim().min(1).max(2000),
})

export function AftersalesPage(props: { orderNo: string }) {
  const { t } = useTranslation()
  const base = `/api/shop/orders/${shopPath(props.orderNo)}`
  const order = useShopQuery(base, orderSchema)
  const cases = useShopQuery(`${base}/aftersales`, pageSchema(aftersaleSchema))
  const action = useShopAction()
  const identity = useShopIdentity()
  const form = useForm({
    resolver: zodResolver(requestSchema),
    defaultValues: {
      issue_type: 'unusable' as const,
      desired_resolution: 'investigate' as const,
      description: '',
    },
  })
  useEffect(() => {
    const clear = () => form.reset()
    clear()
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
  }, [form, identity, props.orderNo])
  return (
    <>
      <ShopHeading
        eyebrow={t('We are here to help')}
        title={t('After-sales support')}
        description={props.orderNo}
      >
        <Link to='/shop/orders/$orderNo' params={{ orderNo: props.orderNo }}>
          {t('Back to order')}
        </Link>
      </ShopHeading>
      <div className='shop-columns'>
        <div className='shop-stack'>
          <ShopErrorNotice
            error={cases.error ?? order.error}
            retry={() => {
              void cases.refetch()
              void order.refetch()
            }}
          />
          {cases.isPending && <ShopLoading />}
          {cases.data?.items.length === 0 && <ShopEmpty />}
          {cases.data?.items.map((item) => (
            <AftersaleConversation key={item.id} item={item} />
          ))}
        </div>
        {order.data?.allowed_actions.includes('create_aftersale') && (
          <form
            className='shop-panel shop-receipt'
            onSubmit={form.handleSubmit(async (values) => {
              const response = await action.run(
                `${base}/aftersales`,
                aftersaleSchema,
                values
              )
              if (response) form.reset()
            })}
          >
            <h2>{t('Request support')}</h2>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor='shop-issue'>
                  {t('What happened?')}
                </FieldLabel>
                <NativeSelect id='shop-issue' {...form.register('issue_type')}>
                  {[
                    ['unusable', t('Unable to use')],
                    ['mismatch', t('Not as described')],
                    ['not_delivered', t('Not delivered')],
                    ['other', t('Other')],
                  ].map(([value, label]) => (
                    <NativeSelectOption key={value} value={value}>
                      {label}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
              </Field>
              <Field>
                <FieldLabel htmlFor='shop-resolution'>
                  {t('Preferred resolution')}
                </FieldLabel>
                <NativeSelect
                  id='shop-resolution'
                  {...form.register('desired_resolution')}
                >
                  {[
                    ['investigate', t('Please investigate')],
                    ['replace', t('Replacement')],
                    ['refund', t('Refund')],
                  ].map(([value, label]) => (
                    <NativeSelectOption key={value} value={value}>
                      {label}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
              </Field>
              <Field>
                <FieldLabel htmlFor='shop-description'>
                  {t('Describe the issue')}
                </FieldLabel>
                <Textarea
                  id='shop-description'
                  required
                  maxLength={2000}
                  {...form.register('description')}
                />
              </Field>
              <p className='shop-muted text-sm'>
                {t(
                  'Do not include passwords, card codes or verification codes in messages.'
                )}
              </p>
              <Button disabled={action.pending} type='submit'>
                {t('Submit request')}
              </Button>
              <ShopErrorNotice error={action.error} />
            </FieldGroup>
          </form>
        )}
      </div>
    </>
  )
}

export function AftersaleConversation(props: {
  item: Aftersale
  admin?: boolean
}) {
  const { t } = useTranslation()
  const action = useShopAction()
  const identity = useShopIdentity()
  const form = useForm({ defaultValues: { body: '', visibility: 'public' } })
  useEffect(() => {
    const clear = () => form.reset()
    clear()
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
  }, [form, identity, props.item.id])
  const detail = useShopQuery(
    `${props.admin ? '/api/admin/shop' : '/api/shop'}/aftersales/${shopPath(props.item.id)}`,
    aftersaleSchema,
    { enabled: !props.admin }
  )
  const item = detail.data ?? props.item
  return (
    <section className='shop-panel'>
      <div className='shop-actions justify-between'>
        <h2>{t('Support request')}</h2>
        <small>{item.id}</small>
      </div>
      <ShopErrorNotice error={detail.error} />
      <p>{item.description}</p>
      <small className='shop-muted'>
        <ShopTime value={item.created_at} />
      </small>
      {item.messages.map((message) => (
        <div key={message.id} className='bg-muted rounded-lg p-3'>
          <p className='whitespace-pre-wrap'>{message.body}</p>
          <small>
            <ShopTime value={message.created_at} />
          </small>
        </div>
      ))}
      {item.allowed_actions.includes('message') && (
        <form
          onSubmit={form.handleSubmit(async (values) => {
            const response = await action.run(
              `${props.admin ? '/api/admin/shop' : '/api/shop'}/aftersales/${shopPath(item.id)}/messages`,
              aftersaleSchema,
              {
                expected_version: item.version,
                body: values.body,
                ...(props.admin ? { visibility: values.visibility } : {}),
              }
            )
            if (response) form.reset()
          })}
        >
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor={`message-${item.id}`}>
                {t('Add information')}
              </FieldLabel>
              <Textarea
                id={`message-${item.id}`}
                maxLength={2000}
                required
                {...form.register('body')}
              />
            </Field>
            {props.admin && (
              <Field>
                <FieldLabel htmlFor={`visibility-${item.id}`}>
                  {t('Visibility')}
                </FieldLabel>
                <NativeSelect
                  id={`visibility-${item.id}`}
                  {...form.register('visibility')}
                >
                  <NativeSelectOption value='public'>
                    {t('Visible to buyer')}
                  </NativeSelectOption>
                  <NativeSelectOption value='internal'>
                    {t('Internal only')}
                  </NativeSelectOption>
                </NativeSelect>
              </Field>
            )}
            <Button type='submit' disabled={action.pending}>
              {t('Send message')}
            </Button>
          </FieldGroup>
        </form>
      )}
      <ShopErrorNotice error={action.error} />
    </section>
  )
}
