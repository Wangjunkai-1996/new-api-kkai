import { Upload01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { uploadInvoiceAttachment } from '../api'
import type { InvoiceAttachment } from '../types'
import { InvoiceError } from './shared'

export function InvoiceUpload(props: {
  requestId: string
  admin?: boolean
  value: InvoiceAttachment | null
  onChange: (file: InvoiceAttachment | null) => void
  onBusy?: (busy: boolean) => void
}) {
  const { t } = useTranslation()
  const [progress, setProgress] = useState<number | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [validation, setValidation] = useState('')
  const keys = useRef(new Map<string, string>())
  async function upload(file: File | undefined) {
    if (!file) return
    setValidation('')
    setError(null)
    if (
      file.size > 20 * 1024 * 1024 ||
      !['application/pdf', 'image/png', 'image/jpeg'].includes(file.type)
    ) {
      setValidation(t('Choose a PDF, PNG or JPEG file up to 20 MiB.'))
      return
    }
    setProgress(0)
    props.onBusy?.(true)
    try {
      const bytes = await crypto.subtle.digest(
        'SHA-256',
        await file.arrayBuffer()
      )
      const digest = `${file.name}:${file.type}:${Array.from(new Uint8Array(bytes), (byte) => byte.toString(16).padStart(2, '0')).join('')}`
      const key = keys.current.get(digest) || crypto.randomUUID()
      keys.current.set(digest, key)
      const attachment = await uploadInvoiceAttachment(
        props.requestId,
        file,
        !!props.admin,
        key,
        setProgress
      )
      props.onChange(attachment)
    } catch (cause) {
      setError(cause)
    } finally {
      setProgress(null)
      props.onBusy?.(false)
    }
  }
  return (
    <div className='space-y-3'>
      <label className='flex cursor-pointer flex-col items-center justify-center gap-2 rounded-lg border border-dashed p-5 text-center'>
        <HugeiconsIcon icon={Upload01Icon} size={24} className='text-primary' />
        <span className='text-sm font-medium'>
          {props.admin
            ? t('Upload issued invoice')
            : t('Add supporting evidence')}
        </span>
        <span className='text-muted-foreground text-xs'>
          {t('PDF, PNG or JPEG · up to 20 MiB')}
        </span>
        <input
          aria-label={
            props.admin
              ? t('Upload issued invoice')
              : t('Add supporting evidence')
          }
          type='file'
          accept='application/pdf,image/png,image/jpeg'
          disabled={progress !== null}
          onChange={(event) => {
            void upload(event.target.files?.[0])
            event.target.value = ''
          }}
          className='mt-2 max-w-full text-xs file:mr-2 file:rounded file:border file:px-2 file:py-1'
        />
      </label>
      {progress !== null && (
        <p role='status' className='text-muted-foreground text-xs'>
          {t('Uploading {{percent}}%', { percent: progress })}
        </p>
      )}
      {props.value && (
        <div className='bg-muted/30 flex items-start justify-between gap-3 rounded border p-3 text-xs'>
          <span className='min-w-0 wrap-anywhere'>
            {props.value.file_name}
            <span className='mt-1 block text-emerald-700 dark:text-emerald-300'>
              {t('File uploaded and validated. Not delivered yet.')}
            </span>
          </span>
          <Button
            variant='ghost'
            size='sm'
            onClick={() => props.onChange(null)}
          >
            {t('Remove')}
          </Button>
        </div>
      )}
      <InvoiceError error={error} message={validation || undefined} />
    </div>
  )
}
