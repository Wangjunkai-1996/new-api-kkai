import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { toIntlLocale } from '@/i18n/languages'
import { cn } from '@/lib/utils'

import type { RequestEvent } from './api'
import { RequestStatusBadge } from './status-badge'

export function RequestTimeline(props: {
  events: RequestEvent[]
  highlightedEventId?: number
}) {
  const { t, i18n } = useTranslation()
  const highlighted = useRef<HTMLLIElement>(null)
  const locale = toIntlLocale(i18n.resolvedLanguage ?? i18n.language)
  const highlightedEventExists = props.events.some(
    (event) => event.id === props.highlightedEventId
  )

  useEffect(() => {
    if (!highlightedEventExists) return
    highlighted.current?.scrollIntoView({
      block: 'center',
      behavior: 'instant',
    })
    highlighted.current?.focus({ preventScroll: true })
  }, [props.highlightedEventId, highlightedEventExists])

  return (
    <ol aria-label={t('Request timeline')} className='flex flex-col gap-4'>
      {[...props.events]
        .sort((a, b) => b.id - a.id)
        .map((event) => {
          const isHighlighted = event.id === props.highlightedEventId
          return (
            <li
              key={event.id}
              id={`request-event-${event.id}`}
              ref={isHighlighted ? highlighted : undefined}
              tabIndex={isHighlighted ? -1 : undefined}
              aria-current={isHighlighted ? 'step' : undefined}
              className={cn(
                'border-border relative ml-1 border-l-2 py-1 pl-5 outline-none',
                isHighlighted &&
                  'bg-primary/5 border-primary rounded-r-lg ring-2 ring-primary/30'
              )}
            >
              <span
                aria-hidden='true'
                className='bg-background border-primary absolute top-2.5 -left-[5px] size-2 rounded-full border-2'
              />
              <div className='flex flex-wrap items-center gap-2'>
                <RequestStatusBadge status={event.status} />
                <time
                  className='text-muted-foreground text-xs'
                  dateTime={new Date(event.created_at * 1000).toISOString()}
                >
                  {new Date(event.created_at * 1000).toLocaleString(locale)}
                </time>
              </div>
              {event.feedback ? (
                <p className='mt-3 text-sm leading-relaxed [overflow-wrap:anywhere] break-words whitespace-pre-wrap'>
                  {event.feedback}
                </p>
              ) : (
                <p className='text-muted-foreground mt-3 text-sm'>
                  {t('Request submitted')}
                </p>
              )}
            </li>
          )
        })}
    </ol>
  )
}
