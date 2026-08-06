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
import { UserRoundPlus } from 'lucide-react'
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
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

import {
  getAdminInvitationRegistrations,
  runRegistrationRewardAction,
} from '../../api'
import { requireInvitationData } from '../../api/result'
import { getInvitationErrorMessage } from '../../format'
import { PaginationControls } from '../pagination-controls'
import { InvitationRegistrationTable } from './invitation-registration-table'
import type { RegistrationRewardAction } from './registration-actions'

const PAGE_SIZE = 20
const REGISTRATIONS_KEY = ['kkai', 'invitations', 'admin', 'registrations']

export const InvitationRegistrations = () => {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [action, setAction] = useState<RegistrationRewardAction | null>(null)
  const query = useQuery({
    queryKey: [...REGISTRATIONS_KEY, page],
    queryFn: async () =>
      requireInvitationData(
        await getAdminInvitationRegistrations({ page, pageSize: PAGE_SIZE }),
        'Failed to load invitation registrations'
      ),
  })
  const mutation = useMutation({
    mutationFn: async (input: RegistrationRewardAction) =>
      requireInvitationData(
        await runRegistrationRewardAction(
          input.registration.id,
          input.recipient,
          input.action
        ),
        'Failed to update signup reward'
      ),
    onSuccess: async () => {
      setAction(null)
      toast.success(t('Signup reward updated'))
      await queryClient.invalidateQueries({
        queryKey: ['kkai', 'invitations'],
      })
    },
    onError: (error) =>
      toast.error(
        getInvitationErrorMessage(error, t('Failed to update signup reward'))
      ),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Invitation Registrations')}</CardTitle>
      </CardHeader>
      <CardContent>
        {query.isPending && <Skeleton className='h-72 w-full' />}
        {query.isError && (
          <ErrorState
            title={t('Failed to load invitation registrations')}
            description={query.error.message}
            onRetry={() => void query.refetch()}
          />
        )}
        {query.data?.items.length === 0 && (
          <EmptyState
            icon={UserRoundPlus}
            title={t('No invitation registrations')}
            bordered
          />
        )}
        {query.data && query.data.items.length > 0 && (
          <InvitationRegistrationTable
            registrations={query.data.items}
            onAction={setAction}
          />
        )}
        {query.data && (
          <PaginationControls
            page={page}
            pageSize={PAGE_SIZE}
            total={query.data.total}
            onPageChange={setPage}
          />
        )}
      </CardContent>
      <AlertDialog
        open={Boolean(action)}
        onOpenChange={(open) => !open && !mutation.isPending && setAction(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Confirm signup reward action?')}
            </AlertDialogTitle>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              disabled={mutation.isPending}
              onClick={(event) => {
                event.preventDefault()
                if (action) mutation.mutate(action)
              }}
            >
              {mutation.isPending ? t('Processing...') : t('Confirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  )
}
