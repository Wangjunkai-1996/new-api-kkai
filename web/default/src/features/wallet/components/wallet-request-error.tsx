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

import { Button } from '@/components/ui/button'

import { WalletPaymentError } from '../payment-flow-api'

function useWalletCooldown(error: unknown): number {
  const retryAt =
    error instanceof WalletPaymentError ? error.retryAt : undefined
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    if (!retryAt || retryAt <= Date.now()) return
    const timer = window.setInterval(() => {
      const time = Date.now()
      setNow(time)
      if (time >= retryAt) window.clearInterval(timer)
    }, 1000)
    return () => window.clearInterval(timer)
  }, [retryAt])
  return retryAt ? Math.max(0, Math.ceil((retryAt - now) / 1000)) : 0
}

export function WalletRequestError(props: {
  error: unknown
  onRetry?: () => void
  retryLabel?: string
  message?: string
}) {
  const { t } = useTranslation()
  const seconds = useWalletCooldown(props.error)
  const limited =
    props.error instanceof WalletPaymentError && props.error.status === 429
  let message =
    props.message ??
    t(
      'We could not update this payment information. Your order is still saved.'
    )
  if (limited) {
    message =
      seconds > 0
        ? t('Too many requests. Try again in {{seconds}} seconds.', { seconds })
        : t('Too many requests. Please wait before trying again.')
  }
  return (
    <div role='alert' className='bg-muted rounded-lg border p-4 text-sm'>
      {message}
      {props.onRetry && (
        <Button variant='link' disabled={seconds > 0} onClick={props.onRetry}>
          {props.retryLabel ?? t('Retry')}
        </Button>
      )}
    </div>
  )
}
