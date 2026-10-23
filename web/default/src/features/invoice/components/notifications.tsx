import { Notification01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { useAuthStore } from '@/stores/auth-store'

import {
  getInvoiceNotifications,
  invoiceKey,
  readInvoiceNotification,
} from '../api'
import { invoiceDate, statusKeys } from '../lib/format'
import { useInvoiceOperation } from '../lib/use-invoice-operation'
import { InvoiceEmpty, InvoiceError, InvoicePagination } from './shared'

export function InvoiceNotifications(props: { admin?: boolean }) {
  const { t, i18n } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [open, setOpen] = useState(false)
  const [page, setPage] = useState(1)
  const navigate = useNavigate()
  const op = useInvoiceOperation()
  const query = useQuery({
    queryKey: [...invoiceKey(userId, props.admin), 'notifications', page],
    queryFn: () => getInvoiceNotifications(!!props.admin, page),
    retry: false,
    gcTime: 0,
  })
  return (
    <>
      <Button variant='outline' size='sm' onClick={() => setOpen(true)}>
        <HugeiconsIcon icon={Notification01Icon} size={16} />
        {t('Invoice updates')}
        {!!query.data?.unread_count && (
          <span className='bg-primary text-primary-foreground rounded-full px-1.5'>
            {query.data.unread_count}
          </span>
        )}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className='sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>{t('Invoice updates')}</DialogTitle>
            <DialogDescription>
              {t('Read an update to open its invoice request.')}
            </DialogDescription>
          </DialogHeader>
          <Button
            variant='ghost'
            size='sm'
            disabled={op.busy || !query.data?.unread_count}
            onClick={() =>
              void op.run('read-all', {}, () =>
                readInvoiceNotification(!!props.admin)
              )
            }
          >
            {t('Mark all as read')}
          </Button>
          <InvoiceError
            error={query.error || op.error}
            onRetry={() => void query.refetch()}
          />
          {query.isPending && <Skeleton className='h-36' />}
          {query.data?.items.length === 0 && (
            <InvoiceEmpty>{t('No invoice updates yet.')}</InvoiceEmpty>
          )}
          <div className='divide-y'>
            {query.data?.items.map((event) => (
              <button
                key={event.id}
                type='button'
                className='flex w-full gap-3 py-3 text-left'
                disabled={op.busy}
                onClick={async () => {
                  await op.run('read', event.id, () =>
                    readInvoiceNotification(!!props.admin, event.id)
                  )
                  setOpen(false)
                  void navigate({
                    to: props.admin
                      ? '/invoice-admin/$invoiceId'
                      : '/invoices/$invoiceId',
                    params: { invoiceId: event.request_public_id || '' },
                  })
                }}
              >
                <span
                  className={`mt-1.5 size-1.5 shrink-0 rounded-full ${event.read_at ? 'bg-muted' : 'bg-primary'}`}
                />
                <span className='min-w-0'>
                  <span className='text-sm font-medium'>
                    {event.display_no || event.request_public_id}
                  </span>
                  <span className='text-muted-foreground mt-1 block text-xs'>
                    {t(statusKeys[event.status])}
                  </span>
                  <span className='text-muted-foreground mt-1 block text-xs'>
                    {invoiceDate(event.created_at, i18n.language)}
                  </span>
                </span>
              </button>
            ))}
          </div>
          <InvoicePagination
            page={page}
            total={query.data?.total || 0}
            onChange={setPage}
          />
        </DialogContent>
      </Dialog>
    </>
  )
}
