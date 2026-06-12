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
import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { RotateCcw, Save } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { getSystemConfig, updateSystemConfig } from '../../api'
import { getInvitationErrorMessage } from '../../lib/error'
import { formatRebateAmount } from '../../lib/format'
import type { SystemConfig } from '../../types'

const formSchema = z.object({
  minRebateRequestAmount: z
    .number()
    .int()
    .min(0, 'Minimum rebate request amount must be at least 0'),
  rebateRequestFrequencyDays: z
    .number()
    .int()
    .min(0, 'Rebate request frequency must be at least 0 days'),
  userInvitationRebateEnabled: z.boolean(),
  orderRebateEnabled: z.boolean(),
  invitationSignupRewardEnabled: z.boolean(),
  invitationSignupRewardAmount: z
    .number()
    .int()
    .min(0, 'Invitation signup reward amount must be at least 0'),
  invitationSignupInviterRewardAmount: z
    .number()
    .int()
    .min(0, 'Inviter reward amount must be at least 0'),
  invitationSignupInviteeRewardAmount: z
    .number()
    .int()
    .min(0, 'Invitee reward amount must be at least 0'),
  invitationSignupRewardReviewRequired: z.boolean(),
  invitationSignupInviterRewardRequiresPaidOrder: z.boolean(),
  invitationSignupInviteeRewardRequiresPaidOrder: z.boolean(),
  rebateToBalanceEnabled: z.boolean(),
})

type FormData = z.infer<typeof formSchema>

function systemConfigToFormData(config: SystemConfig): FormData {
  return {
    minRebateRequestAmount: config.minRebateRequestAmount,
    rebateRequestFrequencyDays: config.rebateRequestFrequencyDays,
    userInvitationRebateEnabled: config.userInvitationRebateEnabled,
    orderRebateEnabled: config.orderRebateEnabled,
    invitationSignupRewardEnabled: config.invitationSignupRewardEnabled,
    invitationSignupRewardAmount: config.invitationSignupRewardAmount,
    invitationSignupInviterRewardAmount:
      config.invitationSignupInviterRewardAmount ??
      config.invitationSignupRewardAmount,
    invitationSignupInviteeRewardAmount:
      config.invitationSignupInviteeRewardAmount ??
      config.invitationSignupRewardAmount,
    invitationSignupRewardReviewRequired:
      config.invitationSignupRewardReviewRequired,
    invitationSignupInviterRewardRequiresPaidOrder:
      config.invitationSignupInviterRewardRequiresPaidOrder,
    invitationSignupInviteeRewardRequiresPaidOrder:
      config.invitationSignupInviteeRewardRequiresPaidOrder,
    rebateToBalanceEnabled: config.rebateToBalanceEnabled,
  }
}

export function SystemConfigCard() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const { data: configData, isLoading } = useQuery({
    queryKey: ['systemConfig'],
    queryFn: async () => {
      const response = await getSystemConfig()
      return response.data
    },
  })

  const {
    register,
    handleSubmit,
    formState: { errors },
    reset,
    setValue,
    watch,
  } = useForm<FormData>({
    resolver: zodResolver(formSchema),
    values: configData ? systemConfigToFormData(configData) : undefined,
  })

  const userInvitationRebateEnabled = watch('userInvitationRebateEnabled')
  const orderRebateEnabled = watch('orderRebateEnabled')
  const invitationSignupRewardEnabled = watch('invitationSignupRewardEnabled')
  const invitationSignupRewardReviewRequired = watch(
    'invitationSignupRewardReviewRequired'
  )
  const invitationSignupInviterRewardRequiresPaidOrder = watch(
    'invitationSignupInviterRewardRequiresPaidOrder'
  )
  const invitationSignupInviteeRewardRequiresPaidOrder = watch(
    'invitationSignupInviteeRewardRequiresPaidOrder'
  )
  const rebateToBalanceEnabled = watch('rebateToBalanceEnabled')

  const updateMutation = useMutation({
    mutationFn: (data: SystemConfig) => updateSystemConfig(data),
    onSuccess: () => {
      toast.success(t('System configuration updated successfully'))
      queryClient.invalidateQueries({ queryKey: ['systemConfig'] })
      queryClient.invalidateQueries({ queryKey: ['invitationFeatureStatus'] })
    },
    onError: (error: unknown) => {
      toast.error(
        getInvitationErrorMessage(
          error,
          t('Failed to update system configuration')
        )
      )
    },
  })

  const onSubmit = (data: FormData) => {
    updateMutation.mutate({
      ...data,
      invitationSignupRewardAmount:
        data.invitationSignupInviterRewardAmount === 0 &&
        data.invitationSignupInviteeRewardAmount === 0
          ? 0
          : Math.max(
              data.invitationSignupInviterRewardAmount,
              data.invitationSignupInviteeRewardAmount
            ),
    })
  }

  const handleReset = () => {
    if (configData) {
      reset(systemConfigToFormData(configData))
      return
    }
    reset()
  }

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{t('System Configuration')}</CardTitle>
        </CardHeader>
        <CardContent>
          <div className='text-muted-foreground'>{t('Loading...')}</div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <form onSubmit={handleSubmit(onSubmit)}>
        <CardHeader className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
          <CardTitle>{t('System Configuration')}</CardTitle>
          <div className='flex items-center gap-2'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={handleReset}
              disabled={updateMutation.isPending}
            >
              <RotateCcw className='mr-2 size-4' />
              {t('Reset')}
            </Button>
            <Button type='submit' size='sm' disabled={updateMutation.isPending}>
              <Save className='mr-2 size-4' />
              {updateMutation.isPending ? t('Saving...') : t('Save')}
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <div className='grid gap-4 sm:grid-cols-2'>
            <div className='space-y-2'>
              <Label htmlFor='minRebateRequestAmount'>
                {t('Minimum Rebate Request Amount')} ({t('cents')})
              </Label>
              <Input
                id='minRebateRequestAmount'
                type='number'
                min='0'
                {...register('minRebateRequestAmount', {
                  valueAsNumber: true,
                })}
              />
              {errors.minRebateRequestAmount && (
                <p className='text-destructive text-sm'>
                  {errors.minRebateRequestAmount.message}
                </p>
              )}
              <p className='text-muted-foreground text-sm'>
                {t('Amount in cents (100 = {{amount}})', {
                  amount: formatRebateAmount(100),
                })}
              </p>
            </div>

            <div className='space-y-2'>
              <Label htmlFor='rebateRequestFrequencyDays'>
                {t('Rebate Request Frequency (Days)')}
              </Label>
              <Input
                id='rebateRequestFrequencyDays'
                type='number'
                min='0'
                {...register('rebateRequestFrequencyDays', {
                  valueAsNumber: true,
                })}
              />
              {errors.rebateRequestFrequencyDays && (
                <p className='text-destructive text-sm'>
                  {errors.rebateRequestFrequencyDays.message}
                </p>
              )}
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Minimum days between rebate requests; 0 disables the limit'
                )}
              </p>
            </div>

            <div className='space-y-2 sm:col-span-2'>
              <Label htmlFor='userInvitationRebateEnabled'>
                {t('User Invitation Rebate')}
              </Label>
              <div className='border-input flex min-h-10 items-center justify-between rounded-md border px-3 py-2'>
                <div className='space-y-1'>
                  <div className='text-sm font-medium'>
                    {userInvitationRebateEnabled ? t('Enabled') : t('Disabled')}
                  </div>
                  <p className='text-muted-foreground text-sm'>
                    {t('Show invitation rebate to users')}
                  </p>
                </div>
                <Switch
                  id='userInvitationRebateEnabled'
                  checked={userInvitationRebateEnabled === true}
                  onCheckedChange={(checked) =>
                    setValue('userInvitationRebateEnabled', checked, {
                      shouldDirty: true,
                      shouldValidate: true,
                    })
                  }
                />
              </div>
            </div>

            <div className='space-y-2'>
              <Label htmlFor='orderRebateEnabled'>{t('Order Rebate')}</Label>
              <div className='border-input flex min-h-10 items-center justify-between rounded-md border px-3 py-2'>
                <div className='space-y-1'>
                  <div className='text-sm font-medium'>
                    {orderRebateEnabled ? t('Enabled') : t('Disabled')}
                  </div>
                  <p className='text-muted-foreground text-sm'>
                    {t('Generate rebates from top-up and subscription orders')}
                  </p>
                </div>
                <Switch
                  id='orderRebateEnabled'
                  checked={orderRebateEnabled === true}
                  onCheckedChange={(checked) =>
                    setValue('orderRebateEnabled', checked, {
                      shouldDirty: true,
                      shouldValidate: true,
                    })
                  }
                />
              </div>
            </div>

            <div className='space-y-2'>
              <Label htmlFor='rebateToBalanceEnabled'>
                {t('Rebate to Balance')}
              </Label>
              <div className='border-input flex min-h-10 items-center justify-between rounded-md border px-3 py-2'>
                <div className='space-y-1'>
                  <div className='text-sm font-medium'>
                    {rebateToBalanceEnabled ? t('Enabled') : t('Disabled')}
                  </div>
                  <p className='text-muted-foreground text-sm'>
                    {t('Allow users to claim rebates to balance')}
                  </p>
                </div>
                <Switch
                  id='rebateToBalanceEnabled'
                  checked={rebateToBalanceEnabled === true}
                  onCheckedChange={(checked) =>
                    setValue('rebateToBalanceEnabled', checked, {
                      shouldDirty: true,
                      shouldValidate: true,
                    })
                  }
                />
              </div>
            </div>

            <div className='space-y-2'>
              <Label htmlFor='invitationSignupInviterRewardAmount'>
                {t('Inviter Signup Reward Amount')} ({t('cents')})
              </Label>
              <Input
                id='invitationSignupInviterRewardAmount'
                type='number'
                min='0'
                {...register('invitationSignupInviterRewardAmount', {
                  valueAsNumber: true,
                })}
              />
              {errors.invitationSignupInviterRewardAmount && (
                <p className='text-destructive text-sm'>
                  {errors.invitationSignupInviterRewardAmount.message}
                </p>
              )}
              <p className='text-muted-foreground text-sm'>
                {t('Amount in cents (200 = {{amount}})', {
                  amount: formatRebateAmount(200),
                })}
              </p>
            </div>

            <div className='space-y-2'>
              <Label htmlFor='invitationSignupInviteeRewardAmount'>
                {t('Invitee Signup Reward Amount')} ({t('cents')})
              </Label>
              <Input
                id='invitationSignupInviteeRewardAmount'
                type='number'
                min='0'
                {...register('invitationSignupInviteeRewardAmount', {
                  valueAsNumber: true,
                })}
              />
              {errors.invitationSignupInviteeRewardAmount && (
                <p className='text-destructive text-sm'>
                  {errors.invitationSignupInviteeRewardAmount.message}
                </p>
              )}
              <p className='text-muted-foreground text-sm'>
                {t('Amount in cents (200 = {{amount}})', {
                  amount: formatRebateAmount(200),
                })}
              </p>
            </div>

            <div className='space-y-2'>
              <Label htmlFor='invitationSignupRewardEnabled'>
                {t('Invitation Signup Reward')}
              </Label>
              <div className='border-input flex min-h-10 items-center justify-between rounded-md border px-3 py-2'>
                <div className='space-y-1'>
                  <div className='text-sm font-medium'>
                    {invitationSignupRewardEnabled
                      ? t('Enabled')
                      : t('Disabled')}
                  </div>
                  <p className='text-muted-foreground text-sm'>
                    {t('Generate rewards for invited registrations')}
                  </p>
                </div>
                <Switch
                  id='invitationSignupRewardEnabled'
                  checked={invitationSignupRewardEnabled === true}
                  onCheckedChange={(checked) =>
                    setValue('invitationSignupRewardEnabled', checked, {
                      shouldDirty: true,
                      shouldValidate: true,
                    })
                  }
                />
              </div>
            </div>

            <div className='space-y-2 sm:col-span-2'>
              <Label htmlFor='invitationSignupRewardReviewRequired'>
                {t('Invitation Signup Reward Review')}
              </Label>
              <div className='border-input flex min-h-10 items-center justify-between rounded-md border px-3 py-2'>
                <div className='space-y-1'>
                  <div className='text-sm font-medium'>
                    {invitationSignupRewardReviewRequired
                      ? t('Review Required')
                      : t('Manual payout only')}
                  </div>
                  <p className='text-muted-foreground text-sm'>
                    {t(
                      'When disabled, claimed invitation rewards skip approval and wait for manual payout'
                    )}
                  </p>
                </div>
                <Switch
                  id='invitationSignupRewardReviewRequired'
                  checked={invitationSignupRewardReviewRequired === true}
                  onCheckedChange={(checked) =>
                    setValue('invitationSignupRewardReviewRequired', checked, {
                      shouldDirty: true,
                      shouldValidate: true,
                    })
                  }
                />
              </div>
            </div>

            <div className='space-y-2'>
              <Label htmlFor='invitationSignupInviterRewardRequiresPaidOrder'>
                {t('Inviter Reward Unlock')}
              </Label>
              <div className='border-input flex min-h-10 items-center justify-between gap-3 rounded-md border px-3 py-2'>
                <div className='space-y-1'>
                  <div className='text-sm font-medium'>
                    {invitationSignupInviterRewardRequiresPaidOrder
                      ? t('Wait for paid order')
                      : t('Immediately claimable')}
                  </div>
                  <p className='text-muted-foreground text-sm'>
                    {t(
                      'Inviter reward waits until the invited user first tops up or subscribes'
                    )}
                  </p>
                </div>
                <Switch
                  id='invitationSignupInviterRewardRequiresPaidOrder'
                  checked={
                    invitationSignupInviterRewardRequiresPaidOrder === true
                  }
                  onCheckedChange={(checked) =>
                    setValue(
                      'invitationSignupInviterRewardRequiresPaidOrder',
                      checked,
                      {
                        shouldDirty: true,
                        shouldValidate: true,
                      }
                    )
                  }
                />
              </div>
            </div>

            <div className='space-y-2'>
              <Label htmlFor='invitationSignupInviteeRewardRequiresPaidOrder'>
                {t('Invitee Reward Unlock')}
              </Label>
              <div className='border-input flex min-h-10 items-center justify-between gap-3 rounded-md border px-3 py-2'>
                <div className='space-y-1'>
                  <div className='text-sm font-medium'>
                    {invitationSignupInviteeRewardRequiresPaidOrder
                      ? t('Wait for paid order')
                      : t('Immediately claimable')}
                  </div>
                  <p className='text-muted-foreground text-sm'>
                    {t(
                      'Invitee reward waits until the invited user first tops up or subscribes'
                    )}
                  </p>
                </div>
                <Switch
                  id='invitationSignupInviteeRewardRequiresPaidOrder'
                  checked={
                    invitationSignupInviteeRewardRequiresPaidOrder === true
                  }
                  onCheckedChange={(checked) =>
                    setValue(
                      'invitationSignupInviteeRewardRequiresPaidOrder',
                      checked,
                      {
                        shouldDirty: true,
                        shouldValidate: true,
                      }
                    )
                  }
                />
              </div>
            </div>
          </div>
        </CardContent>
      </form>
    </Card>
  )
}
