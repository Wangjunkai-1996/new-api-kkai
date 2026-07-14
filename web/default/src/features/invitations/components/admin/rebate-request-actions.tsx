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
import { MoreHorizontal } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

import { isApproveAndPayEligible } from '../../admin-action-policy'
import { createApproveAndPayCommand } from '../../payout-command'
import type { RebateRequestAdmin } from '../../types'
import type { RebateRequestAction } from './request-action-dialog'

export const RebateRequestActions = (props: {
  request: RebateRequestAdmin
  onAction: (action: RebateRequestAction) => void
}) => {
  const { t } = useTranslation()
  const request = props.request

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type='button'
            size='icon-sm'
            variant='ghost'
            aria-label={t('Actions')}
            title={t('Actions')}
          />
        }
      >
        <MoreHorizontal />
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end'>
        {request.status === 'pending' && (
          <>
            <DropdownMenuItem
              onSelect={() =>
                props.onAction({ kind: 'approve', requestId: request.id })
              }
            >
              {t('Approve')}
            </DropdownMenuItem>
            <DropdownMenuItem
              variant='destructive'
              onSelect={() =>
                props.onAction({
                  kind: 'reject',
                  requestId: request.id,
                  reason: '',
                })
              }
            >
              {t('Reject')}
            </DropdownMenuItem>
          </>
        )}
        {isApproveAndPayEligible(request.status) && (
          <DropdownMenuItem
            onSelect={() =>
              props.onAction({
                kind: 'approve-pay',
                requestId: request.id,
                command: createApproveAndPayCommand(`request:${request.id}`),
              })
            }
          >
            {t('Approve and Pay')}
          </DropdownMenuItem>
        )}
        {(request.status === 'approved' || request.status === 'rejected') && (
          <DropdownMenuItem
            onSelect={() =>
              props.onAction({ kind: 'reset', requestId: request.id })
            }
          >
            {t('Reset Review')}
          </DropdownMenuItem>
        )}
        {request.status === 'approved' && (
          <DropdownMenuItem
            onSelect={() =>
              props.onAction({ kind: 'complete', requestId: request.id })
            }
          >
            {t('Mark Completed')}
          </DropdownMenuItem>
        )}
        {request.status === 'completed' && (
          <DropdownMenuItem
            onSelect={() =>
              props.onAction({ kind: 'undo-complete', requestId: request.id })
            }
          >
            {t('Undo Completed')}
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
