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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import {
  provisionDomain,
  updateTenantStatus,
  type ManagedTenant,
} from './admin-api'
import { resellerQueryKey } from './api'

const domainSchema = z.object({
  hostname: z
    .string()
    .trim()
    .min(1)
    .max(253)
    .regex(/^[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]$/),
})

function TenantCard(props: { tenant: ManagedTenant }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [confirmStatus, setConfirmStatus] = useState(false)
  const form = useForm({
    resolver: zodResolver(domainSchema),
    defaultValues: { hostname: '' },
  })
  const domain = useMutation({
    mutationFn: (data: z.infer<typeof domainSchema>) =>
      provisionDomain(props.tenant.id, data.hostname.toLowerCase()),
    onSuccess: () => {
      form.reset()
      toast.success(t('Saved successfully'))
      void client.invalidateQueries({
        queryKey: resellerQueryKey(userId, 'admin'),
      })
    },
    onError: (error) => handleServerError(error),
  })
  const status = useMutation({
    mutationFn: () =>
      updateTenantStatus(
        props.tenant.id,
        props.tenant.status === 'active' ? 'disabled' : 'active'
      ),
    onSuccess: () => {
      setConfirmStatus(false)
      void client.invalidateQueries({
        queryKey: resellerQueryKey(userId, 'admin'),
      })
    },
    onError: (error) => handleServerError(error),
  })
  let statusLabel = t('Disabled')
  if (props.tenant.status === 'active') statusLabel = t('Active')
  if (props.tenant.status === 'provisioning') statusLabel = t('Provisioning')
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          {props.tenant.display_name}{' '}
          <span className='text-muted-foreground text-sm'>
            #{props.tenant.id}
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent className='space-y-4'>
        <div className='flex flex-wrap items-center gap-3'>
          <p>{statusLabel}</p>
          <Button
            variant='outline'
            disabled={
              status.isPending ||
              (props.tenant.status === 'provisioning' &&
                props.tenant.domains.length === 0)
            }
            onClick={() => setConfirmStatus(true)}
          >
            {props.tenant.status === 'active'
              ? t('Disable Site')
              : t('Enable Site')}
          </Button>
          {props.tenant.status === 'provisioning' &&
            props.tenant.domains.length === 0 && (
              <p className='text-muted-foreground text-sm'>
                {t('Assign a domain before activating this site.')}
              </p>
            )}
        </div>
        <ul className='space-y-1 text-sm break-all'>
          {props.tenant.domains.map((domain) => (
            <li key={domain.hostname}>
              {domain.hostname} ·{' '}
              {domain.status === 'active' ? t('Active') : t('Disabled')}
            </li>
          ))}
        </ul>
        <form
          onSubmit={form.handleSubmit((data) => domain.mutate(data))}
          className='flex flex-wrap items-end gap-2'
        >
          <div className='min-w-48 flex-1 space-y-2'>
            <Label htmlFor={`domain-${props.tenant.id}`}>{t('Hostname')}</Label>
            <Input
              id={`domain-${props.tenant.id}`}
              autoComplete='off'
              placeholder='agent.example.com'
              {...form.register('hostname')}
              aria-invalid={Boolean(form.formState.errors.hostname)}
            />
            {form.formState.errors.hostname && (
              <p className='text-destructive text-sm'>
                {t('Please enter a valid value.')}
              </p>
            )}
          </div>
          <Button type='submit' disabled={domain.isPending}>
            {t('Assign Domain')}
          </Button>
        </form>
      </CardContent>
      <ConfirmDialog
        open={confirmStatus}
        onOpenChange={(open) => {
          if (!status.isPending) setConfirmStatus(open)
        }}
        title={
          props.tenant.status === 'active'
            ? t('Disable Site')
            : t('Enable Site')
        }
        desc={
          props.tenant.status === 'active'
            ? t(
                'Disabling this site blocks new registrations, payments and API requests.'
              )
            : props.tenant.display_name
        }
        destructive={props.tenant.status === 'active'}
        isLoading={status.isPending}
        handleConfirm={() => status.mutate()}
      />
    </Card>
  )
}

export function AdminTenants(props: { tenants: ManagedTenant[] }) {
  const { t } = useTranslation()
  if (!props.tenants.length) {
    return <p className='text-muted-foreground p-4'>{t('No data available')}</p>
  }
  return (
    <div className='grid gap-4 lg:grid-cols-2'>
      {props.tenants.map((tenant) => (
        <TenantCard key={tenant.id} tenant={tenant} />
      ))}
    </div>
  )
}
