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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { formatInvitationDate, formatRebateAmount } from '../../format'
import type { AdminRebateOrderRecord } from '../../types'
import type { OrderRecordActionInput } from './order-record-action-dialog'
import { OrderRecordActions } from './order-record-actions'

export const RebateOrderRecordTable = (props: {
  records: AdminRebateOrderRecord[]
  onAction: (input: OrderRecordActionInput) => void
}) => {
  const { t } = useTranslation()
  return (
    <div className='overflow-x-auto rounded-md border'>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Order')}</TableHead>
            <TableHead>{t('Inviter')}</TableHead>
            <TableHead>{t('Invitee')}</TableHead>
            <TableHead>{t('Order Amount')}</TableHead>
            <TableHead>{t('Rebate Amount')}</TableHead>
            <TableHead>{t('Status')}</TableHead>
            <TableHead>{t('Effective At')}</TableHead>
            <TableHead className='w-14' />
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.records.map((record) => (
            <TableRow key={`${record.orderType}:${record.orderId}`}>
              <TableCell>
                {record.orderType} #{record.orderId}
              </TableCell>
              <TableCell>{record.inviterName || record.inviterId}</TableCell>
              <TableCell>{record.inviteeName || record.inviteeId}</TableCell>
              <TableCell>{formatRebateAmount(record.orderAmount)}</TableCell>
              <TableCell>{formatRebateAmount(record.rebateAmount)}</TableCell>
              <TableCell>
                <Badge variant={record.ruleMissing ? 'destructive' : 'outline'}>
                  {record.status}
                </Badge>
              </TableCell>
              <TableCell>{formatInvitationDate(record.effectiveAt)}</TableCell>
              <TableCell>
                <OrderRecordActions record={record} onAction={props.onAction} />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
