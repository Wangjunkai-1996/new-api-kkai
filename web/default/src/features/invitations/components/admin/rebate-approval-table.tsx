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
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { isApproveAndPayEligible } from '../../admin-action-policy'
import { formatInvitationDate, formatRebateAmount } from '../../format'
import {
  rebateRequestStatusLabel,
  rebateStatusVariant,
} from '../../status-label'
import type { RebateRequestAdmin } from '../../types'
import { RebateRequestActions } from './rebate-request-actions'
import type { RebateRequestAction } from './request-action-dialog'

export const RebateApprovalTable = (props: {
  requests: RebateRequestAdmin[]
  selectedIds: number[]
  onSelectedIdsChange: (ids: number[]) => void
  onAction: (action: RebateRequestAction) => void
}) => {
  const { t } = useTranslation()
  const payoutEligibleRequests = props.requests.filter((request) =>
    isApproveAndPayEligible(request.status)
  )
  const allEligibleSelected =
    payoutEligibleRequests.length > 0 &&
    payoutEligibleRequests.every((request) =>
      props.selectedIds.includes(request.id)
    )

  return (
    <div className='overflow-x-auto rounded-md border'>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className='w-10'>
              <Checkbox
                aria-label={t('Select all payout eligible requests')}
                checked={allEligibleSelected}
                onCheckedChange={(checked) =>
                  props.onSelectedIdsChange(
                    checked === true
                      ? payoutEligibleRequests.map((request) => request.id)
                      : []
                  )
                }
              />
            </TableHead>
            <TableHead>{t('User')}</TableHead>
            <TableHead>{t('Amount')}</TableHead>
            <TableHead>{t('Status')}</TableHead>
            <TableHead>{t('Created At')}</TableHead>
            <TableHead className='w-14' />
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.requests.map((request) => (
            <TableRow key={request.id}>
              <TableCell>
                <Checkbox
                  disabled={!isApproveAndPayEligible(request.status)}
                  aria-label={t('Select request {{id}}', { id: request.id })}
                  checked={props.selectedIds.includes(request.id)}
                  onCheckedChange={(checked) =>
                    props.onSelectedIdsChange(
                      checked === true
                        ? [...props.selectedIds, request.id]
                        : props.selectedIds.filter((id) => id !== request.id)
                    )
                  }
                />
              </TableCell>
              <TableCell>{request.userName}</TableCell>
              <TableCell>{formatRebateAmount(request.amount)}</TableCell>
              <TableCell>
                <Badge variant={rebateStatusVariant(request.status)}>
                  {t(rebateRequestStatusLabel(request.status))}
                </Badge>
              </TableCell>
              <TableCell>{formatInvitationDate(request.createdAt)}</TableCell>
              <TableCell>
                <RebateRequestActions
                  request={request}
                  onAction={props.onAction}
                />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
