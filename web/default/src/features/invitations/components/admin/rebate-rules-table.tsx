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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Edit, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { deleteRebateRule } from '../../api'
import { getInvitationErrorMessage } from '../../lib/error'
import { ALL_USER_GROUP, type RebateRule } from '../../types'

interface RebateRulesTableProps {
  rules: RebateRule[]
  loading: boolean
  onEdit: (id: number) => void
}

export function RebateRulesTable({
  rules,
  loading,
  onEdit,
}: RebateRulesTableProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  // 删除规则
  const deleteMutation = useMutation({
    mutationFn: deleteRebateRule,
    onSuccess: () => {
      toast.success(t('Rule deleted successfully'))
      queryClient.invalidateQueries({ queryKey: ['rebateRules'] })
    },
    onError: (error: unknown) => {
      toast.error(getInvitationErrorMessage(error, t('Failed to delete rule')))
    },
  })

  const handleDelete = (id: number) => {
    if (window.confirm(t('Are you sure you want to delete this rule?'))) {
      deleteMutation.mutate(id)
    }
  }

  const formatRuleType = (type: string) => {
    return type === 'subscription' ? t('Subscription') : t('Top-up')
  }

  const formatUserGroup = (group: string) => {
    return group === ALL_USER_GROUP ? t('All User Groups') : group
  }

  const formatRebateRate = (rate: string) => {
    const percentage = (parseFloat(rate) * 100).toFixed(2)
    return `${percentage}%`
  }

  const formatDate = (timestamp: number) => {
    return new Date(timestamp * 1000).toLocaleString()
  }

  if (loading) {
    return (
      <div className='flex items-center justify-center py-8'>
        <div className='text-muted-foreground'>{t('Loading...')}</div>
      </div>
    )
  }

  if (rules.length === 0) {
    return (
      <div className='flex items-center justify-center py-8'>
        <div className='text-muted-foreground'>{t('No rules found')}</div>
      </div>
    )
  }

  return (
    <div className='overflow-x-auto'>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('User Group')}</TableHead>
            <TableHead>{t('Rule Type')}</TableHead>
            <TableHead>{t('Rebate Rate')}</TableHead>
            <TableHead>{t('Created At')}</TableHead>
            <TableHead className='text-right'>{t('Actions')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rules.map((rule) => (
            <TableRow key={rule.id}>
              <TableCell>
                <Badge variant='secondary'>
                  {formatUserGroup(rule.user_group)}
                </Badge>
              </TableCell>
              <TableCell>{formatRuleType(rule.rule_type)}</TableCell>
              <TableCell className='font-medium'>
                {formatRebateRate(rule.rebate_rate)}
              </TableCell>
              <TableCell className='text-muted-foreground'>
                {formatDate(rule.created_at)}
              </TableCell>
              <TableCell className='text-right'>
                <div className='flex justify-end gap-2'>
                  <Button
                    variant='ghost'
                    size='sm'
                    onClick={() => onEdit(rule.id)}
                  >
                    <Edit className='size-4' />
                  </Button>
                  <Button
                    variant='ghost'
                    size='sm'
                    onClick={() => handleDelete(rule.id)}
                    disabled={deleteMutation.isPending}
                  >
                    <Trash2 className='size-4' />
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
