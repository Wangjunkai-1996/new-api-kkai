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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toIntlLocale } from '@/i18n/languages'
import { useAuthStore } from '@/stores/auth-store'

import { getRequests, requestQueryKey } from './api'
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
  const query = useQuery({
    queryKey: [...requestQueryKey(userId, 'list'), filter, page],
    queryFn: () => getRequests({ status: filter, page }),
    enabled: Boolean(userId),
  })

  return (
    <Main className='request-workspace overflow-y-auto'>
      <div className='mx-auto flex w-full max-w-5xl flex-col gap-6 px-4 py-6 sm:px-8 sm:py-9'>
        <header className='grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-4 sm:grid-cols-[auto_minmax(0,1fr)_auto]'>
          <img
            src={welcomeImage}
            alt=''
            width={84}
            height={76}
            className='col-start-2 row-start-1 h-[58px] w-[64px] object-contain sm:col-start-1 sm:h-[76px] sm:w-[84px]'
          />
          <div className='col-start-1 row-start-1 min-w-0 sm:col-start-2'>
            <p className='text-primary mb-1 text-xs font-medium'>
              {t('Your ideas, our next step')}
            </p>
            <h1 className='text-2xl font-semibold tracking-tight'>
              {t('My requests')}
            </h1>
            <p className='text-muted-foreground mt-1.5 text-sm leading-relaxed'>
              {t('Share your ideas. Follow every update here.')}
            </p>
          </div>
          <Button
            className='col-span-2 h-11 sm:col-span-1 sm:col-start-3 sm:h-9'
            onClick={() => setComposing(true)}
          >
            <Plus aria-hidden='true' />
            {t('Submit a request')}
          </Button>
        </header>

        <Tabs
          value={filter || 'all'}
          onValueChange={(value) => {
            setFilter(value === 'all' ? '' : (value as RequestFilter))
            setPage(1)
          }}
          className='gap-4'
        >
          <div className='flex flex-wrap items-center justify-between gap-3'>
            <TabsList className='max-w-full' aria-label={t('Filter requests')}>
              <TabsTrigger value='all'>{t('All')}</TabsTrigger>
              <TabsTrigger value='active'>{t('In progress')}</TabsTrigger>
              <TabsTrigger value='completed'>{t('Completed')}</TabsTrigger>
              <TabsTrigger value='rejected'>{t('Rejected')}</TabsTrigger>
            </TabsList>
            <span className='text-muted-foreground text-xs'>
              {t('Most recently updated first')}
            </span>
          </div>
          <TabsContent value={filter || 'all'} className='min-w-0'>
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
                description={t(
                  'Please try again. Your requests are still saved.'
                )}
                onRetry={() => void query.refetch()}
              />
            )}
            {query.data?.items.length === 0 && (
              <Empty className='request-results bg-card min-h-80 rounded-2xl border p-6 sm:p-10'>
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
            {query.data && query.data.items.length > 0 && (
              <div
                key={`${filter}-${page}`}
                className='request-results bg-card divide-y overflow-hidden rounded-2xl border'
              >
                {query.data.items.map((ticket) => (
                  <Link
                    key={ticket.id}
                    to='/requests/$requestId'
                    params={{ requestId: String(ticket.id) }}
                    className='request-row group focus-visible:ring-primary flex items-center gap-3 px-4 py-5 outline-none focus-visible:ring-2 focus-visible:ring-inset sm:px-6'
                  >
                    <div className='flex min-w-0 flex-1 flex-col gap-2.5'>
                      <div className='flex flex-wrap items-center gap-x-3 gap-y-2'>
                        <h2 className='text-sm font-semibold [overflow-wrap:anywhere]'>
                          {ticket.title}
                        </h2>
                        <RequestStatusBadge status={ticket.status} />
                        {ticket.unread_count > 0 && (
                          <span
                            className='bg-primary size-1.5 shrink-0 rounded-full'
                            role='img'
                            aria-label={t('Unread updates')}
                          />
                        )}
                      </div>
                      <p className='text-muted-foreground line-clamp-2 text-sm [overflow-wrap:anywhere]'>
                        {ticket.latest_feedback || ticket.description}
                      </p>
                      <div className='text-muted-foreground flex flex-wrap gap-x-3 gap-y-1 text-xs'>
                        <span>#{ticket.id}</span>
                        <time
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
                    </div>
                    <ChevronRight
                      className='request-row-chevron text-muted-foreground size-4 shrink-0'
                      aria-hidden='true'
                    />
                  </Link>
                ))}
              </div>
            )}
          </TabsContent>
        </Tabs>
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
