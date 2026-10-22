import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Bell, MessageSquarePlus, Send } from 'lucide-react'
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
  const [title, description] = useWatch({
    control: form.control,
    name: ['title', 'description'],
  })
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
        className='request-compose-dialog gap-0 rounded-[15px] p-0 max-sm:top-0 max-sm:max-h-dvh max-sm:min-h-dvh max-sm:max-w-full max-sm:translate-y-0 max-sm:grid-rows-[auto_1fr] max-sm:rounded-none sm:max-w-[570px] [&_[data-slot=dialog-close]]:top-5 [&_[data-slot=dialog-close]]:right-5 max-sm:[&_[data-slot=dialog-close]]:size-11'
        showCloseButton={!mutation.isPending}
      >
        <DialogHeader className='gap-0 px-5 pt-6 sm:px-[26px]'>
          <div
            className='bg-primary/8 text-primary mb-[15px] flex size-[34px] items-center justify-center rounded-[10px]'
            aria-hidden='true'
          >
            <MessageSquarePlus className='size-[18px]' />
          </div>
          <DialogTitle className='text-xl leading-snug tracking-tight'>
            {t('Submit a request')}
          </DialogTitle>
          <DialogDescription className='mt-[7px] text-xs leading-relaxed'>
            {t(
              'A small idea can make a big difference. Tell us what you need.'
            )}
          </DialogDescription>
        </DialogHeader>
        <form
          className='flex flex-col'
          onSubmit={form.handleSubmit((input) => mutation.mutate(input))}
        >
          <div className='space-y-[22px] px-5 py-[26px] sm:px-[26px]'>
            <Field
              className='gap-0'
              data-invalid={Boolean(form.formState.errors.title)}
            >
              <div className='mb-[9px] flex items-center justify-between gap-3'>
                <FieldLabel className='text-xs' htmlFor='request-title'>
                  {t('Request title')}
                </FieldLabel>
                <span
                  className='text-muted-foreground text-[11px] tabular-nums'
                  aria-hidden='true'
                >
                  {title.length}/80
                </span>
              </div>
              <Input
                id='request-title'
                className='h-10 px-3 md:text-[13px]'
                maxLength={80}
                placeholder={t('Describe your idea in one sentence')}
                aria-invalid={Boolean(form.formState.errors.title)}
                aria-describedby='request-title-help'
                {...form.register('title')}
              />
              <FieldDescription
                id='request-title-help'
                className='mt-2 text-[11px]'
              >
                {t('Up to 80 characters.')}
              </FieldDescription>
              {form.formState.errors.title && (
                <FieldError className='mt-2'>
                  {t('Enter a request title (1–80 characters).')}
                </FieldError>
              )}
            </Field>
            <Field
              className='gap-0'
              data-invalid={Boolean(form.formState.errors.description)}
            >
              <div className='mb-[9px] flex items-center justify-between gap-3'>
                <FieldLabel className='text-xs' htmlFor='request-description'>
                  {t('Request description')}
                </FieldLabel>
                <span
                  className='text-muted-foreground text-[11px] tabular-nums'
                  aria-hidden='true'
                >
                  {description.length}/2000
                </span>
              </div>
              <Textarea
                id='request-description'
                className='min-h-[170px] px-3 py-2.5 leading-[1.85] max-sm:min-h-[230px] md:text-[13px]'
                rows={7}
                maxLength={2000}
                placeholder={t(
                  'Describe the problem, your use case, and how you would like it improved.'
                )}
                aria-invalid={Boolean(form.formState.errors.description)}
                aria-describedby='request-description-help'
                {...form.register('description')}
              />
              <FieldDescription
                id='request-description-help'
                className='mt-2 text-[11px] leading-relaxed'
              >
                {t(
                  'Do not include passwords, API keys, or other sensitive information.'
                )}
              </FieldDescription>
              {form.formState.errors.description && (
                <FieldError className='mt-2'>
                  {t('Enter a request description (1–2,000 characters).')}
                </FieldError>
              )}
            </Field>
          </div>
          <DialogFooter className='m-0 flex-col gap-4 rounded-b-[15px] px-5 py-4 max-sm:mt-auto max-sm:rounded-none sm:items-center sm:justify-between sm:px-[26px]'>
            <p className='text-muted-foreground flex items-start gap-1.5 text-[11px] leading-relaxed sm:max-w-56'>
              <Bell className='mt-0.5 size-3 shrink-0' aria-hidden='true' />
              {t('Progress updates will arrive in your notifications.')}
            </p>
            <div className='flex gap-2 max-sm:w-full'>
              <Button
                className='max-sm:h-11 max-sm:flex-1'
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
                className='max-sm:h-11 max-sm:flex-1'
              >
                <Send aria-hidden='true' />
                {mutation.isPending ? t('Submitting...') : t('Send request')}
              </Button>
            </div>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
