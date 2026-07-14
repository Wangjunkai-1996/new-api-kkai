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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

import { deleteRebateRule, getRebateRules } from '../../api'
import { requireInvitationData } from '../../api/result'
import { getInvitationErrorMessage } from '../../format'
import type { RebateRule } from '../../types'
import { RebateRuleDialog } from './rebate-rule-dialog'
import { RebateRulesTable } from './rebate-rules-table'

const RULES_QUERY_KEY = ['kkai', 'invitations', 'admin', 'rules']

export const RebateRules = () => {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<RebateRule | null>(null)
  const [deletingRule, setDeletingRule] = useState<RebateRule | null>(null)
  const query = useQuery({
    queryKey: RULES_QUERY_KEY,
    queryFn: async () =>
      requireInvitationData(await getRebateRules(), 'Failed to load rules'),
  })
  const deleteMutation = useMutation({
    mutationFn: async (id: number) => {
      const response = await deleteRebateRule(id)
      if (!response.success) throw new Error(response.message)
    },
    onSuccess: async () => {
      setDeletingRule(null)
      toast.success(t('Rebate rule deleted'))
      await queryClient.invalidateQueries({ queryKey: RULES_QUERY_KEY })
    },
    onError: (error) =>
      toast.error(
        getInvitationErrorMessage(error, t('Failed to delete rebate rule'))
      ),
  })

  return (
    <>
      <Card>
        <CardHeader className='flex-row items-center justify-between gap-3'>
          <CardTitle>{t('Rebate Rules')}</CardTitle>
          <Button
            size='sm'
            onClick={() => {
              setEditingRule(null)
              setDialogOpen(true)
            }}
          >
            <Plus aria-hidden='true' />
            {t('Create Rule')}
          </Button>
        </CardHeader>
        <CardContent>
          {query.isPending && <Skeleton className='h-48 w-full' />}
          {query.isError && (
            <ErrorState
              title={t('Failed to load rebate rules')}
              description={query.error.message}
              onRetry={() => void query.refetch()}
            />
          )}
          {query.data?.length === 0 && (
            <EmptyState title={t('No rebate rules')} bordered />
          )}
          {query.data && query.data.length > 0 && (
            <RebateRulesTable
              rules={query.data}
              onEdit={(rule) => {
                setEditingRule(rule)
                setDialogOpen(true)
              }}
              onDelete={setDeletingRule}
            />
          )}
        </CardContent>
      </Card>
      <RebateRuleDialog
        open={dialogOpen}
        rule={editingRule}
        onClose={() => setDialogOpen(false)}
      />
      <AlertDialog
        open={Boolean(deletingRule)}
        onOpenChange={(open) => !open && setDeletingRule(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Delete rebate rule?')}</AlertDialogTitle>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              disabled={deleteMutation.isPending}
              onClick={(event) => {
                event.preventDefault()
                if (deletingRule) deleteMutation.mutate(deletingRule.id)
              }}
            >
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
