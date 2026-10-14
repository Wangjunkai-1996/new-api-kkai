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
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { ErrorState } from '@/components/error-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'

import { getSalesAccounts, updateSalesAccount } from '../../api'
import { requireInvitationData } from '../../api/result'
import { formatInvitationDate, getInvitationErrorMessage } from '../../format'
import type { SalesAccount } from '../../types'
import { PaginationControls } from '../pagination-controls'

const PAGE_SIZE = 20
const accountSchema = z.object({
  userId: z.number().int().positive(),
  active: z.boolean(),
  note: z.string().trim().max(500),
})

export const SalesAccounts = () => {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [editing, setEditing] = useState<SalesAccount | 'new' | null>(null)
  const query = useQuery({
    queryKey: ['kkai', 'invitations', 'admin', 'sales-accounts', page],
    queryFn: async () =>
      requireInvitationData(
        await getSalesAccounts({ page, pageSize: PAGE_SIZE }),
        'Failed to load sales accounts'
      ),
  })
  return (
    <Card>
      <CardHeader className='flex-row flex-wrap items-center justify-between gap-3'>
        <CardTitle>{t('Sales Accounts')}</CardTitle>
        <Button onClick={() => setEditing('new')}>
          {t('Add Sales Account')}
        </Button>
      </CardHeader>
      <CardContent className='space-y-4'>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Sales accounts keep their existing login and invitation code. Changing sales access does not change past requests.'
          )}
        </p>
        {query.isPending && <Skeleton className='h-56 w-full' />}
        {query.isError && (
          <ErrorState
            title={t('Failed to load sales accounts')}
            description={query.error.message}
            onRetry={() => void query.refetch()}
          />
        )}
        {query.data && (
          <>
            {!query.data.items.length && (
              <p className='text-muted-foreground py-6 text-center text-sm'>
                {t('No sales accounts')}
              </p>
            )}
            {query.data.items.length > 0 && (
              <div className='overflow-x-auto rounded-md border'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('User')}</TableHead>
                      <TableHead>{t('Status')}</TableHead>
                      <TableHead>{t('Note')}</TableHead>
                      <TableHead>{t('Updated At')}</TableHead>
                      <TableHead>{t('Actions')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {query.data.items.map((account) => (
                      <TableRow key={account.userId}>
                        <TableCell>
                          {account.userName}{' '}
                          <span className='text-muted-foreground'>
                            #{account.userId}
                          </span>
                        </TableCell>
                        <TableCell>
                          <Badge
                            variant={account.active ? 'default' : 'outline'}
                          >
                            {account.active ? t('Active') : t('Inactive')}
                          </Badge>
                        </TableCell>
                        <TableCell className='max-w-64 whitespace-normal'>
                          {account.note || '—'}
                        </TableCell>
                        <TableCell>
                          {formatInvitationDate(account.updatedAt)}
                        </TableCell>
                        <TableCell>
                          <Button
                            size='sm'
                            variant='outline'
                            onClick={() => setEditing(account)}
                          >
                            {t('Edit')}
                          </Button>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
            <PaginationControls
              page={page}
              pageSize={PAGE_SIZE}
              total={query.data.total}
              onPageChange={setPage}
            />
          </>
        )}
      </CardContent>
      {editing && (
        <SalesAccountDialog
          account={editing === 'new' ? undefined : editing}
          onClose={() => setEditing(null)}
        />
      )}
    </Card>
  )
}

const SalesAccountDialog = (props: {
  account?: SalesAccount
  onClose: () => void
}) => {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm<z.infer<typeof accountSchema>>({
    resolver: zodResolver(accountSchema),
    defaultValues: {
      userId: props.account?.userId,
      active: props.account?.active ?? true,
      note: props.account?.note ?? '',
    },
  })
  const mutation = useMutation({
    mutationFn: async (input: z.infer<typeof accountSchema>) =>
      requireInvitationData(
        await updateSalesAccount(input),
        'Failed to update sales account'
      ),
    onSuccess: async () => {
      toast.success(t('Sales account updated'))
      await queryClient.invalidateQueries({ queryKey: ['kkai', 'invitations'] })
      props.onClose()
    },
    onError: (error) =>
      toast.error(
        getInvitationErrorMessage(error, t('Failed to update sales account'))
      ),
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) props.onClose()
      }}
    >
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>
            {props.account ? t('Edit Sales Account') : t('Add Sales Account')}
          </DialogTitle>
        </DialogHeader>
        <form
          className='space-y-4'
          onSubmit={form.handleSubmit((data) => mutation.mutate(data))}
        >
          <div className='space-y-2'>
            <Label htmlFor='sales-user-id'>{t('User ID')}</Label>
            <Input
              id='sales-user-id'
              type='number'
              min={1}
              step={1}
              readOnly={Boolean(props.account)}
              aria-invalid={Boolean(form.formState.errors.userId)}
              {...form.register('userId', { valueAsNumber: true })}
            />
            {form.formState.errors.userId && (
              <p className='text-destructive text-sm'>
                {t('Enter a valid user ID')}
              </p>
            )}
          </div>
          <label className='flex items-center gap-2 text-sm'>
            <input type='checkbox' {...form.register('active')} />
            {t('Sales Access Enabled')}
          </label>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Inactive sales accounts can view history but cannot submit new sales requests.'
            )}
          </p>
          <div className='space-y-2'>
            <Label htmlFor='sales-account-note'>{t('Note')}</Label>
            <Textarea
              id='sales-account-note'
              maxLength={500}
              {...form.register('note')}
            />
          </div>
          <DialogFooter>
            <Button
              type='button'
              variant='outline'
              disabled={mutation.isPending}
              onClick={props.onClose}
            >
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
