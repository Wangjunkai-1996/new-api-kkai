import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { toIntlLocale } from '@/i18n/languages'
import { cn } from '@/lib/utils'

import type { RequestEvent } from './api'
import { requestStatusIcons } from './status'
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
    <ol aria-label={t('Request timeline')} className='request-timeline'>
      {[...props.events]
        .sort((a, b) => b.id - a.id)
        .map((event, index) => {
          const isHighlighted = event.id === props.highlightedEventId
          const StatusIcon = requestStatusIcons[event.status]
          return (
            <li
              key={event.id}
              id={`request-event-${event.id}`}
              ref={isHighlighted ? highlighted : undefined}
              tabIndex={isHighlighted ? -1 : undefined}
              aria-current={isHighlighted ? 'step' : undefined}
              className={cn(
                'request-event relative grid grid-cols-[24px_minmax(0,1fr)] gap-2.5 pb-6 outline-none last:pb-0',
                isHighlighted &&
                  'bg-primary/5 rounded-lg ring-2 ring-primary/30 ring-offset-4 ring-offset-background'
              )}
            >
              <span
                aria-hidden='true'
                data-status={event.status}
                className='request-status-tone request-event-node relative z-10 grid size-6 place-items-center rounded-full'
              >
                <StatusIcon className='size-3' />
              </span>
              <div className='min-w-0'>
                <div className='mb-2.5 flex min-h-6 flex-wrap items-center gap-x-2.5 gap-y-1'>
                  <RequestStatusBadge status={event.status} />
                  {index === 0 && event.feedback && (
                    <span
                      data-status={event.status}
                      className='request-status-tone text-[11px] font-medium text-[var(--request-status-color)]'
                    >
                      {t('Latest progress')}
                    </span>
                  )}
                  <time
                    className='text-muted-foreground w-full text-[11px] sm:ml-auto sm:w-auto'
                    dateTime={new Date(event.created_at * 1000).toISOString()}
                  >
                    {new Date(event.created_at * 1000).toLocaleString(locale)}
                  </time>
                </div>
                {event.feedback ? (
                  <div
                    data-status={event.status}
                    className={cn(
                      'request-status-tone bg-card rounded-[9px] border p-3.5 sm:px-4',
                      index === 0 && 'request-latest-feedback'
                    )}
                  >
                    <p className='text-foreground/90 text-[13px] leading-[1.85] [overflow-wrap:anywhere] whitespace-pre-wrap'>
                      {event.feedback}
                    </p>
                    <p className='text-muted-foreground mt-3 text-[11px]'>
                      {t('Official feedback')}
                    </p>
                  </div>
                ) : (
                  <p className='text-muted-foreground text-xs leading-relaxed'>
                    {t('Request submitted')}
                  </p>
                )}
              </div>
            </li>
          )
        })}
    </ol>
  )
}
