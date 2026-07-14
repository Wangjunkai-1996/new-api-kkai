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
import { Pencil, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { ALL_USER_GROUP, type RebateRule } from '../../types'

export const RebateRulesTable = (props: {
  rules: RebateRule[]
  onEdit: (rule: RebateRule) => void
  onDelete: (rule: RebateRule) => void
}) => {
  const { t } = useTranslation()
  return (
    <div className='overflow-hidden rounded-md border'>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('User Group')}</TableHead>
            <TableHead>{t('Rule Type')}</TableHead>
            <TableHead>{t('Rebate Rate')}</TableHead>
            <TableHead className='w-24 text-right'>{t('Actions')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.rules.map((rule) => (
            <TableRow key={rule.id}>
              <TableCell>
                <Badge variant='secondary'>
                  {rule.user_group === ALL_USER_GROUP
                    ? t('All User Groups')
                    : rule.user_group}
                </Badge>
              </TableCell>
              <TableCell>{t(rule.rule_type)}</TableCell>
              <TableCell className='tabular-nums'>
                {(Number(rule.rebate_rate) * 100).toFixed(2)}%
              </TableCell>
              <TableCell>
                <div className='flex justify-end gap-1'>
                  <Button
                    size='icon-sm'
                    variant='ghost'
                    aria-label={t('Edit')}
                    title={t('Edit')}
                    onClick={() => props.onEdit(rule)}
                  >
                    <Pencil />
                  </Button>
                  <Button
                    size='icon-sm'
                    variant='ghost'
                    aria-label={t('Delete')}
                    title={t('Delete')}
                    onClick={() => props.onDelete(rule)}
                  >
                    <Trash2 />
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
