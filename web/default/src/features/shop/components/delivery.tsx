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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'

import { shopPath, shopRequest } from '../api'
import { usePrivateResult } from '../hooks/use-shop'
import { revealSchema, type Order, type Reveal } from '../schemas'
import { ShopErrorNotice, ShopTime } from './common'

export function DeliveryDetails(props: { order: Order }) {
  const { t } = useTranslation()
  const order = props.order
  const delivery = order.current_delivery
  const result = usePrivateResult<Reveal>(
    `${order.order_no}:${order.version}:${delivery?.id}:${delivery?.version}`
  )
  const [copyNotice, setCopyNotice] = useState('')
  const allowed = Boolean(
    delivery?.can_reveal && order.allowed_actions.includes('reveal')
  )
  const { value: revealed, clear: clearResult } = result
  useEffect(() => {
    if (!revealed) return
    const remaining =
      Date.parse(revealed.view_expires_at) - Date.parse(revealed.server_time)
    if (remaining <= 0) {
      clearResult()
      return
    }
    const timer = setTimeout(clearResult, Math.min(remaining, 2147483647))
    return () => clearTimeout(timer)
  }, [revealed, clearResult])
  useEffect(() => {
    if (!allowed) clearResult()
  }, [allowed, clearResult])
  if (!delivery) return null
  const reveal = async (field?: string) => {
    if (!allowed) return
    setCopyNotice('')
    const value = await result.read((signal) =>
      shopRequest(
        `/api/shop/orders/${shopPath(order.order_no)}/reveal`,
        revealSchema,
        {
          method: 'POST',
          body: {
            delivery_id: delivery.id,
            intent: field ? 'copy' : 'view',
            ...(field ? { field_key: field } : {}),
          },
          signal,
        }
      )
    )
    if (!value) return
    if (Date.parse(value.view_expires_at) <= Date.parse(value.server_time)) {
      clearResult()
      return
    }
    if (field) {
      const selected = value.fields.find((entry) => entry.key === field)
      if (!selected) {
        clearResult()
        return
      }
      try {
        await navigator.clipboard.writeText(selected.value)
        setCopyNotice(t('Copied'))
      } catch {
        setCopyNotice(
          t(
            'Copy was not completed. You can select the visible text to copy it manually.'
          )
        )
      }
    }
  }
  return (
    <section className='shop-panel'>
      <h2>{t('Your delivery')}</h2>
      <p className='shop-muted'>
        {t('Viewing period ends at')}{' '}
        <ShopTime value={delivery.view_expires_at} />
      </p>
      <p className='whitespace-pre-wrap'>{delivery.instructions}</p>
      {!allowed && (
        <Alert>
          <AlertDescription>
            {t(
              'Delivery details are temporarily unavailable. Check the order for the next step.'
            )}
          </AlertDescription>
        </Alert>
      )}
      {delivery.masked_fields.map((field) => (
        <div key={field.key}>
          <div className='shop-actions justify-between'>
            <span className='font-medium'>{field.label}</span>
            {allowed && (
              <Button
                variant='outline'
                disabled={result.pending}
                onClick={() => reveal(field.key)}
              >
                {t('Copy')}
              </Button>
            )}
          </div>
          <p className='shop-secret'>
            {revealed?.fields.find((value) => value.key === field.key)?.value ??
              field.masked_value ??
              '••••••••'}
          </p>
        </div>
      ))}
      <div className='shop-actions'>
        {allowed && (
          <Button disabled={result.pending} onClick={() => reveal()}>
            {t('View delivery details')}
          </Button>
        )}
        {revealed && (
          <Button variant='outline' onClick={clearResult}>
            {t('Hide details')}
          </Button>
        )}
      </div>
      {copyNotice && <p role='status'>{copyNotice}</p>}
      <ShopErrorNotice error={result.error} />
      <p className='shop-muted text-sm'>
        {t(
          'Details are hidden when you leave this page. Information already copied or viewed cannot be recalled.'
        )}
      </p>
    </section>
  )
}
