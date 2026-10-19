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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'
import { getSiteConfigQueryKey } from '@/features/site-config'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import {
  getResellerProfile,
  resellerQueryKey,
  updateResellerProfile,
  uploadResellerLogo,
  type ResellerProfile,
} from './api'

const plainText = z.string().refine((value) => !/[<>]/.test(value))

const brandSchema = z.object({
  site_name: z.string().trim().min(1).max(128),
  contact: plainText.pipe(z.string().max(512)),
  announcement: plainText.pipe(z.string().max(20000)),
  homepage_content: plainText.pipe(z.string().max(50000)),
})

function BrandForm(props: { profile: ResellerProfile }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const form = useForm<z.infer<typeof brandSchema>>({
    resolver: zodResolver(brandSchema),
    defaultValues: props.profile,
  })
  const save = useMutation({
    mutationFn: updateResellerProfile,
    onSuccess: async (data) => {
      form.reset(data)
      client.setQueryData(resellerQueryKey(userId, 'profile'), data)
      await client.invalidateQueries({ queryKey: getSiteConfigQueryKey() })
      toast.success(t('Settings updated successfully'))
    },
    onError: (error) => handleServerError(error),
  })
  const upload = useMutation({
    mutationFn: uploadResellerLogo,
    onSuccess: async () => {
      await Promise.all([
        client.invalidateQueries({
          queryKey: resellerQueryKey(userId, 'profile'),
        }),
        client.invalidateQueries({ queryKey: getSiteConfigQueryKey() }),
      ])
      toast.success(t('Settings updated successfully'))
    },
    onError: (error) => handleServerError(error),
  })

  return (
    <form
      className='max-w-2xl'
      onSubmit={form.handleSubmit((data) => save.mutate(data))}
    >
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor='reseller-site-name'>{t('Site Name')}</FieldLabel>
          <Input
            id='reseller-site-name'
            maxLength={128}
            aria-invalid={Boolean(form.formState.errors.site_name)}
            {...form.register('site_name')}
          />
          {form.formState.errors.site_name && (
            <FieldError>{t('Please enter a valid value.')}</FieldError>
          )}
        </Field>
        <Field>
          <FieldLabel htmlFor='reseller-logo'>{t('Logo')}</FieldLabel>
          {props.profile.logo_url && (
            <img
              src={props.profile.logo_url}
              alt={t('Logo')}
              className='size-16 rounded-md object-contain'
            />
          )}
          <Input
            id='reseller-logo'
            type='file'
            accept='image/jpeg,image/png,image/webp'
            disabled={upload.isPending}
            onChange={(event) => {
              const file = event.target.files?.[0]
              event.target.value = ''
              if (!file) return
              if (
                !['image/jpeg', 'image/png', 'image/webp'].includes(
                  file.type
                ) ||
                file.size > 2 * 1024 * 1024
              ) {
                toast.error(
                  t(
                    'Use a JPEG, PNG or WebP image up to 2 MB and 1024 × 1024 pixels.'
                  )
                )
                return
              }
              upload.mutate(file)
            }}
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              'Use a JPEG, PNG or WebP image up to 2 MB and 1024 × 1024 pixels.'
            )}
          </p>
        </Field>
        <Field>
          <FieldLabel htmlFor='reseller-contact'>
            {t('Contact Information')}
          </FieldLabel>
          <Textarea
            id='reseller-contact'
            maxLength={512}
            aria-invalid={Boolean(form.formState.errors.contact)}
            {...form.register('contact')}
          />
          {form.formState.errors.contact && (
            <FieldError>{t('Please enter a valid value.')}</FieldError>
          )}
        </Field>
        <Field>
          <FieldLabel htmlFor='reseller-announcement'>
            {t('Announcement')}
          </FieldLabel>
          <Textarea
            id='reseller-announcement'
            maxLength={20000}
            rows={5}
            aria-invalid={Boolean(form.formState.errors.announcement)}
            {...form.register('announcement')}
          />
          {form.formState.errors.announcement && (
            <FieldError>{t('Please enter a valid value.')}</FieldError>
          )}
        </Field>
        <Field>
          <FieldLabel htmlFor='reseller-homepage'>
            {t('Homepage Introduction')}
          </FieldLabel>
          <Textarea
            id='reseller-homepage'
            maxLength={50000}
            rows={8}
            aria-invalid={Boolean(form.formState.errors.homepage_content)}
            {...form.register('homepage_content')}
          />
          {form.formState.errors.homepage_content && (
            <FieldError>{t('Please enter a valid value.')}</FieldError>
          )}
          <p className='text-muted-foreground text-sm'>
            {t('Content is displayed as plain text.')}
          </p>
        </Field>
        <Button
          type='submit'
          disabled={
            save.isPending || upload.isPending || !form.formState.isDirty
          }
        >
          {t('Save Changes')}
        </Button>
      </FieldGroup>
    </form>
  )
}

export function BrandPanel() {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const query = useQuery({
    queryKey: resellerQueryKey(userId, 'profile'),
    queryFn: getResellerProfile,
  })
  if (query.isPending) return <Skeleton className='h-80 w-full' />
  if (query.isError) {
    return (
      <ErrorState
        title={t('Failed to load data')}
        description={query.error.message}
        onRetry={() => void query.refetch()}
      />
    )
  }
  return <BrandForm profile={query.data} />
}
