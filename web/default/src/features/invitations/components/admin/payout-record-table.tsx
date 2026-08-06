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
import { rebateStatusLabel, rebateStatusVariant } from '../../status-label'
import type { RebateRecord } from '../../types'
import type { PayoutActionInput } from './payout-action-dialog'
import { PayoutRecordActions } from './payout-record-actions'

export const PayoutRecordTable = (props: {
  records: RebateRecord[]
  mode: 'order' | 'signup'
  onAction: (input: PayoutActionInput) => void
}) => {
  const { t } = useTranslation()
  return (
    <div className='overflow-x-auto rounded-md border'>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('User')}</TableHead>
            <TableHead>{t('Source')}</TableHead>
            <TableHead>{t('Rebate Amount')}</TableHead>
            <TableHead>{t('Rebate Status')}</TableHead>
            <TableHead>{t('Payout Status')}</TableHead>
            <TableHead>{t('Created At')}</TableHead>
            <TableHead className='w-14' />
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.records.map((record) => (
            <TableRow key={record.id}>
              <TableCell>{record.userName || record.userId || '-'}</TableCell>
              <TableCell>{record.orderType}</TableCell>
              <TableCell>{formatRebateAmount(record.rebateAmount)}</TableCell>
              <TableCell>
                <Badge variant={rebateStatusVariant(record.status)}>
                  {t(rebateStatusLabel(record.status))}
                </Badge>
              </TableCell>
              <TableCell>
                <Badge variant='outline'>
                  {record.payout?.status || record.payoutStatus || 'none'}
                </Badge>
              </TableCell>
              <TableCell>{formatInvitationDate(record.createdAt)}</TableCell>
              <TableCell>
                <PayoutRecordActions
                  record={record}
                  mode={props.mode}
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
