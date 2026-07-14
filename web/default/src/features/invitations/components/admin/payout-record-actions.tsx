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

import {
  derivePayoutActions,
  type PayoutRecordMode,
} from '../../admin-action-policy'
import { createRebatePayoutCommand } from '../../payout-command'
import type { RebateRecord } from '../../types'
import type { PayoutActionInput } from './payout-action-dialog'

export const PayoutRecordActions = (props: {
  record: RebateRecord
  mode: PayoutRecordMode
  onAction: (input: PayoutActionInput) => void
}) => {
  const { t } = useTranslation()
  const actions = derivePayoutActions(props.record, props.mode)

  if (!actions.pay && !actions.reverse && !actions.revokeSignup) return null

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
        {actions.pay && (
          <DropdownMenuItem
            onSelect={() =>
              props.onAction({
                kind: 'payout',
                record: props.record,
                command: createRebatePayoutCommand(props.record.id, 'pay'),
              })
            }
          >
            {t('Pay to Balance')}
          </DropdownMenuItem>
        )}
        {actions.reverse && (
          <DropdownMenuItem
            variant='destructive'
            onSelect={() =>
              props.onAction({
                kind: 'payout',
                record: props.record,
                command: createRebatePayoutCommand(props.record.id, 'reverse'),
              })
            }
          >
            {t('Reverse Payout')}
          </DropdownMenuItem>
        )}
        {actions.revokeSignup && (
          <DropdownMenuItem
            variant='destructive'
            onSelect={() =>
              props.onAction({ kind: 'revoke-signup', record: props.record })
            }
          >
            {t('Revoke Signup Reward')}
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
