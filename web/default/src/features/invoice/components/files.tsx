import {
  Download01Icon,
  File01Icon,
  ViewIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

import { fetchInvoiceFile } from '../api'
import { formatInvoiceMoney, invoiceDate } from '../lib/format'
import type { InvoiceAttachment, InvoiceDelivery } from '../types'
import { InvoiceError } from './shared'

export function InvoiceFileButton(props: {
  requestId: string
  file: InvoiceAttachment
  admin?: boolean
  download?: boolean
}) {
  const { t } = useTranslation()
  const [url, setUrl] = useState('')
  const [previewFailed, setPreviewFailed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  useEffect(
    () => () => {
      if (url) URL.revokeObjectURL(url)
    },
    [url]
  )
  async function open(download = !!props.download) {
    setBusy(true)
    setError(null)
    try {
      const blob = await fetchInvoiceFile(
        props.requestId,
        props.file.id,
        !!props.admin,
        download
      )
      const nextUrl = URL.createObjectURL(blob)
      if (download) {
        const anchor = document.createElement('a')
        anchor.href = nextUrl
        anchor.download = props.file.file_name
        anchor.click()
        setTimeout(() => URL.revokeObjectURL(nextUrl), 1000)
      } else {
        setPreviewFailed(false)
        setUrl(nextUrl)
      }
    } catch (cause) {
      setError(cause)
    } finally {
      setBusy(false)
    }
  }
  return (
    <>
      <Button
        type='button'
        size='sm'
        variant='outline'
        disabled={busy}
        onClick={() => void open()}
      >
        <HugeiconsIcon
          icon={props.download ? Download01Icon : ViewIcon}
          size={16}
        />
        {props.download ? t('Download') : t('Preview')}
      </Button>
      {error && !url ? (
        <InvoiceError error={error} onRetry={() => void open()} />
      ) : null}
      <Dialog
        open={!!url}
        onOpenChange={(open) => {
          if (!open) setUrl('')
        }}
      >
        <DialogContent className='sm:max-w-4xl'>
          <DialogHeader>
            <DialogTitle className='pr-8 wrap-anywhere'>
              {props.file.file_name}
            </DialogTitle>
          </DialogHeader>
          <div className='flex flex-wrap gap-2'>
            <Button
              type='button'
              size='sm'
              variant='outline'
              disabled={busy}
              onClick={() => void open(false)}
            >
              {previewFailed ? t('Retry') : t('Refresh')}
            </Button>
            <Button
              type='button'
              size='sm'
              variant='outline'
              disabled={busy}
              onClick={() => void open(true)}
            >
              <HugeiconsIcon icon={Download01Icon} size={16} />
              {t('Download')}
            </Button>
          </div>
          <InvoiceError
            error={error}
            message={previewFailed ? t('Failed to load image') : undefined}
          />
          {props.file.mime_type === 'application/pdf' ? (
            <iframe
              src={url}
              sandbox=''
              title={t('Invoice preview')}
              className='h-[65dvh] w-full rounded border'
            />
          ) : (
            !previewFailed && (
              <img
                src={url}
                alt={t('Invoice attachment')}
                onError={() => setPreviewFailed(true)}
                className='max-h-[65dvh] w-full rounded object-contain'
              />
            )
          )}
        </DialogContent>
      </Dialog>
    </>
  )
}
export function InvoiceFileCard(props: {
  requestId: string
  delivery: InvoiceDelivery
  current: boolean
  exponent: number
  admin?: boolean
}) {
  const { t, i18n } = useTranslation()
  return (
    <article className='rounded-lg border p-4'>
      <div className='flex flex-wrap items-start justify-between gap-3'>
        <div className='flex min-w-0 gap-3'>
          <HugeiconsIcon
            icon={File01Icon}
            className='text-primary size-8 shrink-0'
          />
          <div className='min-w-0'>
            <p className='text-sm font-medium wrap-anywhere'>
              {props.delivery.file.file_name}
            </p>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('Version {{version}}', {
                version: props.delivery.delivery_version,
              })}{' '}
              ·{' '}
              {props.current
                ? t('Current invoice')
                : t('Historical invoice — cannot be confirmed')}
            </p>
            <p className='text-muted-foreground mt-1 text-xs'>
              {props.delivery.file.mime_type} ·{' '}
              {(props.delivery.file.size / 1024).toFixed(0)} KB
            </p>
          </div>
        </div>
        <div className='flex flex-wrap gap-2'>
          <InvoiceFileButton
            requestId={props.requestId}
            file={props.delivery.file}
            admin={props.admin}
          />
          <InvoiceFileButton
            requestId={props.requestId}
            file={props.delivery.file}
            admin={props.admin}
            download
          />
        </div>
      </div>
      <dl className='mt-4 grid gap-x-4 gap-y-2 text-xs sm:grid-cols-2'>
        <div>
          <dt className='text-muted-foreground'>{t('Invoice number')}</dt>
          <dd className='mt-1 wrap-anywhere'>{props.delivery.invoice_no}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Issue date')}</dt>
          <dd className='mt-1'>{props.delivery.issued_on}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Invoice amount')}</dt>
          <dd className='mt-1 tabular-nums'>
            {formatInvoiceMoney(
              props.delivery.amount_minor,
              props.delivery.currency,
              props.exponent,
              i18n.language
            )}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Delivered')}</dt>
          <dd className='mt-1'>
            {invoiceDate(props.delivery.created_at, i18n.language)}
          </dd>
        </div>
      </dl>
    </article>
  )
}
