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

export function InvoiceUserActions(props: { invoice: InvoiceRequest }) {
  const { t } = useTranslation()
  const invoice = props.invoice
  const [dialog, setDialog] = useState<
    'confirm' | 'dispute' | 'message' | null
  >(null)
  const [description, setDescription] = useState('')
  const [category, setCategory] = useState('title_tax')
  const [attachment, setAttachment] = useState<InvoiceAttachment | null>(null)
  const [uploading, setUploading] = useState(false)
  const op = useInvoiceOperation()
  const [actionVersion, setActionVersion] = useState({
    version: invoice.version,
    delivery_version: invoice.current_delivery_version,
  })
  function openDialog(value: 'confirm' | 'dispute' | 'message') {
    setActionVersion({
      version: invoice.version,
      delivery_version: invoice.current_delivery_version,
    })
    op.setError(null)
    setDialog(value)
  }
  const stale =
    !!dialog &&
    (actionVersion.version !== invoice.version ||
      actionVersion.delivery_version !== invoice.current_delivery_version)
  const canConfirm = invoice.allowed_actions.includes('confirm')
  const canDispute =
    invoice.allowed_actions.includes('dispute') ||
    invoice.allowed_actions.includes('disputes')
  const canMessage =
    invoice.allowed_actions.includes('message') ||
    invoice.allowed_actions.includes('messages')
  async function submit() {
    if (stale) return
    let action = 'confirm'
    let payload: object = {
      ...actionVersion,
    }
    if (dialog !== 'confirm') {
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
  if (op.busy) submitLabel = t('Processing...')
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
      </div>
      <Dialog
        open={!!dialog}
        onOpenChange={(open) => {
          if (!open && !op.busy && !uploading) setDialog(null)
        }}
      >
        <DialogContent className='sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>
              {dialog === 'confirm'
                ? t('Confirm this invoice?')
                : t('Report an invoice problem')}
            </DialogTitle>
            <DialogDescription>
              {dialog === 'confirm'
                ? t(
                    'Check the current invoice title, tax ID, amount and attachment. Confirmation does not waive correction rights.'
                  )
                : t(
                    'Your feedback stays linked to the current invoice version. Orders remain reserved.'
                  )}
            </DialogDescription>
          </DialogHeader>
          {dialog !== 'confirm' && (
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
              disabled={
                op.busy ||
                stale ||
                uploading ||
                (dialog !== 'confirm' && !description.trim())
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
