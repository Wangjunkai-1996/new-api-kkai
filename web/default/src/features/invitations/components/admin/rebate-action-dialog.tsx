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
import { useMutation, useQueryClient } from '@tanstack/react-query'
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
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import {
  approveRebateRequest,
  rejectRebateRequest,
  completeRebateRequest,
  resetRebateRequestReview,
  undoCompleteRebateRequest,
} from '../../api'
import { getInvitationErrorMessage } from '../../lib/error'
import { formatRebateAmount } from '../../lib/format'
import type { RebateRequestAdmin } from '../../types'

const approveSchema = z.object({
  note: z.string().optional(),
})

const rejectSchema = z.object({
  reason: z.string().min(1, 'Reject reason is required'),
  note: z.string().optional(),
})

type ApproveFormData = z.infer<typeof approveSchema>
type RejectFormData = z.infer<typeof rejectSchema>

interface RebateActionDialogProps {
  open: boolean
  onClose: () => void
  request: RebateRequestAdmin | null
  actionType: 'approve' | 'reject' | 'reset' | 'complete' | 'undoComplete'
}

export function RebateActionDialog({
  open,
  onClose,
  request,
  actionType,
}: RebateActionDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const approveForm = useForm<ApproveFormData>({
    resolver: zodResolver(approveSchema),
    defaultValues: { note: '' },
  })

  const rejectForm = useForm<RejectFormData>({
    resolver: zodResolver(rejectSchema),
    defaultValues: { reason: '', note: '' },
  })

  useEffect(() => {
    if (!request || !open) return

    approveForm.reset({ note: request.reviewNote ?? '' })
    rejectForm.reset({
      reason: request.rejectReason ?? request.reviewNote ?? '',
      note: request.reviewNote ?? '',
    })
  }, [approveForm, rejectForm, open, request])

  // 通过申请
  const approveMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data?: ApproveFormData }) =>
      approveRebateRequest(id, data),
    onSuccess: () => {
      toast.success(t('Rebate request approved'))
      queryClient.invalidateQueries({ queryKey: ['adminRebateRequests'] })
      onClose()
      approveForm.reset()
    },
    onError: (error: unknown) => {
      toast.error(
        getInvitationErrorMessage(error, t('Failed to approve rebate request'))
      )
    },
  })

  // 拒绝申请
  const rejectMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: RejectFormData }) =>
      rejectRebateRequest(id, data),
    onSuccess: () => {
      toast.success(t('Rebate request rejected'))
      queryClient.invalidateQueries({ queryKey: ['adminRebateRequests'] })
      onClose()
      rejectForm.reset()
    },
    onError: (error: unknown) => {
      toast.error(
        getInvitationErrorMessage(error, t('Failed to reject rebate request'))
      )
    },
  })

  // 撤回审核
  const resetMutation = useMutation({
    mutationFn: (id: number) => resetRebateRequestReview(id),
    onSuccess: () => {
      toast.success(t('Rebate review reset'))
      queryClient.invalidateQueries({ queryKey: ['adminRebateRequests'] })
      onClose()
      approveForm.reset()
      rejectForm.reset()
    },
    onError: (error: unknown) => {
      toast.error(
        getInvitationErrorMessage(error, t('Failed to reset rebate review'))
      )
    },
  })

  // 标记完成
  const completeMutation = useMutation({
    mutationFn: (id: number) => completeRebateRequest(id),
    onSuccess: () => {
      toast.success(t('Rebate marked as completed'))
      queryClient.invalidateQueries({ queryKey: ['adminRebateRequests'] })
      queryClient.invalidateQueries({ queryKey: ['rebateStats'] })
      queryClient.invalidateQueries({ queryKey: ['adminRebateRecords'] })
      onClose()
    },
    onError: (error: unknown) => {
      toast.error(
        getInvitationErrorMessage(error, t('Failed to complete rebate request'))
      )
    },
  })

  // 撤回完成
  const undoCompleteMutation = useMutation({
    mutationFn: (id: number) => undoCompleteRebateRequest(id),
    onSuccess: () => {
      toast.success(t('Rebate completion withdrawn'))
      queryClient.invalidateQueries({ queryKey: ['adminRebateRequests'] })
      queryClient.invalidateQueries({ queryKey: ['rebateStats'] })
      queryClient.invalidateQueries({ queryKey: ['adminRebateRecords'] })
      onClose()
    },
    onError: (error: unknown) => {
      toast.error(
        getInvitationErrorMessage(
          error,
          t('Failed to withdraw rebate completion')
        )
      )
    },
  })

  const handleApprove = (data: ApproveFormData) => {
    if (!request) return
    approveMutation.mutate({ id: request.id, data })
  }

  const handleReject = (data: RejectFormData) => {
    if (!request) return
    rejectMutation.mutate({ id: request.id, data })
  }

  const handleComplete = () => {
    if (!request) return
    if (
      window.confirm(
        t('Are you sure you want to mark this rebate as completed?')
      )
    ) {
      completeMutation.mutate(request.id)
    }
  }

  const handleReset = () => {
    if (!request) return
    resetMutation.mutate(request.id)
  }

  const handleUndoComplete = () => {
    if (!request) return
    undoCompleteMutation.mutate(request.id)
  }

  const formatUser = (request: RebateRequestAdmin) => {
    return request.userName
      ? `${request.userName} (#${request.userId})`
      : `#${request.userId}`
  }

  const isPending =
    approveMutation.isPending ||
    rejectMutation.isPending ||
    resetMutation.isPending ||
    completeMutation.isPending ||
    undoCompleteMutation.isPending

  if (!request) return null

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className='sm:max-w-[500px]'>
        <DialogHeader>
          <DialogTitle>
            {actionType === 'approve' && t('Approve Rebate')}
            {actionType === 'reject' && t('Reject Rebate')}
            {actionType === 'reset' && t('Reset Rebate Review')}
            {actionType === 'complete' && t('Complete Rebate')}
            {actionType === 'undoComplete' && t('Withdraw Rebate Completion')}
          </DialogTitle>
          <DialogDescription>
            {t('Rebate User')}: {formatUser(request)} | {t('Rebate Amount')}:{' '}
            {formatRebateAmount(request.amount)}
          </DialogDescription>
        </DialogHeader>

        {actionType === 'approve' && (
          <form
            onSubmit={approveForm.handleSubmit(handleApprove)}
            className='space-y-4'
          >
            <div className='space-y-2'>
              <Label htmlFor='approve-note'>
                {t('Note')} ({t('Optional')})
              </Label>
              <Textarea
                id='approve-note'
                placeholder={t('Add any notes for this approval')}
                {...approveForm.register('note')}
              />
            </div>

            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                {t('Cancel')}
              </Button>
              <Button type='submit' disabled={isPending}>
                {isPending ? t('Processing...') : t('Approve')}
              </Button>
            </DialogFooter>
          </form>
        )}

        {actionType === 'reject' && (
          <form
            onSubmit={rejectForm.handleSubmit(handleReject)}
            className='space-y-4'
          >
            <div className='space-y-2'>
              <Label htmlFor='reject-reason'>
                {t('Reject Reason')} <span className='text-destructive'>*</span>
              </Label>
              <Textarea
                id='reject-reason'
                placeholder={t('Please provide a reason for rejection')}
                {...rejectForm.register('reason')}
              />
              {rejectForm.formState.errors.reason && (
                <p className='text-destructive text-sm'>
                  {rejectForm.formState.errors.reason.message}
                </p>
              )}
            </div>

            <div className='space-y-2'>
              <Label htmlFor='reject-note'>
                {t('Note')} ({t('Optional')})
              </Label>
              <Textarea
                id='reject-note'
                placeholder={t('Add any additional notes')}
                {...rejectForm.register('note')}
              />
            </div>

            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                {t('Cancel')}
              </Button>
              <Button type='submit' variant='destructive' disabled={isPending}>
                {isPending ? t('Processing...') : t('Reject')}
              </Button>
            </DialogFooter>
          </form>
        )}

        {actionType === 'complete' && (
          <div className='space-y-4'>
            <p className='text-muted-foreground text-sm'>
              {t('This will mark the rebate as completed.')}
            </p>

            {request.rejectReason && (
              <div className='bg-muted rounded-md p-3'>
                <div className='text-sm font-medium'>
                  {t('Previous Reject Reason')}:
                </div>
                <div className='text-muted-foreground mt-1 text-sm'>
                  {request.rejectReason}
                </div>
              </div>
            )}

            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                {t('Cancel')}
              </Button>
              <Button onClick={handleComplete} disabled={isPending}>
                {isPending ? t('Processing...') : t('Complete')}
              </Button>
            </DialogFooter>
          </div>
        )}

        {actionType === 'reset' && (
          <div className='space-y-4'>
            <p className='text-muted-foreground text-sm'>
              {t(
                'This will reset the rebate request to pending review and clear the current review note.'
              )}
            </p>

            {request.reviewNote && (
              <div className='bg-muted rounded-md p-3'>
                <div className='text-sm font-medium'>
                  {t('Current Review Note')}:
                </div>
                <div className='text-muted-foreground mt-1 text-sm'>
                  {request.reviewNote}
                </div>
              </div>
            )}

            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                {t('Cancel')}
              </Button>
              <Button onClick={handleReset} disabled={isPending}>
                {isPending ? t('Processing...') : t('Reset Review')}
              </Button>
            </DialogFooter>
          </div>
        )}

        {actionType === 'undoComplete' && (
          <div className='space-y-4'>
            <p className='text-muted-foreground text-sm'>
              {t(
                'This will move the completed rebate request back to approved status.'
              )}
            </p>

            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                {t('Cancel')}
              </Button>
              <Button onClick={handleUndoComplete} disabled={isPending}>
                {isPending ? t('Processing...') : t('Withdraw Completion')}
              </Button>
            </DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
