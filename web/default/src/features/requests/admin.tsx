import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldLabel } from '@/components/ui/field'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { toIntlLocale } from '@/i18n/languages'
import { useAuthStore } from '@/stores/auth-store'

import { AdminRequestDetail } from './admin-detail'
import { getAdminRequests, requestQueryKey, type RequestStatus } from './api'
import { REQUEST_STATUSES, requestStatusLabel } from './status'
import { RequestStatusBadge } from './status-badge'

export function RequestAdmin() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage ?? i18n.language)
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState<RequestStatus | ''>('')
  const [requestId, setRequestId] = useState<number | null>(null)
  const pageSize = 20
  const query = useQuery({
    queryKey: [...requestQueryKey(userId, 'admin'), page, status],
    queryFn: () =>
      getAdminRequests({ page, pageSize, status: status || undefined }),
  })

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Request management')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          type='button'
          variant='outline'
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-5'>
          <Field className='max-w-xs'>
            <FieldLabel htmlFor='admin-request-status'>
              {t('Filter by status')}
            </FieldLabel>
            <NativeSelect
              id='admin-request-status'
              value={status}
              onChange={(event) => {
                setStatus(event.target.value as RequestStatus | '')
                setPage(1)
              }}
            >
              <NativeSelectOption value=''>
                {t('All statuses')}
              </NativeSelectOption>
              {REQUEST_STATUSES.map((value) => (
                <NativeSelectOption key={value} value={value}>
                  {requestStatusLabel(value, t)}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </Field>
          {query.isPending && <Skeleton className='h-64 w-full' />}
          {query.isError && (
            <ErrorState
              title={t('Failed to load data')}
              onRetry={() => void query.refetch()}
            />
          )}
          {query.data && (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Request')}</TableHead>
                    <TableHead>{t('User')}</TableHead>
                    <TableHead>{t('Status')}</TableHead>
                    <TableHead>{t('Updated At')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {query.data.items.map((ticket) => (
                    <TableRow key={ticket.id}>
                      <TableCell className='max-w-sm'>
                        <Button
                          type='button'
                          variant='link'
                          className='h-auto max-w-full justify-start px-0 text-left whitespace-normal'
                          onClick={() => setRequestId(ticket.id)}
                        >
                          {ticket.title}
                        </Button>
                        <p className='text-muted-foreground text-xs'>
                          #{ticket.id}
                        </p>
                      </TableCell>
                      <TableCell>
                        {ticket.username ?? `#${ticket.user_id}`}
                      </TableCell>
                      <TableCell>
                        <RequestStatusBadge status={ticket.status} />
                      </TableCell>
                      <TableCell className='text-muted-foreground text-xs'>
                        {new Date(ticket.updated_at * 1000).toLocaleString(
                          locale
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              {query.data.items.length === 0 && (
                <Empty>
                  <EmptyHeader>
                    <EmptyTitle>{t('No matching requests')}</EmptyTitle>
                  </EmptyHeader>
                </Empty>
              )}
              <div className='flex flex-wrap items-center justify-between gap-3'>
                <p className='text-muted-foreground text-sm'>
                  {t('Page {{page}} of {{pageCount}}', {
                    page,
                    pageCount: Math.max(
                      1,
                      Math.ceil(query.data.total / pageSize)
                    ),
                  })}
                </p>
                <div className='flex gap-2'>
                  <Button
                    type='button'
                    variant='outline'
                    disabled={page <= 1 || query.isFetching}
                    onClick={() => setPage((value) => value - 1)}
                  >
                    {t('Previous')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    disabled={
                      page * pageSize >= query.data.total || query.isFetching
                    }
                    onClick={() => setPage((value) => value + 1)}
                  >
                    {t('Next')}
                  </Button>
                </div>
              </div>
            </>
          )}
        </div>
        {requestId !== null && (
          <AdminRequestDetail
            key={requestId}
            requestId={requestId}
            onClose={() => setRequestId(null)}
          />
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
