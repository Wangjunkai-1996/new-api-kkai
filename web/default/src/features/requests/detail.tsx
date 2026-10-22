import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { Main } from '@/components/layout/components/main'
import { buttonVariants } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { toIntlLocale } from '@/i18n/languages'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import {
  getRequest,
  isRequestTicketUnavailable,
  markRequestNotificationRead,
  requestQueryKey,
  type RequestDetail,
} from './api'
import successImage from './assets/request-success.webp'
import { requestStatusDescription } from './status'
import { RequestStatusBadge } from './status-badge'
import { RequestTimeline } from './timeline'

import '@/styles/requests.css'

export function RequestDetailPage(props: {
  requestId: number
  eventId?: number
  submitted?: boolean
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage ?? i18n.language)
  const userId = useAuthStore((state) => state.auth.user?.id)
  const client = useQueryClient()
  const attempted = useRef(new Set<number>())
  const detailKey = [...requestQueryKey(userId, 'detail'), props.requestId]
  const query = useQuery({
    queryKey: detailKey,
    queryFn: () => getRequest(props.requestId),
    enabled: Boolean(userId),
    retry: false,
    meta: { errorToast: false },
  })
  const { mutate: markRead } = useMutation({
    mutationFn: async (ids: number[]) => {
      const results = await Promise.allSettled(
        ids.map(markRequestNotificationRead)
      )
      return { ids, results }
    },
    onSuccess: ({ ids, results }) => {
      const readIds = new Set(
        ids.filter((_, index) => results[index].status === 'fulfilled')
      )
      if (readIds.size > 0) {
        client.setQueryData<RequestDetail>(
          detailKey,
          (previous) =>
            previous && {
              ...previous,
              ticket: {
                ...previous.ticket,
                unread_count: previous.events.filter(
                  (event) => !event.read_at && !readIds.has(event.id)
                ).length,
              },
              events: previous.events.map((event) =>
                readIds.has(event.id)
                  ? { ...event, read_at: Math.floor(Date.now() / 1000) }
                  : event
              ),
            }
        )
        void client.invalidateQueries({
          queryKey: requestQueryKey(userId, 'list'),
        })
        void client.invalidateQueries({
          queryKey: requestQueryKey(userId, 'notifications'),
        })
      }
      const failure = results.find((result) => result.status === 'rejected')
      if (failure?.status === 'rejected') handleServerError(failure.reason)
    },
  })

  // Reading a detail acknowledges only the events actually fetched, including
  // when a newer status update arrives while this page remains open.
  useEffect(() => {
    if (!query.data || query.isError) return
    const ids = query.data.events
      .filter((event) => !event.read_at && !attempted.current.has(event.id))
      .map((event) => event.id)
    if (ids.length === 0) return
    ids.forEach((id) => attempted.current.add(id))
    markRead(ids)
  }, [query.data, query.isError, markRead])

  return (
    <Main className='request-workspace overflow-y-auto'>
      <div className='request-detail mx-auto flex w-full max-w-4xl flex-col gap-6 px-4 py-6 sm:px-8 sm:py-9'>
        <Link
          to='/requests'
          className={buttonVariants({
            variant: 'ghost',
            size: 'sm',
            className: 'w-fit -ml-2',
          })}
        >
          <ArrowLeft aria-hidden='true' />
          {t('Back to my requests')}
        </Link>
        {query.isPending && (
          <div className='space-y-4' aria-label={t('Loading...')}>
            <Skeleton className='h-32 w-full' />
            <Skeleton className='h-64 w-full' />
          </div>
        )}
        {query.isError && (
          <ErrorState
            title={t('Unable to load this request')}
            description={
              isRequestTicketUnavailable(query.error)
                ? t(
                    'Requests are temporarily unavailable. Please try again later.'
                  )
                : t('It may be unavailable, or you may not have access.')
            }
            onRetry={() => void query.refetch()}
          />
        )}
        {query.data && !query.isError && (
          <>
            {props.submitted && (
              <div
                className='request-success bg-primary/5 border-primary/15 flex items-center gap-4 rounded-2xl border p-4 sm:p-5'
                role='status'
              >
                <img
                  src={successImage}
                  alt=''
                  width={80}
                  height={74}
                  className='request-success-mascot h-[68px] w-[74px] shrink-0 object-contain'
                />
                <div>
                  <p className='font-semibold'>
                    {t('Your idea is on its way')}
                  </p>
                  <p className='text-muted-foreground mt-1 text-sm leading-relaxed'>
                    {t(
                      'Request received. We will let you know when there is an update.'
                    )}
                  </p>
                </div>
              </div>
            )}
            <header className='bg-card rounded-2xl border p-5 sm:p-6'>
              <div className='mb-3 flex flex-wrap items-center gap-3'>
                <span className='text-muted-foreground text-xs'>
                  #{query.data.ticket.id}
                </span>
                <RequestStatusBadge status={query.data.ticket.status} />
              </div>
              <h1 className='text-xl font-semibold tracking-tight [overflow-wrap:anywhere] sm:text-2xl'>
                {query.data.ticket.title}
              </h1>
              <p className='text-muted-foreground mt-3 text-sm leading-relaxed'>
                {requestStatusDescription(query.data.ticket.status, t)}
              </p>
            </header>
            <section
              className='bg-card rounded-2xl border p-5 sm:p-6'
              aria-labelledby='request-timeline-heading'
            >
              <div className='mb-6 flex flex-wrap items-baseline justify-between gap-2'>
                <h2 id='request-timeline-heading' className='font-semibold'>
                  {t('Request timeline')}
                </h2>
                <span className='text-muted-foreground text-xs'>
                  {t('Latest update first')}
                </span>
              </div>
              <RequestTimeline
                events={query.data.events}
                highlightedEventId={props.eventId}
              />
            </section>
            <section
              className='bg-card rounded-2xl border p-5 sm:p-6'
              aria-labelledby='request-original-heading'
            >
              <h2 id='request-original-heading' className='font-semibold'>
                {t('Original request')}
              </h2>
              <time
                className='text-muted-foreground mt-2 block text-xs'
                dateTime={new Date(
                  query.data.ticket.created_at * 1000
                ).toISOString()}
              >
                {new Date(query.data.ticket.created_at * 1000).toLocaleString(
                  locale
                )}
              </time>
              <p className='mt-4 text-sm leading-7 [overflow-wrap:anywhere] whitespace-pre-wrap'>
                {query.data.ticket.description}
              </p>
            </section>
          </>
        )}
      </div>
    </Main>
  )
}
