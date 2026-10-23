import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'

import { invoiceAction } from '../api'
import { formatInvoiceMoney, invoiceMinorFromDecimal } from '../lib/format'
import { useInvoiceOperation } from '../lib/use-invoice-operation'
import type { InvoiceAttachment, InvoiceRequest } from '../types'
import { InvoiceError } from './shared'
import { InvoiceUpload } from './upload'

type AdminDialog =
  | 'approve'
  | 'reject'
  | 'hold'
  | 'resume'
  | 'reply'
  | 'deliver'
  | 'reissue'
export function InvoiceAdminActions(props: { invoice: InvoiceRequest }) {
  const { t, i18n } = useTranslation()
  const invoice = props.invoice
  const op = useInvoiceOperation()
  const [dialog, setDialog] = useState<AdminDialog | null>(null)
  const [actionVersion, setActionVersion] = useState(invoice.version)
  function openDialog(value: AdminDialog) {
    setActionVersion(invoice.version)
    op.setError(null)
    setDialog(value)
  }
  const stale = !!dialog && actionVersion !== invoice.version
  const [reason, setReason] = useState('')
  const [notIssued, setNotIssued] = useState(false)
  const [attachment, setAttachment] = useState<InvoiceAttachment | null>(null)
  const [uploading, setUploading] = useState(false)
  const [invoiceNo, setInvoiceNo] = useState('')
  const [issuedOn, setIssuedOn] = useState(() =>
    new Date().toLocaleDateString('en-CA')
  )
  const [taxAmount, setTaxAmount] = useState('')
  const taxMinor = invoiceMinorFromDecimal(taxAmount, invoice.currency_exponent)
  const taxInvalid =
    taxAmount !== '' &&
    (taxMinor === null || BigInt(taxMinor) > BigInt(invoice.amount_minor))
  const requiresReason =
    dialog && ['reject', 'hold', 'resume', 'reply'].includes(dialog)
  const delivery = dialog === 'deliver' || dialog === 'reissue'
  const allowed = new Set(invoice.allowed_actions)
  const labels: Record<AdminDialog, string> = {
    approve: 'Approve request',
    reject: 'Reject request',
    hold: 'Pause for finance review',
    resume: 'Resume processing',
    reply: 'Reply and request confirmation',
    deliver: 'Deliver invoice',
    reissue: 'Deliver corrected invoice',
  }
  async function perform() {
    if (!dialog || stale) return
    let action: string = dialog
    let payload: object = {
      version: actionVersion,
      reason: reason.trim(),
      message: reason.trim(),
      not_issued_confirmed: notIssued,
    }
    if (dialog === 'reply' || dialog === 'reissue') {
      action = `disputes/${invoice.active_dispute?.id}/${dialog}`
    }
    if (dialog === 'deliver') action = 'deliveries'
    if (delivery) {
      payload = {
        version: actionVersion,
        attachment_id: attachment?.id,
        invoice_no: invoiceNo.trim(),
        issued_on: issuedOn,
        currency: invoice.currency,
        amount_minor: invoice.amount_minor,
        ...(taxMinor ? { tax_minor: taxMinor } : {}),
      }
    }
    const result = await op.run(action, payload, (key) =>
      invoiceAction(invoice.public_id, action, payload, key, true)
    )
    if (result) {
      setDialog(null)
      setReason('')
      setAttachment(null)
      setNotIssued(false)
      setInvoiceNo('')
      setTaxAmount('')
    }
  }
  let dialogDescription = t(
    'Your explanation is visible to the user and remains in the request history.'
  )
  if (delivery) {
    dialogDescription = t(
      'Uploading does not deliver the invoice. Check all details before final delivery.'
    )
  }
  if (dialog === 'approve') {
    dialogDescription = t(
      'Approval moves this request to invoice preparation. It does not issue or deliver an invoice.'
    )
  }
  const submitLabel = op.busy
    ? t('Processing...')
    : t(dialog ? labels[dialog] : 'Confirm')
  return (
    <section className='space-y-3 border-t pt-5'>
      <div className='flex flex-wrap gap-2'>
        {allowed.has('approve') && (
          <Button onClick={() => openDialog('approve')}>
            {t('Approve request')}
          </Button>
        )}
        {allowed.has('reject') && (
          <Button variant='destructive' onClick={() => openDialog('reject')}>
            {t('Reject request')}
          </Button>
        )}
        {(allowed.has('deliver') || allowed.has('deliveries')) && (
          <Button onClick={() => openDialog('deliver')}>
            {t('Upload and deliver invoice')}
          </Button>
        )}
        {allowed.has('reply') && (
          <Button variant='outline' onClick={() => openDialog('reply')}>
            {t('Reply and request confirmation')}
          </Button>
        )}
        {allowed.has('reissue') && (
          <Button onClick={() => openDialog('reissue')}>
            {t('Correct and reissue')}
          </Button>
        )}
        {allowed.has('hold') && (
          <Button variant='outline' onClick={() => openDialog('hold')}>
            {t('Pause for finance review')}
          </Button>
        )}
        {allowed.has('resume') && (
          <Button variant='outline' onClick={() => openDialog('resume')}>
            {t('Resume processing')}
          </Button>
        )}
      </div>
      {invoice.status === 'approved_for_issue' && (
        <p className='text-muted-foreground text-xs leading-relaxed'>
          {t(
            'Issue the legal invoice in your finance system, then upload and explicitly deliver it here.'
          )}
        </p>
      )}
      <Dialog
        open={!!dialog}
        onOpenChange={(open) => {
          if (!open && !op.busy && !uploading) setDialog(null)
        }}
      >
        <DialogContent className='sm:max-w-xl'>
          <DialogHeader>
            <DialogTitle>{dialog ? t(labels[dialog]) : ''}</DialogTitle>
            <DialogDescription>{dialogDescription}</DialogDescription>
          </DialogHeader>
          {requiresReason && (
            <label className='grid gap-2 text-sm'>
              {dialog === 'reply' ? t('Reply to user') : t('Reason')}
              <Textarea
                rows={4}
                maxLength={1000}
                value={reason}
                onChange={(event) => setReason(event.target.value)}
              />
            </label>
          )}
          {dialog === 'reject' && (
            <label className='flex items-start gap-3 text-sm leading-relaxed'>
              <Checkbox
                className='mt-1'
                checked={notIssued}
                onCheckedChange={setNotIssued}
              />
              {t(
                'I confirm no invoice has actually been issued or delivered for this request.'
              )}
            </label>
          )}
          {dialog === 'resume' && (
            <p className='text-sm'>
              {t(
                'Payment evidence will be checked again before the previous processing stage is restored.'
              )}
            </p>
          )}
          {delivery && (
            <div className='space-y-4'>
              {dialog === 'reissue' && (
                <p className='rounded-lg bg-amber-500/10 p-3 text-sm'>
                  {t(
                    'The previous invoice remains available. A new version becomes current only after delivery.'
                  )}
                </p>
              )}
              <InvoiceUpload
                requestId={invoice.public_id}
                admin
                value={attachment}
                onChange={setAttachment}
                onBusy={setUploading}
              />
              <div className='grid gap-4 sm:grid-cols-2'>
                <label className='grid gap-2 text-sm'>
                  {t('Invoice number')}
                  <Input
                    maxLength={128}
                    value={invoiceNo}
                    onChange={(event) => setInvoiceNo(event.target.value)}
                  />
                </label>
                <label className='grid gap-2 text-sm'>
                  {t('Issue date')}
                  <Input
                    type='date'
                    value={issuedOn}
                    onChange={(event) => setIssuedOn(event.target.value)}
                  />
                </label>
              </div>
              <div className='rounded-lg border p-4 text-sm'>
                <div className='invoice-data-row'>
                  <span>{t('Invoice title')}</span>
                  <span>{invoice.profile.title}</span>
                </div>
                <div className='invoice-data-row'>
                  <span>{t('Tax ID')}</span>
                  <span>{invoice.profile.tax_id}</span>
                </div>
                <div className='invoice-data-row'>
                  <span>{t('Locked invoice amount')}</span>
                  <span className='tabular-nums'>
                    {formatInvoiceMoney(
                      invoice.amount_minor,
                      invoice.currency,
                      invoice.currency_exponent,
                      i18n.language
                    )}
                  </span>
                </div>
                <div className='invoice-data-row'>
                  <span>{t('Invoice email')}</span>
                  <span>{invoice.profile.email}</span>
                </div>
              </div>
              <label className='grid gap-2 text-sm'>
                {t('Tax amount (optional)')}
                <Input
                  inputMode='decimal'
                  maxLength={30}
                  aria-invalid={taxInvalid}
                  value={taxAmount}
                  onChange={(event) => setTaxAmount(event.target.value)}
                />
              </label>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Check the uploaded invoice matches the title, tax ID, currency and amount above.'
                )}
              </p>
            </div>
          )}
          <InvoiceError
            error={op.error}
            message={
              stale
                ? t(
                    'This request was updated. Review the latest version before continuing.'
                  )
                : undefined
            }
          />
          <DialogFooter>
            <Button
              variant='outline'
              disabled={op.busy || uploading}
              onClick={() => setDialog(null)}
            >
              {t('Cancel')}
            </Button>
            <Button
              variant={dialog === 'reject' ? 'destructive' : 'default'}
              disabled={
                op.busy ||
                stale ||
                uploading ||
                (!!requiresReason && !reason.trim()) ||
                (dialog === 'reject' && !notIssued) ||
                (delivery &&
                  (!attachment || !invoiceNo.trim() || !issuedOn || taxInvalid))
              }
              onClick={() => void perform()}
            >
              {submitLabel}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}
