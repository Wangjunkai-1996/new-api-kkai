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
import { useGroupDisplayNames } from '@/hooks/use-group-display-names'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { formatInvitationDate, formatRebateAmount } from '../../format'
import type { AdminInvitationRegistration } from '../../types'
import {
  RegistrationActions,
  type RegistrationRewardAction,
} from './registration-actions'

export const InvitationRegistrationTable = (props: {
  registrations: AdminInvitationRegistration[]
  onAction: (action: RegistrationRewardAction) => void
}) => {
  const { t } = useTranslation()
  const groupDisplayNames = useGroupDisplayNames()
  return (
    <div className='overflow-x-auto rounded-md border'>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Inviter')}</TableHead>
            <TableHead>{t('Invitee')}</TableHead>
            <TableHead>{t('User Group')}</TableHead>
            <TableHead>{t('Inviter Reward')}</TableHead>
            <TableHead>{t('Invitee Reward')}</TableHead>
            <TableHead>{t('Invited At')}</TableHead>
            <TableHead className='w-14' />
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.registrations.map((registration) => (
            <TableRow key={registration.id}>
              <TableCell>
                {registration.inviterName || registration.inviterId}
              </TableCell>
              <TableCell>
                {registration.inviteeName || registration.inviteeId}
              </TableCell>
              <TableCell>
                {groupDisplayNames[registration.userGroup] ||
                  registration.userGroup}
              </TableCell>
              <TableCell>
                <RewardStatus
                  generated={registration.inviterRewardGenerated}
                  amount={registration.inviterRewardAmount}
                  status={registration.inviterRewardStatus}
                />
              </TableCell>
              <TableCell>
                <RewardStatus
                  generated={registration.inviteeRewardGenerated}
                  amount={registration.inviteeRewardAmount}
                  status={registration.inviteeRewardStatus}
                />
              </TableCell>
              <TableCell>
                {formatInvitationDate(registration.invitedAt)}
              </TableCell>
              <TableCell>
                <RegistrationActions
                  registration={registration}
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

const RewardStatus = (props: {
  generated: boolean
  amount: number
  status?: string | null
}) => (
  <div className='flex items-center gap-2'>
    <span className='tabular-nums'>{formatRebateAmount(props.amount)}</span>
    <Badge variant={props.generated ? 'default' : 'outline'}>
      {props.status || (props.generated ? 'generated' : 'not generated')}
    </Badge>
  </div>
)
