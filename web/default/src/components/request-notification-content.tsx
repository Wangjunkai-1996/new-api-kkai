import { Link } from '@tanstack/react-router'
import { CheckCheck, ChevronRight, Inbox } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button, buttonVariants } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { ScrollArea } from '@/components/ui/scroll-area'
import type { RequestNotification } from '@/features/requests/api'
import { RequestStatusBadge } from '@/features/requests/status-badge'
import { formatDateTimeObject } from '@/lib/time'
import { cn } from '@/lib/utils'

export interface RequestUpdates {
  enabled: boolean
  items: RequestNotification[]
  unreadCount: number
  loading: boolean
  error: boolean
  markingAllRead: boolean
  onRead: (id: number) => void
  onMarkAllRead: () => void
  onRetry: () => void
}

interface RequestNotificationContentProps {
  updates: RequestUpdates
  onNavigate: () => void
}

export function RequestNotificationContent(
  props: RequestNotificationContentProps
) {
  const { t } = useTranslation()

  if (props.updates.loading) {
    return (
      <div
        className='text-muted-foreground flex min-h-48 items-center justify-center text-sm'
        role='status'
      >
        {t('Loading...')}
      </div>
    )
  }

  if (props.updates.error) {
    return (
      <div className='flex min-h-48 flex-col items-center justify-center gap-3 px-4 text-center'>
        <p className='text-muted-foreground text-sm'>
          {t('Unable to load request updates')}
        </p>
        <Button variant='outline' onClick={props.updates.onRetry}>
          {t('Retry')}
        </Button>
      </div>
    )
  }

  return (
    <div className='flex flex-col gap-2'>
      {props.updates.items.length === 0 ? (
        <Empty className='min-h-48 border-0 p-4'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <Inbox aria-hidden='true' />
            </EmptyMedia>
            <EmptyTitle>{t('No request updates yet')}</EmptyTitle>
            <EmptyDescription>
              {t('Updates to your requests will appear here.')}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <>
          <div className='flex justify-end'>
            <Button
              variant='ghost'
              size='sm'
              disabled={
                props.updates.unreadCount === 0 || props.updates.markingAllRead
              }
              onClick={props.updates.onMarkAllRead}
            >
              <CheckCheck aria-hidden='true' />
              {t('Mark all as read')}
            </Button>
          </div>
          <ScrollArea className='h-[min(52vh,28rem)]'>
            <ul className='space-y-1 pr-2'>
              {props.updates.items.map((item) => (
                <li key={item.id}>
                  <Link
                    to='/requests/$requestId'
                    params={{ requestId: String(item.ticket_id) }}
                    search={{ event: item.event_id }}
                    onClick={() => {
                      if (!item.read_at) props.updates.onRead(item.id)
                      props.onNavigate()
                    }}
                    className={cn(
                      'hover:bg-muted focus-visible:ring-ring flex gap-2.5 rounded-lg p-3 transition-colors duration-150 motion-reduce:transition-none focus-visible:ring-2 focus-visible:outline-none',
                      !item.read_at && 'bg-primary/5'
                    )}
                  >
                    <span
                      aria-hidden='true'
                      className={cn(
                        'mt-1.5 size-1.5 shrink-0 rounded-full',
                        !item.read_at && 'bg-primary'
                      )}
                    />
                    <div className='min-w-0 flex-1 space-y-1.5'>
                      <div className='flex flex-wrap items-center gap-2'>
                        <RequestStatusBadge status={item.status} />
                        {!item.read_at ? (
                          <span className='sr-only'>{t('Unread')}</span>
                        ) : null}
                      </div>
                      <p className='line-clamp-2 text-sm font-medium break-words'>
                        {item.title}
                      </p>
                      <p className='text-muted-foreground line-clamp-2 text-xs break-words'>
                        {item.feedback}
                      </p>
                      <time
                        className='text-muted-foreground block text-[11px]'
                        dateTime={new Date(
                          item.created_at * 1000
                        ).toISOString()}
                      >
                        {formatDateTimeObject(new Date(item.created_at * 1000))}
                      </time>
                    </div>
                    <ChevronRight
                      aria-hidden='true'
                      className='text-muted-foreground mt-1 size-3.5 shrink-0'
                    />
                  </Link>
                </li>
              ))}
            </ul>
          </ScrollArea>
        </>
      )}
      <Link
        to='/requests'
        onClick={props.onNavigate}
        className={buttonVariants({
          variant: 'ghost',
          size: 'sm',
          className: 'w-full',
        })}
      >
        {t('View all requests')}
        <ChevronRight aria-hidden='true' />
      </Link>
    </div>
  )
}
