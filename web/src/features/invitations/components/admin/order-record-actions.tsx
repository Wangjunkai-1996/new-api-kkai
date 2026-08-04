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

import type { AdminRebateOrderRecord } from '../../types'
import type { OrderRecordActionInput } from './order-record-action-dialog'

export const OrderRecordActions = (props: {
  record: AdminRebateOrderRecord
  onAction: (input: OrderRecordActionInput) => void
}) => {
  const { t } = useTranslation()
  const select = (action: OrderRecordActionInput['action']) =>
    props.onAction({ record: props.record, action })

  if (!props.record.localRebateRecordId) return null

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
        {props.record.canModify && (
          <DropdownMenuItem onSelect={() => select('modify')}>
            {t('Adjust Rebate')}
          </DropdownMenuItem>
        )}
        {props.record.canClose && (
          <DropdownMenuItem onSelect={() => select('close')}>
            {t('Close')}
          </DropdownMenuItem>
        )}
        {props.record.canReopen && (
          <DropdownMenuItem onSelect={() => select('reopen')}>
            {t('Reopen')}
          </DropdownMenuItem>
        )}
        {props.record.canEndInitialization && (
          <DropdownMenuItem onSelect={() => select('end-initialization')}>
            {t('End Initialization')}
          </DropdownMenuItem>
        )}
        {props.record.canExtendInitialization && (
          <DropdownMenuItem onSelect={() => select('extend-initialization')}>
            {t('Extend Initialization')}
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
