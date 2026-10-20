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
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { toIntlLocale } from '@/i18n/languages'

import type { ResellerWithdrawal } from './api'
import { withdrawalStatusLabel } from './withdrawal-status'

const timelineSchema = z.array(
  z.object({
    status: z.enum(['requested', 'approved', 'paid', 'rejected', 'failed']),
    actor_id: z.number().optional(),
    payment_ref: z.string().optional(),
    note: z.string().optional(),
    at: z.number(),
  })
)

export function WithdrawalTimeline(props: { withdrawal: ResellerWithdrawal }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage ?? i18n.language)
  let timeline: z.infer<typeof timelineSchema> = []
  try {
    const result = timelineSchema.safeParse(
      JSON.parse(props.withdrawal.timeline ?? '[]')
    )
    if (result.success) timeline = result.data
  } catch {
    // Older rows may not contain a serialized timeline. Their recorded update
    // timestamp remains useful and must not prevent rendering the history.
  }
  return (
    <details className='mt-2 text-sm'>
      <summary className='cursor-pointer'>{t('Timeline')}</summary>
      <ol className='mt-2 space-y-2'>
        {timeline.map((event) => (
          <li key={`${event.status}:${event.at}:${event.actor_id ?? 0}`}>
            <p>
              {withdrawalStatusLabel(event.status, t)} ·{' '}
              {new Date(event.at * 1000).toLocaleString(locale)}
            </p>
            {Boolean(event.actor_id) && (
              <p>
                {t('Reviewer')} #{event.actor_id}
              </p>
            )}
            {event.payment_ref && (
              <p className='break-all'>{event.payment_ref}</p>
            )}
            {event.note && <p className='whitespace-pre-wrap'>{event.note}</p>}
          </li>
        ))}
        {!timeline.length && (
          <li>
            {withdrawalStatusLabel(props.withdrawal.status, t)} ·{' '}
            {new Date(props.withdrawal.updated_at * 1000).toLocaleString(
              locale
            )}
          </li>
        )}
      </ol>
    </details>
  )
}
