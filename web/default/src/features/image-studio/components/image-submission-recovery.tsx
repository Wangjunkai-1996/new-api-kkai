/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  clearImageStudioSubmissionKey,
  listPendingImageStudioSubmissions,
} from '@/stores/image-studio-draft-store'

import { lookupImageSubmission } from '../api'
import { isImageSubmissionReceiptExpired } from '../image-domain'
import type { ImageGeneration } from '../types'

export function ImageSubmissionRecovery(props: {
  userId: number
  onRecovered: (generation: ImageGeneration) => void
}) {
  const { t } = useTranslation()
  const [receipts, setReceipts] = useState(() =>
    listPendingImageStudioSubmissions(props.userId)
  )
  const recovery = useMutation({
    mutationFn: (idempotencyKey: string) =>
      lookupImageSubmission(idempotencyKey),
    retry: false,
  })
  if (receipts.length === 0) return null

  const recover = async (receipt: (typeof receipts)[number]): Promise<void> => {
    try {
      const result = await recovery.mutateAsync(receipt.idempotencyKey)
      if (result.state === 'found' && result.generation) {
        if (result.generation.status !== 'unknown') {
          clearImageStudioSubmissionKey(
            props.userId,
            receipt.requestFingerprint
          )
          setReceipts((current) =>
            current.filter(
              (item) => item.idempotencyKey !== receipt.idempotencyKey
            )
          )
        }
        props.onRecovered(result.generation)
        return
      }
      if (result.state === 'not_found') {
        if (isImageSubmissionReceiptExpired(receipt.expiresAt)) {
          toast.error(
            t(
              'This submission receipt has expired. Check your image history or contact support before submitting again.'
            )
          )
        } else {
          toast.info(
            t('No submitted task was found. Click Generate to retry safely.')
          )
        }
      } else {
        toast.info(
          t(
            'Your submission is still being checked. No new image request was sent.'
          )
        )
      }
    } catch {
      toast.error(t('Unable to check the submitted task. Please try again.'))
    }
  }

  return (
    <Alert>
      <AlertTitle>{t('Check your previous submission')}</AlertTitle>
      <AlertDescription>
        <p>
          {t(
            'A previous submission has no confirmed response. Find its task without uploading the images again.'
          )}
        </p>
        {receipts.map((receipt) => (
          <Button
            key={receipt.idempotencyKey}
            type='button'
            variant='outline'
            size='sm'
            disabled={recovery.isPending}
            onClick={() => void recover(receipt)}
          >
            {t('Check submitted task')} ·{' '}
            {new Date(receipt.submittedAt ?? 0).toLocaleString()}
          </Button>
        ))}
      </AlertDescription>
    </Alert>
  )
}
