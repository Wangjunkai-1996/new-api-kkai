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

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

import {
  createRebateRule,
  getInvitationUserGroups,
  updateRebateRule,
} from '../../api'
import { requireInvitationData } from '../../api/result'
import { getInvitationErrorMessage } from '../../format'
import { rebateRuleSchema, type RebateRuleFormValues } from '../../schemas'
import { ALL_USER_GROUP, type RebateRule } from '../../types'
import { RebateRuleFields } from './rebate-rule-fields'

export const RebateRuleDialog = (props: {
  open: boolean
  rule: RebateRule | null
  onClose: () => void
}) => {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm<RebateRuleFormValues>({
    resolver: zodResolver(rebateRuleSchema),
    defaultValues: {
      user_group: ALL_USER_GROUP,
      rule_type: 'subscription',
      rebate_rate: '',
    },
  })
  const groupsQuery = useQuery({
    queryKey: ['kkai', 'invitations', 'admin', 'user-groups'],
    queryFn: async () =>
      requireInvitationData(
        await getInvitationUserGroups(),
        'Failed to load user groups'
      ),
    enabled: props.open,
  })
  const mutation = useMutation({
    mutationFn: async (values: RebateRuleFormValues) => {
      const data = {
        ...values,
        rebate_rate: String(Number(values.rebate_rate) / 100),
      }
      const response = props.rule
        ? await updateRebateRule(props.rule.id, data)
        : await createRebateRule(data)
      return requireInvitationData(response, 'Failed to save rebate rule')
    },
    onSuccess: async () => {
      toast.success(t('Rebate rule saved'))
      await queryClient.invalidateQueries({
        queryKey: ['kkai', 'invitations', 'admin', 'rules'],
      })
      props.onClose()
    },
    onError: (error) =>
      toast.error(
        getInvitationErrorMessage(error, t('Failed to save rebate rule'))
      ),
  })

  useEffect(() => {
    form.reset(
      props.rule
        ? {
            user_group: props.rule.user_group,
            rule_type: props.rule.rule_type,
            rebate_rate: String(Number(props.rule.rebate_rate) * 100),
          }
        : {
            user_group: ALL_USER_GROUP,
            rule_type: 'subscription',
            rebate_rate: '',
          }
    )
  }, [form, props.rule, props.open])

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
    >
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>
            {props.rule ? t('Edit Rebate Rule') : t('Create Rebate Rule')}
          </DialogTitle>
        </DialogHeader>
        <form
          className='space-y-4'
          onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
        >
          <RebateRuleFields form={form} groups={groupsQuery.data} />
          <DialogFooter>
            <Button type='button' variant='outline' onClick={props.onClose}>
              {t('Cancel')}
            </Button>
            <Button type='submit' disabled={mutation.isPending}>
              {mutation.isPending ? t('Saving...') : t('Save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
