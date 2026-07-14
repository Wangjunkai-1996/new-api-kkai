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
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

import {
  getInvitationSystemConfig,
  updateInvitationSystemConfig,
} from '../../api'
import { requireInvitationData } from '../../api/result'
import { getInvitationErrorMessage } from '../../format'
import { invitationSystemConfigSchema } from '../../schemas'
import {
  INVITATION_SYSTEM_CONFIG_DEFAULTS,
  normalizeInvitationSystemConfig,
  prepareInvitationSystemConfig,
} from '../../system-config'
import type { SystemConfig } from '../../types'
import { ConfigNumberField, ConfigSwitch } from './config-fields'

export const SystemConfigForm = () => {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const configQuery = useQuery({
    queryKey: ['kkai', 'invitations', 'admin', 'system-config'],
    queryFn: async () =>
      normalizeInvitationSystemConfig(
        requireInvitationData(
          await getInvitationSystemConfig(),
          'Failed to load invitation settings'
        )
      ),
  })
  const form = useForm<SystemConfig>({
    resolver: zodResolver(invitationSystemConfigSchema),
    defaultValues: INVITATION_SYSTEM_CONFIG_DEFAULTS,
  })
  const mutation = useMutation({
    mutationFn: async (values: SystemConfig) =>
      requireInvitationData(
        await updateInvitationSystemConfig(
          prepareInvitationSystemConfig(values)
        ),
        'Failed to save invitation settings'
      ),
    onSuccess: async (data) => {
      form.reset(data)
      toast.success(t('Invitation settings saved'))
      await queryClient.invalidateQueries({
        queryKey: ['kkai', 'invitations'],
      })
    },
    onError: (error) =>
      toast.error(
        getInvitationErrorMessage(
          error,
          t('Failed to save invitation settings')
        )
      ),
  })

  useEffect(() => {
    if (configQuery.data) form.reset(configQuery.data)
  }, [configQuery.data, form])

  if (configQuery.isPending) return <Skeleton className='h-96 w-full' />
  if (configQuery.isError) {
    return (
      <ErrorState
        title={t('Failed to load invitation settings')}
        description={configQuery.error.message}
        onRetry={() => void configQuery.refetch()}
      />
    )
  }

  const errors = form.formState.errors
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Invitation Settings')}</CardTitle>
      </CardHeader>
      <CardContent>
        <form
          className='space-y-6'
          onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
        >
          <div className='grid gap-x-8 lg:grid-cols-2'>
            <div>
              <ConfigSwitch
                control={form.control}
                name='userInvitationRebateEnabled'
                label={t('User invitation rebate')}
              />
              <ConfigSwitch
                control={form.control}
                name='orderRebateEnabled'
                label={t('Order rebate')}
              />
              <ConfigSwitch
                control={form.control}
                name='rebateToBalanceEnabled'
                label={t('Rebate to balance')}
              />
            </div>
            <div>
              <ConfigSwitch
                control={form.control}
                name='invitationSignupRewardEnabled'
                label={t('Invitation signup reward')}
              />
              <ConfigSwitch
                control={form.control}
                name='invitationSignupRewardReviewRequired'
                label={t('Signup reward review required')}
              />
              <ConfigSwitch
                control={form.control}
                name='invitationSignupInviterRewardRequiresPaidOrder'
                label={t('Inviter reward requires paid order')}
              />
              <ConfigSwitch
                control={form.control}
                name='invitationSignupInviteeRewardRequiresPaidOrder'
                label={t('Invitee reward requires paid order')}
              />
            </div>
          </div>
          <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
            <ConfigNumberField
              name='minRebateRequestAmount'
              label={t('Minimum rebate request amount (cents)')}
              register={form.register}
              error={errors.minRebateRequestAmount?.message}
            />
            <ConfigNumberField
              name='rebateRequestFrequencyDays'
              label={t('Rebate request interval (days)')}
              register={form.register}
              error={errors.rebateRequestFrequencyDays?.message}
            />
            <ConfigNumberField
              name='invitationSignupInviterRewardAmount'
              label={t('Inviter reward amount (cents)')}
              register={form.register}
            />
            <ConfigNumberField
              name='invitationSignupInviteeRewardAmount'
              label={t('Invitee reward amount (cents)')}
              register={form.register}
            />
          </div>
          <div className='flex justify-end'>
            <Button type='submit' disabled={mutation.isPending}>
              {mutation.isPending ? t('Saving...') : t('Save')}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}
