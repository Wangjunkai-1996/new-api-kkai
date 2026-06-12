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
import { useEffect } from 'react'
import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  createRebateRule,
  updateRebateRule,
  getRebateRules,
  getUserGroups,
} from '../../api'
import { getInvitationErrorMessage } from '../../lib/error'
import { ALL_USER_GROUP, type RebateRuleFormData } from '../../types'

const formSchema = z.object({
  user_group: z.string().min(1, 'User group is required'),
  rule_type: z.enum(['subscription', 'topup']),
  rebate_rate: z
    .string()
    .min(1, 'Rebate rate is required')
    .refine(
      (val) => {
        const num = parseFloat(val)
        return !isNaN(num) && num >= 0 && num <= 100
      },
      { message: 'Rebate rate must be between 0 and 100' }
    ),
})

type FormData = z.infer<typeof formSchema>

interface RebateRuleDialogProps {
  open: boolean
  onClose: () => void
  editingRuleId: number | null
}

export function RebateRuleDialog({
  open,
  onClose,
  editingRuleId,
}: RebateRuleDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const {
    register,
    handleSubmit,
    formState: { errors },
    reset,
    setValue,
    watch,
  } = useForm<FormData>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      user_group: ALL_USER_GROUP,
      rule_type: 'subscription',
      rebate_rate: '',
    },
  })

  const ruleType = watch('rule_type')
  const userGroup = watch('user_group')

  // 获取用户组列表
  const { data: userGroupsData } = useQuery({
    queryKey: ['userGroups'],
    queryFn: async () => {
      const response = await getUserGroups()
      return response.data ?? []
    },
    enabled: open,
  })

  // 获取编辑的规则数据
  const { data: rulesData } = useQuery({
    queryKey: ['rebateRules'],
    queryFn: async () => {
      const response = await getRebateRules()
      return response.data ?? []
    },
    enabled: open && editingRuleId !== null,
  })

  // 创建规则
  const createMutation = useMutation({
    mutationFn: (data: RebateRuleFormData) => createRebateRule(data),
    onSuccess: () => {
      toast.success(t('Rule created successfully'))
      queryClient.invalidateQueries({ queryKey: ['rebateRules'] })
      onClose()
      reset()
    },
    onError: (error: unknown) => {
      toast.error(getInvitationErrorMessage(error, t('Failed to create rule')))
    },
  })

  // 更新规则
  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: RebateRuleFormData }) =>
      updateRebateRule(id, data),
    onSuccess: () => {
      toast.success(t('Rule updated successfully'))
      queryClient.invalidateQueries({ queryKey: ['rebateRules'] })
      onClose()
      reset()
    },
    onError: (error: unknown) => {
      toast.error(getInvitationErrorMessage(error, t('Failed to update rule')))
    },
  })

  // 加载编辑数据
  useEffect(() => {
    if (editingRuleId && rulesData) {
      const rule = rulesData.find((r) => r.id === editingRuleId)
      if (rule) {
        setValue('user_group', rule.user_group)
        setValue('rule_type', rule.rule_type)
        // 转换为百分比显示
        const percentage = (parseFloat(rule.rebate_rate) * 100).toString()
        setValue('rebate_rate', percentage)
      }
    } else {
      reset()
    }
  }, [editingRuleId, rulesData, setValue, reset])

  const onSubmit = (data: FormData) => {
    // 转换百分比为小数
    const rebateRate = (parseFloat(data.rebate_rate) / 100).toString()
    const formData: RebateRuleFormData = {
      ...data,
      rebate_rate: rebateRate,
    }

    if (editingRuleId) {
      updateMutation.mutate({ id: editingRuleId, data: formData })
    } else {
      createMutation.mutate(formData)
    }
  }

  const isPending = createMutation.isPending || updateMutation.isPending

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className='sm:max-w-[500px]'>
        <DialogHeader>
          <DialogTitle>
            {editingRuleId ? t('Edit Rule') : t('Create Rule')}
          </DialogTitle>
          <DialogDescription>
            {t('Configure rebate rules for different user groups')}
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit(onSubmit)} className='space-y-4'>
          <div className='space-y-2'>
            <Label htmlFor='user_group'>{t('User Group')}</Label>
            <Select
              value={userGroup ?? ''}
              onValueChange={(value) => {
                if (value) setValue('user_group', value)
              }}
            >
              <SelectTrigger id='user_group'>
                <SelectValue placeholder={t('Select user group')} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL_USER_GROUP}>
                  {t('All User Groups')}
                </SelectItem>
                {userGroupsData?.map((group) => (
                  <SelectItem key={group.name} value={group.name}>
                    {group.name} ({group.user_count} {t('users')})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {errors.user_group && (
              <p className='text-destructive text-sm'>
                {errors.user_group.message}
              </p>
            )}
          </div>

          <div className='space-y-2'>
            <Label htmlFor='rule_type'>{t('Rule Type')}</Label>
            <Select
              value={ruleType ?? 'subscription'}
              onValueChange={(value) =>
                setValue('rule_type', value as 'subscription' | 'topup')
              }
            >
              <SelectTrigger id='rule_type'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='subscription'>
                  {t('Subscription')}
                </SelectItem>
                <SelectItem value='topup'>{t('Top-up')}</SelectItem>
              </SelectContent>
            </Select>
            {errors.rule_type && (
              <p className='text-destructive text-sm'>
                {errors.rule_type.message}
              </p>
            )}
          </div>

          <div className='space-y-2'>
            <Label htmlFor='rebate_rate'>{t('Rebate Rate')} (%)</Label>
            <Input
              id='rebate_rate'
              type='number'
              step='0.01'
              min='0'
              max='100'
              placeholder='5.00'
              {...register('rebate_rate')}
            />
            {errors.rebate_rate && (
              <p className='text-destructive text-sm'>
                {errors.rebate_rate.message}
              </p>
            )}
            <p className='text-muted-foreground text-sm'>
              {t('Enter percentage value (0-100)')}
            </p>
          </div>

          <DialogFooter>
            <Button type='button' variant='outline' onClick={onClose}>
              {t('Cancel')}
            </Button>
            <Button type='submit' disabled={isPending}>
              {isPending ? t('Saving...') : t('Save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
