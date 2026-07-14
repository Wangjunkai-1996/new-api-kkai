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
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import type { RebateRuleFormValues } from '../../schemas'
import { ALL_USER_GROUP, type UserGroup } from '../../types'

export const RebateRuleFields = (props: {
  form: UseFormReturn<RebateRuleFormValues>
  groups?: UserGroup[]
}) => {
  const { t } = useTranslation()
  return (
    <>
      <div className='space-y-2'>
        <Label htmlFor='invitation-user-group'>{t('User Group')}</Label>
        <Select
          value={props.form.watch('user_group')}
          onValueChange={(value) => {
            if (value) props.form.setValue('user_group', value)
          }}
        >
          <SelectTrigger id='invitation-user-group'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL_USER_GROUP}>
              {t('All User Groups')}
            </SelectItem>
            {props.groups?.map((group) => (
              <SelectItem key={group.name} value={group.name}>
                {group.name} ({group.user_count})
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className='space-y-2'>
        <Label htmlFor='invitation-rule-type'>{t('Rule Type')}</Label>
        <Select
          value={props.form.watch('rule_type')}
          onValueChange={(value) =>
            props.form.setValue(
              'rule_type',
              value as RebateRuleFormValues['rule_type']
            )
          }
        >
          <SelectTrigger id='invitation-rule-type'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='subscription'>{t('Subscription')}</SelectItem>
            <SelectItem value='topup'>{t('Top-up')}</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className='space-y-2'>
        <Label htmlFor='invitation-rebate-rate'>{t('Rebate Rate')} (%)</Label>
        <Input
          id='invitation-rebate-rate'
          type='number'
          min='0'
          max='100'
          step='0.01'
          {...props.form.register('rebate_rate')}
        />
        {props.form.formState.errors.rebate_rate && (
          <p className='text-destructive text-sm'>
            {t('Rebate rate must be between 0 and 100')}
          </p>
        )}
      </div>
    </>
  )
}
