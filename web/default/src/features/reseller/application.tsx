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
import { PublicLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import { getApplications, submitApplication } from './admin-api'
import { resellerQueryKey } from './api'
import { applicationStatusLabel } from './withdrawal-status'

const schema = z.object({
  business_name: z.string().trim().min(1).max(128),
  contact_name: z.string().trim().min(1).max(64),
  contact_email: z.string().trim().email().max(254),
  notes: z.string().trim().max(4096),
})

export function ResellerApplicationPage() {
  const { t, i18n } = useTranslation()
  const client = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const queryKey = resellerQueryKey(userId, 'applications')
  const query = useQuery({ queryKey, queryFn: getApplications })
  const form = useForm({
    resolver: zodResolver(schema),
    defaultValues: {
      business_name: '',
      contact_name: '',
      contact_email: '',
      notes: '',
    },
  })
  const mutation = useMutation({
    mutationFn: submitApplication,
    onSuccess: () => {
      form.reset()
      toast.success(t('Application submitted'))
      void client.invalidateQueries({ queryKey })
    },
    onError: (error) => handleServerError(error),
  })
  const canApply = query.data?.every(
    (application) => application.status === 'rejected'
  )
  return (
    <PublicLayout>
      <section className='mx-auto max-w-3xl space-y-6 py-8'>
        <h1 className='text-2xl font-semibold'>
          {t('Apply to Become a Reseller')}
        </h1>
        {query.isPending && <Skeleton className='h-64 w-full' />}
        {query.isError && (
          <ErrorState
            title={t('Failed to load data')}
            description={query.error.message}
            onRetry={() => void query.refetch()}
          />
        )}
        {query.data?.map((application) => (
          <Card key={application.id}>
            <CardHeader>
              <CardTitle>{application.business_name}</CardTitle>
            </CardHeader>
            <CardContent className='space-y-2 text-sm'>
              <p>
                {applicationStatusLabel(application.status, t)} ·{' '}
                {new Date(application.created_at).toLocaleString(i18n.language)}
              </p>
              {application.rejection_reason && (
                <p className='whitespace-pre-wrap'>
                  {application.rejection_reason}
                </p>
              )}
              {application.status === 'approved' &&
                application.domains?.map((domain) => (
                  <Button
                    key={domain.hostname}
                    render={<a href={`https://${domain.hostname}/reseller`} />}
                  >
                    {t('Reseller Workspace')} · {domain.hostname}
                  </Button>
                ))}
              {application.status === 'approved' &&
                !application.domains?.length && (
                  <p>{t('Awaiting domain assignment')}</p>
                )}
            </CardContent>
          </Card>
        ))}
        {canApply && (
          <form
            className='space-y-4'
            onSubmit={form.handleSubmit((input) => mutation.mutate(input))}
          >
            <div className='space-y-2'>
              <Label htmlFor='business-name'>{t('Business Name')}</Label>
              <Input
                id='business-name'
                maxLength={128}
                {...form.register('business_name')}
                aria-invalid={Boolean(form.formState.errors.business_name)}
              />
              {form.formState.errors.business_name && (
                <p className='text-destructive text-sm'>
                  {t('Please enter a valid value.')}
                </p>
              )}
            </div>
            <div className='space-y-2'>
              <Label htmlFor='contact-name'>{t('Contact Name')}</Label>
              <Input
                id='contact-name'
                maxLength={64}
                {...form.register('contact_name')}
                aria-invalid={Boolean(form.formState.errors.contact_name)}
              />
              {form.formState.errors.contact_name && (
                <p className='text-destructive text-sm'>
                  {t('Please enter a valid value.')}
                </p>
              )}
            </div>
            <div className='space-y-2'>
              <Label htmlFor='contact-email'>{t('Contact Email')}</Label>
              <Input
                id='contact-email'
                type='email'
                maxLength={254}
                {...form.register('contact_email')}
                aria-invalid={Boolean(form.formState.errors.contact_email)}
              />
              {form.formState.errors.contact_email && (
                <p className='text-destructive text-sm'>
                  {t('Please enter a valid value.')}
                </p>
              )}
            </div>
            <div className='space-y-2'>
              <Label htmlFor='application-notes'>{t('Notes')}</Label>
              <Textarea
                id='application-notes'
                maxLength={4096}
                {...form.register('notes')}
              />
            </div>
            <Button type='submit' disabled={mutation.isPending}>
              {t('Submit Application')}
            </Button>
          </form>
        )}
      </section>
    </PublicLayout>
  )
}
