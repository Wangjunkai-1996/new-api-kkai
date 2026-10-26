import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ChevronLeft, ChevronRight, Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { Main } from '@/components/layout/components/main'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { toIntlLocale } from '@/i18n/languages'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import { getRequests, isRequestTicketUnavailable, requestQueryKey } from './api'
import emptyImage from './assets/request-empty.webp'
import welcomeImage from './assets/request-welcome.webp'
import { RequestComposeDialog } from './compose-dialog'
import { RequestStatusBadge } from './status-badge'

import '@/styles/requests.css'

type RequestFilter = '' | 'active' | 'completed' | 'rejected'

export function RequestsPage() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage ?? i18n.language)
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [filter, setFilter] = useState<RequestFilter>('')
  const [page, setPage] = useState(1)
  const [composing, setComposing] = useState(false)
  const filters: { value: RequestFilter; label: string }[] = [
    { value: '', label: t('All') },
    { value: 'active', label: t('In progress') },
    { value: 'completed', label: t('Completed') },
    { value: 'rejected', label: t('Rejected') },
  ]
  const query = useQuery({
    queryKey: [...requestQueryKey(userId, 'list'), filter, page],
    queryFn: () => getRequests({ status: filter, page }),
    enabled: Boolean(userId),
    retry: false,
    meta: { errorToast: false },
  })

  return (
    <Main className='request-workspace overflow-y-auto'>
      <div className='flex w-full flex-col gap-6 px-4 py-6 sm:px-[30px] sm:py-[30px]'>
        <header className='grid grid-cols-[minmax(0,1fr)_58px] items-center gap-x-3.5 gap-y-3 sm:min-h-[104px] sm:grid-cols-[minmax(0,1fr)_88px_auto] sm:gap-[18px]'>
          <div className='min-w-0'>
            <p className='text-muted-foreground mb-1.5 text-[11px] tracking-wide'>
              {t('Your ideas, our next step')}
            </p>
            <h1 className='text-[26px] leading-tight font-medium tracking-[-0.045em]'>
              {t('My requests')}
            </h1>
            <p className='text-muted-foreground mt-2 max-w-[220px] text-xs leading-relaxed sm:max-w-none sm:text-[13px]'>
              {t('Share your ideas. Follow every update here.')}
            </p>
          </div>
          <img
            src={welcomeImage}
            alt=''
            width={88}
            height={104}
            className='pointer-events-none h-[68px] w-[58px] object-contain select-none sm:h-[104px] sm:w-[88px]'
          />
          <Button
            size='sm'
            className='col-span-2 h-11 justify-self-start rounded-lg px-3.5 text-xs sm:col-span-1 sm:h-9'
            disabled={isRequestTicketUnavailable(query.error)}
            onClick={() => setComposing(true)}
          >
            <Plus aria-hidden='true' />
            {t('Submit a request')}
          </Button>
        </header>

        <div className='flex min-w-0 flex-col gap-4'>
          <div className='flex flex-wrap items-center justify-between gap-3'>
            <div
              className='flex max-w-full flex-wrap items-center gap-1'
              role='group'
              aria-label={t('Filter requests')}
            >
              {filters.map((option) => (
                <button
                  key={option.value}
                  type='button'
                  aria-pressed={filter === option.value}
                  className={cn(
                    'focus-visible:ring-ring rounded-md border border-transparent px-2.5 py-1.5 text-xs transition-colors outline-none focus-visible:ring-2',
                    filter === option.value
                      ? 'bg-card text-foreground border-border shadow-xs'
                      : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                  )}
                  onClick={() => {
                    setFilter(option.value)
                    setPage(1)
                  }}
                >
                  {option.label}
                </button>
              ))}
            </div>
            <span className='text-muted-foreground hidden text-[11px] sm:inline'>
              {t('Most recently updated first')}
            </span>
          </div>
          <div className='min-w-0'>
            {query.isPending && (
              <div className='flex flex-col gap-3' aria-label={t('Loading...')}>
                <Skeleton className='h-28 w-full' />
                <Skeleton className='h-28 w-full' />
                <Skeleton className='h-28 w-full' />
              </div>
            )}
            {query.isError && (
              <ErrorState
                title={t('Unable to load requests')}
                description={
                  isRequestTicketUnavailable(query.error)
                    ? t(
                        'Requests are temporarily unavailable. Please try again later.'
                      )
                    : t('Please try again. Your requests are still saved.')
                }
                onRetry={() => void query.refetch()}
              />
            )}
            {!query.isError && query.data?.items.length === 0 && (
              <Empty className='request-results bg-card min-h-80 rounded-[14px] border p-6 sm:p-10'>
                <EmptyHeader>
                  <img
                    src={emptyImage}
                    alt=''
                    width={180}
                    height={138}
                    className='mx-auto mb-3 h-[138px] w-[180px] object-contain'
                  />
                  <EmptyTitle>
                    {filter
                      ? t('No matching requests')
                      : t('Good ideas start here')}
                  </EmptyTitle>
                  <EmptyDescription>
                    {filter
                      ? t('Try another filter to find your request.')
                      : t(
                          'Tell us what would make your experience better. We will keep you posted.'
                        )}
                  </EmptyDescription>
                </EmptyHeader>
                <EmptyContent>
                  <Button
                    variant={filter ? 'outline' : 'default'}
                    className='h-10'
                    onClick={() => {
                      if (filter) {
                        setFilter('')
                        setPage(1)
                      } else setComposing(true)
                    }}
                  >
                    {filter ? t('Clear filters') : t('Share your first idea')}
                  </Button>
                </EmptyContent>
              </Empty>
            )}
            {query.data && !query.isError && query.data.items.length > 0 && (
              <div
                key={`${filter}-${page}`}
                className='request-results bg-card divide-y overflow-hidden rounded-[14px] border'
              >
                {query.data.items.map((ticket) => (
                  <Link
                    key={ticket.id}
                    to='/requests/$requestId'
                    params={{ requestId: String(ticket.id) }}
                    className='request-row group focus-visible:ring-primary grid grid-cols-1 items-center gap-3 px-4 py-4 outline-none focus-visible:ring-2 focus-visible:ring-inset sm:grid-cols-[minmax(0,1fr)_auto_14px] sm:gap-4 sm:px-5'
                  >
                    <div className='relative min-w-0 pl-3.5'>
                      {ticket.unread_count > 0 && (
                        <span
                          className='bg-primary absolute top-2 left-0 size-1.5 rounded-full'
                          role='img'
                          aria-label={t('Unread updates')}
                        />
                      )}
                      <h2 className='text-[13px] leading-normal font-medium [overflow-wrap:anywhere] sm:text-sm'>
                        {ticket.title}
                      </h2>
                      <p className='text-muted-foreground mt-1 truncate text-xs'>
                        {ticket.latest_feedback || ticket.description}
                      </p>
                      <div className='text-muted-foreground mt-1 hidden flex-wrap items-center gap-x-2 gap-y-1 text-xs sm:flex'>
                        <span>REQ-{ticket.id}</span>
                        <span aria-hidden='true'>·</span>
                        <time
                          dateTime={new Date(
                            ticket.created_at * 1000
                          ).toISOString()}
                        >
                          {t('Submitted')}{' '}
                          {new Date(
                            ticket.created_at * 1000
                          ).toLocaleDateString(locale)}
                        </time>
                      </div>
                    </div>
                    <div className='flex flex-wrap items-center justify-between gap-2 pl-3.5 sm:max-w-64 sm:flex-col sm:items-end sm:gap-2.5 sm:pl-0'>
                      <RequestStatusBadge status={ticket.status} />
                      <time
                        className='text-muted-foreground text-xs'
                        dateTime={new Date(
                          ticket.updated_at * 1000
                        ).toISOString()}
                      >
                        {t('Updated {{date}}', {
                          date: new Date(
                            ticket.updated_at * 1000
                          ).toLocaleDateString(locale),
                        })}
                      </time>
                    </div>
                    <ChevronRight
                      className='request-row-chevron text-muted-foreground hidden size-3.5 shrink-0 sm:block'
                      aria-hidden='true'
                    />
                  </Link>
                ))}
              </div>
            )}
          </div>
          {query.data && !query.isError && (
            <p className='text-muted-foreground text-center text-xs sm:text-left'>
              {t('Total {{count}} records', { count: query.data.total })}
            </p>
          )}
        </div>
        {query.data && query.data.total > 20 && (
          <nav
            className='flex flex-wrap items-center justify-between gap-3'
            aria-label={t('Pagination')}
          >
            <span className='text-muted-foreground text-xs'>
              {t('Page {{page}} of {{total}}', {
                page,
                total: Math.ceil(query.data.total / 20),
              })}
            </span>
            <div className='flex gap-2'>
              <Button
                variant='outline'
                disabled={page === 1}
                onClick={() => setPage(page - 1)}
                aria-label={t('Previous page')}
              >
                <ChevronLeft aria-hidden='true' />
                {t('Previous')}
              </Button>
              <Button
                variant='outline'
                disabled={page * 20 >= query.data.total}
                onClick={() => setPage(page + 1)}
                aria-label={t('Next page')}
              >
                {t('Next')}
                <ChevronRight aria-hidden='true' />
              </Button>
            </div>
          </nav>
        )}
      </div>
      <RequestComposeDialog open={composing} onOpenChange={setComposing} />
    </Main>
  )
}
