import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ErrorState } from '@/components/error-state'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import {
  getAdminRequest,
  isRequestTicketUnavailable,
  requestQueryKey,
  transitionRequest,
  type RequestStatus,
  type RequestTicket,
} from './api'
import {
  requestReviewSchema,
  type RequestReviewValues,
} from './lib/admin-schema'
import {
  requestStatusActions,
  requestStatusDescription,
  requestStatusLabel,
} from './status'
import { RequestStatusBadge } from './status-badge'
import { RequestTimeline } from './timeline'

function AdminReviewForm(props: {
  ticket: RequestTicket
  onRefresh: () => Promise<boolean>
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const nextStatuses = requestStatusActions(props.ticket.status)
  const [selection, setSelection] = useState<RequestStatus>('accepted')
  const [conflict, setConflict] = useState(false)
  const targetStatus = nextStatuses.includes(selection)
    ? selection
    : nextStatuses[0]
  const form = useForm<RequestReviewValues>({
    resolver: zodResolver(requestReviewSchema),
    mode: 'onChange',
    defaultValues: { feedback: '' },
  })
  const mutation = useMutation({
    mutationFn: (input: RequestReviewValues) => {
      if (!targetStatus) {
        throw new Error('No request status transition available')
      }
      return transitionRequest(props.ticket.id, {
        status: targetStatus,
        feedback: input.feedback,
        version: props.ticket.version,
      })
    },
    onSuccess: (detail) => {
      client.setQueryData(
        [...requestQueryKey(userId, 'admin-detail'), props.ticket.id],
        detail
      )
      void client.invalidateQueries({
        queryKey: requestQueryKey(userId, 'admin'),
      })
      form.reset()
      toast.success(t('Request updated'))
    },
    onError: (error) => {
      if (isAxiosError(error) && error.response?.status === 409) {
        setConflict(true)
      }
      handleServerError(error)
    },
  })

  if (!targetStatus) return null

  return (
    <form
      className='flex flex-col gap-4'
      onSubmit={form.handleSubmit((input) => mutation.mutate(input))}
    >
      <h3 className='font-medium'>{t('Update request status')}</h3>
      <FieldGroup>
        <Field data-disabled={mutation.isPending || conflict}>
          <FieldLabel htmlFor='request-next-status'>
            {t('Next status')}
          </FieldLabel>
          <NativeSelect
            id='request-next-status'
            value={targetStatus}
            disabled={mutation.isPending || conflict}
            onChange={(event) =>
              setSelection(event.target.value as RequestStatus)
            }
          >
            {nextStatuses.map((status) => (
              <NativeSelectOption key={status} value={status}>
                {requestStatusLabel(status, t)}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        <Field
          data-invalid={Boolean(form.formState.errors.feedback)}
          data-disabled={mutation.isPending}
        >
          <FieldLabel htmlFor='request-review-feedback'>
            {t('Feedback to customer')}
          </FieldLabel>
          <Textarea
            id='request-review-feedback'
            rows={5}
            maxLength={2000}
            disabled={mutation.isPending}
            aria-invalid={Boolean(form.formState.errors.feedback)}
            aria-describedby='request-feedback-help request-feedback-error'
            {...form.register('feedback')}
          />
          <FieldDescription id='request-feedback-help'>
            {t(
              'Required for every status change. The customer will see this feedback in their request timeline and notifications.'
            )}
          </FieldDescription>
          {form.formState.errors.feedback && (
            <FieldError id='request-feedback-error'>
              {t('Please provide feedback (1–2000 characters).')}
            </FieldError>
          )}
        </Field>
      </FieldGroup>
      {mutation.isError && (
        <Alert variant='destructive'>
          <AlertDescription>
            {conflict
              ? t(
                  'This request has changed. Refresh and review its latest status before submitting again. Your feedback is kept.'
                )
              : t(
                  'The status could not be updated. Your feedback is kept. Please try again.'
                )}
            {conflict && (
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={async () => {
                  if (await props.onRefresh()) {
                    setConflict(false)
                    mutation.reset()
                  }
                }}
              >
                {t('Refresh')}
              </Button>
            )}
          </AlertDescription>
        </Alert>
      )}
      <div className='flex justify-end'>
        <Button
          type='submit'
          variant={targetStatus === 'rejected' ? 'destructive' : 'default'}
          disabled={!form.formState.isValid || mutation.isPending || conflict}
        >
          {mutation.isPending
            ? t('Saving...')
            : t('Send feedback and update status')}
        </Button>
      </div>
    </form>
  )
}

export function AdminRequestDetail(props: {
  requestId: number
  onClose: () => void
}) {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const query = useQuery({
    queryKey: [...requestQueryKey(userId, 'admin-detail'), props.requestId],
    queryFn: () => getAdminRequest(props.requestId),
    retry: false,
    meta: { errorToast: false },
  })

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
    >
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle className='pr-6 leading-snug break-words'>
            {query.data?.ticket.title ?? t('Request details')}
          </DialogTitle>
          <DialogDescription>
            {t('Request')} #{props.requestId}
          </DialogDescription>
        </DialogHeader>
        {query.isPending && <Skeleton className='h-64 w-full' />}
        {query.isError && (
          <ErrorState
            title={t('Failed to load data')}
            description={
              isRequestTicketUnavailable(query.error)
                ? t(
                    'Requests are temporarily unavailable. Please try again later.'
                  )
                : undefined
            }
            onRetry={() => void query.refetch()}
          />
        )}
        {query.data && !query.isError && (
          <div className='flex min-w-0 flex-col gap-6'>
            <div className='flex flex-col gap-2'>
              <div className='flex flex-wrap items-center gap-3'>
                <RequestStatusBadge status={query.data.ticket.status} />
                <span className='text-muted-foreground text-xs'>
                  {t('User')} #{query.data.ticket.user_id}
                  {query.data.ticket.username
                    ? ` · ${query.data.ticket.username}`
                    : ''}
                </span>
              </div>
              <p className='text-muted-foreground text-sm'>
                {requestStatusDescription(query.data.ticket.status, t)}
              </p>
            </div>
            <section className='flex flex-col gap-3'>
              <h3 className='font-medium'>{t('Original request')}</h3>
              <p className='text-sm leading-relaxed [overflow-wrap:anywhere] break-words whitespace-pre-wrap'>
                {query.data.ticket.description}
              </p>
            </section>
            <Separator />
            <AdminReviewForm
              ticket={query.data.ticket}
              onRefresh={async () => !(await query.refetch()).isError}
            />
            <section className='flex flex-col gap-4'>
              <h3 className='font-medium'>{t('Request timeline')}</h3>
              <RequestTimeline events={query.data.events} />
            </section>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
