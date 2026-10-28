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
import { useEffect, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { SecureVerificationDialog } from '@/features/auth/secure-verification/components/secure-verification-dialog'
import { useSecureVerification } from '@/features/auth/secure-verification/hooks/use-secure-verification'
import type { ShopSecurityProofScope } from '@/features/auth/secure-verification/types'
import { shopRequest } from '@/features/shop/api'
import { ShopErrorNotice, ShopTime } from '@/features/shop/components/common'
import {
  usePrivateResult,
  useShopIdentity,
} from '@/features/shop/hooks/use-shop'
import { revealSchema, type Reveal } from '@/features/shop/schemas'

export function AdminPrivateReveal(props: {
  path: string
  target: string
  version: string
  scope: ShopSecurityProofScope
  deliveryId?: string
}) {
  const { t } = useTranslation()
  const reasonId = useId()
  const [reason, setReason] = useState('')
  const identity = useShopIdentity()
  const verification = useSecureVerification()
  const result = usePrivateResult<Reveal>(
    `${props.target}:${props.version}:${props.deliveryId ?? ''}`
  )
  const { value: revealed, clear: clearResult } = result
  const cancelVerification = verification.cancel
  useEffect(() => {
    const hide = () => {
      if (document.hidden) {
        setReason('')
        cancelVerification()
      }
    }
    const clear = () => {
      setReason('')
      cancelVerification()
    }
    document.addEventListener('visibilitychange', hide)
    window.addEventListener('pagehide', clear)
    return () => {
      clear()
      window.removeEventListener('pagehide', clear)
      document.removeEventListener('visibilitychange', hide)
    }
  }, [
    identity,
    props.target,
    props.version,
    props.deliveryId,
    cancelVerification,
  ])
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
  return (
    <div className='shop-stack'>
      <form
        className='shop-actions'
        onSubmit={async (event) => {
          event.preventDefault()
          const proof = await verification.requestVerification({
            scope: props.scope,
            context: {
              action: 'reveal',
              target_id: props.target,
              expected_version: props.version,
              details: {
                reason,
                ...(props.deliveryId ? { delivery_id: props.deliveryId } : {}),
              },
            },
            title: t('View protected details'),
          })
          if (!proof) return
          await result.read((signal) =>
            shopRequest(props.path, revealSchema, {
              method: 'POST',
              body: { expected_version: props.version, reason },
              proof: proof.proof_token,
              signal,
            })
          )
          setReason('')
        }}
      >
        <Field className='min-w-0 flex-1'>
          <FieldLabel htmlFor={reasonId}>{t('Reason')}</FieldLabel>
          <Input
            id={reasonId}
            required
            maxLength={2000}
            autoComplete='off'
            value={reason}
            onChange={(event) => setReason(event.target.value)}
          />
        </Field>
        <Button disabled={result.pending || verification.isActive}>
          {t('View protected details')}
        </Button>
      </form>
      {revealed && (
        <div className='shop-panel'>
          <p>
            {t('Viewing period ends at')}{' '}
            <ShopTime value={revealed.view_expires_at} />
          </p>
          {revealed.fields.map((field) => (
            <div key={field.key}>
              <strong>{field.label}</strong>
              <p className='shop-secret'>{field.value}</p>
            </div>
          ))}
          <Button variant='outline' onClick={clearResult}>
            {t('Hide details')}
          </Button>
        </div>
      )}
      <ShopErrorNotice error={result.error} />
      <SecureVerificationDialog {...verification.dialogProps} />
    </div>
  )
}
