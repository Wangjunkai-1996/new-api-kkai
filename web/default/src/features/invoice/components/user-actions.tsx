import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { NativeSelect } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'

import { invoiceAction } from '../api'
import { useInvoiceOperation } from '../lib/use-invoice-operation'
import type { InvoiceAttachment, InvoiceRequest } from '../types'
import { InvoiceError } from './shared'
import { InvoiceUpload } from './upload'

type UserAction = 'confirm' | 'dispute' | 'message' | 'withdraw'

export function InvoiceUserActions(props: { invoice: InvoiceRequest }) {
  const { t } = useTranslation()
  const invoice = props.invoice
  const [dialog, setDialog] = useState<UserAction | null>(null)
  const [description, setDescription] = useState('')
  const [category, setCategory] = useState('title_tax')
  const [attachment, setAttachment] = useState<InvoiceAttachment | null>(null)
  const [uploading, setUploading] = useState(false)
  const op = useInvoiceOperation()
  const [actionVersion, setActionVersion] = useState({
    version: invoice.version,
    delivery_version: invoice.current_delivery_version,
  })
  function openDialog(value: UserAction) {
    setActionVersion({
      version: invoice.version,
      delivery_version: invoice.current_delivery_version,
    })
    op.setError(null)
    setDialog(value)
  }
  const canWithdraw = invoice.allowed_actions.includes('withdraw')
  const stale =
    !!dialog &&
    (actionVersion.version !== invoice.version ||
      actionVersion.delivery_version !== invoice.current_delivery_version ||
      (dialog === 'withdraw' && !canWithdraw))
  const canConfirm = invoice.allowed_actions.includes('confirm')
  const canDispute =
    invoice.allowed_actions.includes('dispute') ||
    invoice.allowed_actions.includes('disputes')
  const canMessage =
    invoice.allowed_actions.includes('message') ||
    invoice.allowed_actions.includes('messages')
  const needsFeedback = dialog === 'message' || dialog === 'dispute'
  async function submit() {
    if (!dialog || stale) return
    let action: string = dialog
    let payload: object =
      dialog === 'withdraw' ? { version: actionVersion.version } : actionVersion
    if (needsFeedback) {
      action =
        dialog === 'message'
          ? `disputes/${invoice.active_dispute?.id}/messages`
          : 'disputes'
      payload = {
        ...payload,
        category,
        description: description.trim(),
        evidence_attachment_id: attachment?.id,
      }
    }
    const result = await op.run(action, payload, (key) =>
      invoiceAction(invoice.public_id, action, payload, key)
    )
    if (result) {
      setDialog(null)
      setDescription('')
      setAttachment(null)
    }
  }
  let disputeLabel = t('Invoice has a problem')
  if (invoice.status === 'confirmed') disputeLabel = t('Report a problem')
  if (invoice.status === 'awaiting_reconfirmation') {
    disputeLabel = t('Continue feedback')
  }
  let submitLabel = t('Submit feedback')
  if (dialog === 'confirm') submitLabel = t('Confirm correct')
  if (dialog === 'withdraw') submitLabel = t('Withdraw request')
  if (op.busy) submitLabel = t('Processing...')
  let dialogTitle = t('Report an invoice problem')
  let dialogDescription = t(
    'Your feedback stays linked to the current invoice version. Orders remain reserved.'
  )
  if (dialog === 'confirm') {
    dialogTitle = t('Confirm this invoice?')
    dialogDescription = t(
      'Check the current invoice title, tax ID, amount and attachment. Confirmation does not waive correction rights.'
    )
  } else if (dialog === 'withdraw') {
    dialogTitle = t('Withdraw this invoice request?')
    dialogDescription = t(
      'Withdrawing releases the associated payments. Submit a new request if needed.'
    )
  }
  return (
    <>
      <div className='flex flex-wrap gap-2'>
        {canConfirm && (
          <Button onClick={() => openDialog('confirm')}>
            {t('Confirm correct')}
          </Button>
        )}
        {canDispute && (
          <Button variant='outline' onClick={() => openDialog('dispute')}>
            {disputeLabel}
          </Button>
        )}
        {canMessage && (
          <Button variant='outline' onClick={() => openDialog('message')}>
            {t('Add more information')}
          </Button>
        )}
        {canWithdraw && (
          <Button variant='destructive' onClick={() => openDialog('withdraw')}>
            {t('Withdraw request')}
          </Button>
        )}
      </div>
      <Dialog
        open={!!dialog}
        onOpenChange={(open) => {
          if (!open && !op.busy && !uploading) setDialog(null)
        }}
      >
        <DialogContent className='sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>{dialogTitle}</DialogTitle>
            <DialogDescription>{dialogDescription}</DialogDescription>
          </DialogHeader>
          {needsFeedback && (
            <div className='space-y-4'>
              {dialog !== 'message' && (
                <label className='grid gap-2 text-sm'>
                  {t('Problem type')}
                  <NativeSelect
                    value={category}
                    onChange={(event) => setCategory(event.target.value)}
                  >
                    <option value='title_tax'>
                      {t('Incorrect title or tax ID')}
                    </option>
                    <option value='amount'>{t('Incorrect amount')}</option>
                    <option value='attachment'>
                      {t('Attachment cannot be opened')}
                    </option>
                    <option value='incomplete'>
                      {t('Incomplete invoice information')}
                    </option>
                    <option value='other'>{t('Other')}</option>
                  </NativeSelect>
                </label>
              )}
              <label className='grid gap-2 text-sm'>
                {t('Description')}
                <Textarea
                  rows={4}
                  maxLength={1000}
                  value={description}
                  onChange={(event) => setDescription(event.target.value)}
                />
              </label>
              <InvoiceUpload
                requestId={invoice.public_id}
                value={attachment}
                onChange={setAttachment}
                onBusy={setUploading}
              />
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
              variant={dialog === 'withdraw' ? 'destructive' : 'default'}
              disabled={
                op.busy ||
                stale ||
                uploading ||
                (needsFeedback && !description.trim())
              }
              onClick={() => void submit()}
            >
              {submitLabel}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
