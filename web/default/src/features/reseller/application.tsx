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
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { ErrorState } from '@/components/error-state'
import { PublicLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  InputGroupText,
} from '@/components/ui/input-group'
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'
import { useSiteConfig } from '@/features/site-config'
import {
  handleServerError,
  markServerErrorHandled,
} from '@/lib/handle-server-error'
import { getServerErrorSources } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { getApplications, submitApplication } from './admin-api'
import { resellerQueryKey } from './api'
import { applicationStatusLabel } from './withdrawal-status'

const schema = z.object({
  business_name: z.string().trim().min(1).max(128),
  desired_domain_prefix: z
    .string()
    .trim()
    .toLowerCase()
    .max(63)
    .refine(
      (value) => value === '' || /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(value)
    ),
  contact_name: z.string().trim().max(64),
  contact_email: z.string().trim().email().max(254),
  notes: z.string().trim().max(4096),
})

export function ResellerApplicationPage() {
  const { t, i18n } = useTranslation()
  const client = useQueryClient()
  const user = useAuthStore((state) => state.auth.user)
  const userId = user?.id
  const queryKey = resellerQueryKey(userId, 'applications')
  const query = useQuery({ queryKey, queryFn: getApplications })
  const site = useSiteConfig()
  const rootDomain = site.data?.reseller_root_domain
  const prefixEnabled =
    site.data?.reseller_domain_prefix_enabled === true && Boolean(rootDomain)
  const form = useForm({
    resolver: zodResolver(schema),
    defaultValues: {
      business_name: '',
      desired_domain_prefix: '',
      contact_name: '',
      contact_email: user?.email ?? '',
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
    onError: (error) => {
      const code = getServerErrorSources(error).find(
        (source) =>
          typeof source.code === 'string' &&
          source.code.startsWith('RESELLER_DOMAIN_PREFIX_')
      )?.code
      if (
        code === 'RESELLER_DOMAIN_PREFIX_INVALID' ||
        code === 'RESELLER_DOMAIN_PREFIX_TAKEN'
      ) {
        markServerErrorHandled(error)
        form.setError(
          'desired_domain_prefix',
          {
            type: 'server',
            message:
              code === 'RESELLER_DOMAIN_PREFIX_TAKEN'
                ? t(
                    'This domain prefix is already assigned. Choose another one.'
                  )
                : t(
                    'This domain prefix is invalid or reserved. Choose another one.'
                  ),
          },
          { shouldFocus: true }
        )
        return
      }
      if (code === 'RESELLER_DOMAIN_PREFIX_UNAVAILABLE') {
        handleServerError(error, undefined, {
          title: t(
            'Domain prefix selection is temporarily unavailable. Leave it blank to submit your application.'
          ),
        })
        return
      }
      handleServerError(error)
    },
  })
  const prefix = useWatch({
    control: form.control,
    name: 'desired_domain_prefix',
  })
    .trim()
    .toLowerCase()
  const canApply = query.data?.every(
    (application) => application.status === 'rejected'
  )
  return (
    <PublicLayout>
      <section className='mx-auto max-w-2xl space-y-6 py-8'>
        <h1 className='text-2xl font-semibold'>
          {t('Apply to Become a Reseller')}
        </h1>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Choose your site name and preferred domain. The final domain is assigned after review.'
          )}
        </p>
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
              {application.desired_domain_prefix && (
                <p>
                  {t('Preferred Domain Prefix')}:{' '}
                  {application.desired_domain_prefix}
                  {rootDomain && `.${rootDomain}`}
                </p>
              )}
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
            className='space-y-5'
            onSubmit={form.handleSubmit((input) => {
              if (prefixEnabled || input.desired_domain_prefix === '') {
                mutation.mutate(input)
              }
            })}
          >
            <FieldGroup>
              <Field
                data-invalid={Boolean(form.formState.errors.business_name)}
              >
                <FieldLabel htmlFor='business-name'>
                  {t('Site Name')}
                </FieldLabel>
                <Input
                  id='business-name'
                  placeholder='KimiAI'
                  maxLength={128}
                  required
                  {...form.register('business_name')}
                  aria-invalid={Boolean(form.formState.errors.business_name)}
                />
                {form.formState.errors.business_name && (
                  <FieldError>{t('Please enter a valid value.')}</FieldError>
                )}
              </Field>
              <Field
                data-invalid={Boolean(
                  form.formState.errors.desired_domain_prefix
                )}
              >
                <FieldLabel htmlFor='domain-prefix'>
                  {t('Preferred Domain Prefix (optional)')}
                </FieldLabel>
                <InputGroup>
                  <InputGroupInput
                    id='domain-prefix'
                    placeholder='kimi'
                    autoComplete='off'
                    autoCapitalize='none'
                    spellCheck={false}
                    maxLength={63}
                    disabled={!prefixEnabled}
                    {...form.register('desired_domain_prefix')}
                    aria-invalid={Boolean(
                      form.formState.errors.desired_domain_prefix
                    )}
                    aria-describedby='domain-prefix-help domain-preview'
                  />
                  {rootDomain && (
                    <InputGroupAddon align='inline-end'>
                      <InputGroupText>.{rootDomain}</InputGroupText>
                    </InputGroupAddon>
                  )}
                </InputGroup>
                <FieldDescription id='domain-prefix-help'>
                  {t(
                    'Use 1–63 letters, numbers or hyphens. Leave blank for us to assign a domain.'
                  )}
                </FieldDescription>
                {rootDomain && (
                  <output
                    id='domain-preview'
                    className='text-sm break-all'
                    aria-live='polite'
                  >
                    https://{prefix || 'kimi'}.{rootDomain}
                  </output>
                )}
                {form.formState.errors.desired_domain_prefix && (
                  <FieldError>
                    {form.formState.errors.desired_domain_prefix.type ===
                    'server'
                      ? form.formState.errors.desired_domain_prefix.message
                      : t(
                          'This domain prefix is invalid or reserved. Choose another one.'
                        )}
                  </FieldError>
                )}
              </Field>
              <Field
                data-invalid={Boolean(form.formState.errors.contact_email)}
              >
                <FieldLabel htmlFor='contact-email'>
                  {t('Contact Email')}
                </FieldLabel>
                <Input
                  id='contact-email'
                  type='email'
                  maxLength={254}
                  required
                  autoComplete='email'
                  {...form.register('contact_email')}
                  aria-invalid={Boolean(form.formState.errors.contact_email)}
                />
                {form.formState.errors.contact_email && (
                  <FieldError>{t('Please enter a valid value.')}</FieldError>
                )}
                <FieldDescription>
                  {t(
                    "Used for this application and as your site's initial contact email."
                  )}
                </FieldDescription>
              </Field>
              <details>
                <summary className='cursor-pointer text-sm'>
                  {t('Additional Information (optional)')}
                </summary>
                <FieldGroup className='mt-4'>
                  <Field>
                    <FieldLabel htmlFor='contact-name'>
                      {t('Contact Name')}
                    </FieldLabel>
                    <Input
                      id='contact-name'
                      maxLength={64}
                      autoComplete='name'
                      {...form.register('contact_name')}
                    />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor='application-notes'>
                      {t('Notes')}
                    </FieldLabel>
                    <Textarea
                      id='application-notes'
                      maxLength={4096}
                      {...form.register('notes')}
                    />
                  </Field>
                </FieldGroup>
              </details>
            </FieldGroup>
            {!site.isPending && !prefixEnabled && (
              <p role='status' className='text-muted-foreground text-sm'>
                {t(
                  'Domain prefix selection is temporarily unavailable. Leave it blank to submit your application.'
                )}
              </p>
            )}
            <Button
              type='submit'
              disabled={
                mutation.isPending || (!prefixEnabled && Boolean(prefix))
              }
            >
              {t('Submit Application')}
            </Button>
          </form>
        )}
      </section>
    </PublicLayout>
  )
}
