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
import axios from 'axios'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'

import { WalletPaymentError, walletRequestRetryAt } from '../payment-flow-api'

function useWalletCooldown(error: unknown, errorUpdatedAt?: number) {
  const cooldown = useMemo(() => {
    const limited =
      (error instanceof WalletPaymentError && error.status === 429) ||
      (axios.isAxiosError(error) && error.response?.status === 429)
    return { limited, retryAt: walletRequestRetryAt(error, errorUpdatedAt) }
  }, [error, errorUpdatedAt])
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    if (!cooldown.retryAt || cooldown.retryAt <= now) return
    const timer = window.setTimeout(
      () => setNow(Date.now()),
      Math.min(2_147_483_647, Math.max(0, cooldown.retryAt - Date.now()))
    )
    return () => window.clearTimeout(timer)
  }, [cooldown.retryAt, now])
  return {
    ...cooldown,
    blocked: !!cooldown.retryAt && cooldown.retryAt > now,
  }
}

export function WalletRequestError(props: {
  error: unknown
  errorUpdatedAt?: number
  onRetry?: () => void
  retryLabel?: string
  retryDisabled?: boolean
  message?: string
}) {
  const { t, i18n } = useTranslation()
  const cooldown = useWalletCooldown(props.error, props.errorUpdatedAt)
  let message =
    props.message ??
    t(
      'We could not update this payment information. Your order is still saved.'
    )
  if (cooldown.limited) {
    const limitMessage = cooldown.retryAt
      ? t('Too many requests. Try again at {{time}}.', {
          time: new Date(cooldown.retryAt).toLocaleTimeString(
            toIntlLocale(i18n.language)
          ),
        })
      : t('Too many requests. Please wait before trying again.')
    message = props.message ? `${props.message} ${limitMessage}` : limitMessage
  }
  return (
    <div className='bg-muted rounded-lg border p-4 text-sm'>
      <p role='alert'>{message}</p>
      {props.onRetry && (
        <Button
          variant='link'
          disabled={cooldown.blocked || props.retryDisabled}
          onClick={props.onRetry}
        >
          {props.retryLabel ?? t('Retry')}
        </Button>
      )}
    </div>
  )
}
