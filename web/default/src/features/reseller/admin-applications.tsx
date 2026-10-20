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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { toIntlLocale } from '@/i18n/languages'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import { reviewApplication, type ResellerApplication } from './admin-api'
import { resellerQueryKey } from './api'
import { applicationStatusLabel } from './withdrawal-status'

export function AdminApplications(props: {
  applications: ResellerApplication[]
  rootDomain?: string
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage ?? i18n.language)
  const client = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [review, setReview] = useState<{
    application: ResellerApplication
    status: 'approved' | 'rejected'
  } | null>(null)
  const [reason, setReason] = useState('')
  const mutation = useMutation({
    mutationFn: () => {
      if (!review) throw new Error('No application selected')
      return reviewApplication(
        review.application.id,
        review.status,
        reason.trim()
      )
    },
    onSuccess: () => {
      setReview(null)
      setReason('')
      toast.success(t('Saved successfully'))
      void client.invalidateQueries({
        queryKey: resellerQueryKey(userId, 'admin'),
      })
    },
    onError: (error) => handleServerError(error),
  })
  if (!props.applications.length) {
    return <p className='text-muted-foreground p-4'>{t('No data available')}</p>
  }
  return (
    <div className='space-y-4'>
      {props.applications.map((application) => (
        <Card key={application.id}>
          <CardHeader>
            <CardTitle>
              {application.business_name}{' '}
              <span className='text-muted-foreground text-sm'>
                #{application.id}
              </span>
            </CardTitle>
          </CardHeader>
          <CardContent className='space-y-3 text-sm'>
            <dl className='grid gap-2 sm:grid-cols-2'>
              <div>
                <dt className='text-muted-foreground'>{t('Contact')}</dt>
                <dd>
                  {[application.contact_name, application.contact_email]
                    .filter(Boolean)
                    .join(' · ')}
                </dd>
              </div>
              <div>
                <dt className='text-muted-foreground'>
                  {t('Preferred Domain Prefix')}
                </dt>
                <dd className='break-all'>
                  {application.desired_domain_prefix
                    ? `${application.desired_domain_prefix}${props.rootDomain ? `.${props.rootDomain}` : ''}`
                    : t('No preference')}
                </dd>
              </div>
              <div>
                <dt className='text-muted-foreground'>{t('Created At')}</dt>
                <dd>
                  {new Date(application.created_at).toLocaleString(locale)}
                </dd>
              </div>
              <div>
                <dt className='text-muted-foreground'>{t('Status')}</dt>
                <dd>{applicationStatusLabel(application.status, t)}</dd>
              </div>
            </dl>
            <p className='break-words whitespace-pre-wrap'>
              {application.notes}
            </p>
            {application.rejection_reason && (
              <p>{application.rejection_reason}</p>
            )}
            {application.status === 'pending' && (
              <div className='flex flex-wrap gap-2'>
                <Button
                  onClick={() => {
                    setReview({ application, status: 'approved' })
                    setReason('')
                  }}
                >
                  {t('Approve')}
                </Button>
                <Button
                  variant='destructive'
                  onClick={() => {
                    setReview({ application, status: 'rejected' })
                    setReason('')
                  }}
                >
                  {t('Reject')}
                </Button>
              </div>
            )}
          </CardContent>
        </Card>
      ))}
      <ConfirmDialog
        open={Boolean(review)}
        onOpenChange={(open) => {
          if (!open && !mutation.isPending) setReview(null)
        }}
        title={t('Review Reseller Application')}
        desc={review?.application.business_name ?? ''}
        isLoading={mutation.isPending}
        disabled={review?.status === 'rejected' && !reason.trim()}
        destructive={review?.status === 'rejected'}
        handleConfirm={() => mutation.mutate()}
      >
        <div className='space-y-2'>
          <Label htmlFor='application-reason'>{t('Review Note')}</Label>
          <Textarea
            id='application-reason'
            maxLength={4096}
            value={reason}
            onChange={(event) => setReason(event.target.value)}
          />
        </div>
      </ConfirmDialog>
    </div>
  )
}
