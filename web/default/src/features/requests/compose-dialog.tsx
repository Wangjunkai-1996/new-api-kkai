import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Send } from 'lucide-react'
import { useForm, useWatch } from 'react-hook-form'
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
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import { requestQueryKey, submitRequest } from './api'
import { requestSchema, type RequestInput } from './lib/schema'

export function RequestComposeDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const client = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const form = useForm<RequestInput>({
    resolver: zodResolver(requestSchema),
    defaultValues: { title: '', description: '' },
  })
  const description = useWatch({ control: form.control, name: 'description' })
  const mutation = useMutation({
    mutationFn: submitRequest,
    onSuccess: (detail) => {
      client.setQueryData(
        [...requestQueryKey(userId, 'detail'), detail.ticket.id],
        detail
      )
      void client.invalidateQueries({
        queryKey: requestQueryKey(userId, 'list'),
      })
      form.reset()
      props.onOpenChange(false)
      void navigate({
        to: '/requests/$requestId',
        params: { requestId: String(detail.ticket.id) },
        search: { submitted: true },
      })
    },
    onError: (error) => handleServerError(error),
  })

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!mutation.isPending) props.onOpenChange(open)
      }}
    >
      <DialogContent
        className='request-compose-dialog sm:max-w-xl'
        showCloseButton={!mutation.isPending}
      >
        <DialogHeader>
          <DialogTitle>{t('Submit a request')}</DialogTitle>
          <DialogDescription>
            {t(
              'A small idea can make a big difference. Tell us what you need.'
            )}
          </DialogDescription>
        </DialogHeader>
        <form
          className='flex flex-col gap-5'
          onSubmit={form.handleSubmit((input) => mutation.mutate(input))}
        >
          <Field data-invalid={Boolean(form.formState.errors.title)}>
            <FieldLabel htmlFor='request-title'>
              {t('Request title')}
            </FieldLabel>
            <Input
              id='request-title'
              maxLength={80}
              placeholder={t('Describe your idea in one sentence')}
              aria-invalid={Boolean(form.formState.errors.title)}
              aria-describedby='request-title-help'
              {...form.register('title')}
            />
            <FieldDescription id='request-title-help'>
              {t('Up to 80 characters.')}
            </FieldDescription>
            {form.formState.errors.title && (
              <FieldError>
                {t('Enter a request title (1–80 characters).')}
              </FieldError>
            )}
          </Field>
          <Field data-invalid={Boolean(form.formState.errors.description)}>
            <FieldLabel htmlFor='request-description'>
              {t('Request description')}
            </FieldLabel>
            <Textarea
              id='request-description'
              rows={7}
              maxLength={2000}
              placeholder={t(
                'Describe the problem, your use case, and how you would like it improved.'
              )}
              aria-invalid={Boolean(form.formState.errors.description)}
              aria-describedby='request-description-help'
              {...form.register('description')}
            />
            <div className='flex items-start justify-between gap-3'>
              <FieldDescription id='request-description-help'>
                {t(
                  'Do not include passwords, API keys, or other sensitive information.'
                )}
              </FieldDescription>
              <span
                className='text-muted-foreground shrink-0 text-xs tabular-nums'
                aria-hidden='true'
              >
                {description.length}/2000
              </span>
            </div>
            {form.formState.errors.description && (
              <FieldError>
                {t('Enter a request description (1–2,000 characters).')}
              </FieldError>
            )}
          </Field>
          <DialogFooter>
            <Button
              variant='outline'
              type='button'
              disabled={mutation.isPending}
              onClick={() => props.onOpenChange(false)}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='submit'
              disabled={mutation.isPending}
              className='max-sm:h-11'
            >
              <Send aria-hidden='true' />
              {mutation.isPending ? t('Submitting...') : t('Send request')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
