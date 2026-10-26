/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuota } from '@/lib/format'

import { membershipEmblems } from '../lib'
import type { MembershipOrder, MembershipSelf } from '../types'

import './success-dialog.css'

const sparks = Array.from({ length: 12 }, (_, index) => {
  const angle = (index * Math.PI) / 6
  return {
    x: `${Math.round(Math.cos(angle) * 108)}px`,
    y: `${Math.round(Math.sin(angle) * 83)}px`,
  }
})

export function MembershipSuccessDialog(props: {
  order: MembershipOrder
  membership: MembershipSelf
  onClose: () => void
  onViewMembership: () => void
}) {
  const { t, i18n } = useTranslation()
  const account = props.membership.account
  const availableQuota = account
    ? Math.max(
        0,
        account.amount_total - account.amount_used - account.amount_reserved
      )
    : 0
  const [displayQuota, setDisplayQuota] = useState(0)
  const reducedMotion =
    typeof window !== 'undefined' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches

  useEffect(() => {
    if (reducedMotion) return

    let frame = 0
    const start = performance.now() + 500
    const count = (now: number) => {
      const progress = Math.max(0, Math.min(1, (now - start) / 850))
      setDisplayQuota(availableQuota * (1 - (1 - progress) ** 3))
      if (progress < 1) frame = requestAnimationFrame(count)
    }
    frame = requestAnimationFrame(count)
    return () => cancelAnimationFrame(frame)
  }, [availableQuota, reducedMotion])

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={
        <span className='block'>
          <span className='membership-success-stage' aria-hidden='true'>
            <span className='membership-success-ring' />
            <span className='membership-success-ring membership-success-ring-late' />
            {sparks.map((spark) => (
              <span
                key={`${spark.x}-${spark.y}`}
                className='membership-success-spark'
                style={
                  {
                    '--membership-spark-x': spark.x,
                    '--membership-spark-y': spark.y,
                  } as React.CSSProperties
                }
              />
            ))}
            <img
              src={membershipEmblems[props.membership.tier?.style ?? 'gold']}
              width={156}
              height={156}
              alt=''
              className='membership-success-emblem'
            />
          </span>
          <span className='block'>{t('Membership activated')}</span>
        </span>
      }
      description={t('Your membership benefits are active.')}
      contentClassName={`membership-success-dialog membership-success-${props.membership.tier?.style ?? 'gold'} rounded-2xl sm:max-w-[28rem] sm:p-7`}
      headerClassName='membership-success-copy text-center'
      descriptionClassName='text-center'
      showCloseButton={false}
    >
      <div className='text-center'>
        <p className='membership-success-tier text-xl font-semibold'>
          {props.order.tier_name}
        </p>
        <dl className='membership-success-bill bg-muted/40 mt-5 space-y-3 rounded-lg border p-4 text-left text-sm'>
          <div className='flex justify-between gap-3'>
            <dt className='text-muted-foreground'>{t('Included quota')}</dt>
            <dd className='font-medium tabular-nums'>
              {formatQuota(props.order.quota)}
            </dd>
          </div>
          {account && (
            <>
              <div className='flex justify-between gap-3 border-t pt-3'>
                <dt className='text-muted-foreground'>{t('Available now')}</dt>
                <dd className='font-semibold tabular-nums'>
                  <span aria-hidden='true'>
                    {formatQuota(reducedMotion ? availableQuota : displayQuota)}
                  </span>
                  <span className='sr-only'>{formatQuota(availableQuota)}</span>
                </dd>
              </div>
              <div className='flex justify-between gap-3'>
                <dt className='text-muted-foreground'>{t('Expires at')}</dt>
                <dd className='font-medium tabular-nums'>
                  {new Date(account.expires_at * 1000).toLocaleString(
                    toIntlLocale(i18n.language)
                  )}
                </dd>
              </div>
            </>
          )}
        </dl>
        <Button
          className='membership-success-action mt-5 w-full'
          onClick={props.onViewMembership}
        >
          {t('View membership')}
        </Button>
      </div>
    </Dialog>
  )
}
