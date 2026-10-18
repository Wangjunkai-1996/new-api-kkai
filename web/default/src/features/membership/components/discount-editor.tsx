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

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'

import { findMembershipDiscountConflicts } from '../lib'
import type { MembershipCatalog } from '../types'

export function MembershipDiscountEditor(props: {
  catalog: MembershipCatalog
  publishedGroups?: string[]
  onDiscountChange: (tierIndex: number, group: string, value: number) => void
  onPublishedChange: (groupIndex: number, value: boolean) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const conflicts = findMembershipDiscountConflicts(props.catalog)
  const tiers = props.catalog.tiers
    .map((tier, index) => ({ tier, index }))
    .sort((a, b) => a.tier.rank - b.tier.rank)
  return (
    <section className='space-y-4'>
      <div>
        <h3 className='font-semibold'>{t('Group discount matrix')}</h3>
        <p className='text-muted-foreground mt-1 text-sm'>
          {t(
            'Lower coefficients mean lower prices. Each higher tier must be at least as favorable as the tier below it.'
          )}
        </p>
      </div>
      {conflicts.length > 0 && (
        <Alert variant='destructive'>
          <AlertDescription>
            {t('Fix the highlighted group discounts before publishing.')}
          </AlertDescription>
        </Alert>
      )}
      <div className='space-y-3'>
        {props.catalog.groups.map((group, groupIndex) => (
          <div key={group.key} className='rounded-xl border p-4'>
            <div className='mb-4 flex flex-wrap items-center justify-between gap-3'>
              <div>
                <p className='font-medium'>{group.display_name || group.key}</p>
                <p className='text-muted-foreground text-xs'>{group.key}</p>
              </div>
              <label className='flex items-center gap-2 text-sm'>
                <Checkbox
                  checked={group.published}
                  onCheckedChange={(checked) =>
                    props.onPublishedChange(groupIndex, checked === true)
                  }
                  disabled={
                    props.disabled || props.publishedGroups?.includes(group.key)
                  }
                />
                {t('Publish group benefits')}
              </label>
            </div>
            <div className='grid gap-3 sm:grid-cols-3'>
              {tiers.map(({ tier, index }) => {
                const invalid = conflicts.some(
                  (conflict) =>
                    conflict.group === group.key && conflict.tier === tier.id
                )
                const inputId = `membership-discount-${groupIndex}-${tier.id}`
                return (
                  <div key={`${tier.id}:${tier.rank}`} className='space-y-1.5'>
                    <label htmlFor={inputId} className='text-sm'>
                      {tier.name || tier.id}
                    </label>
                    <Input
                      id={inputId}
                      type='number'
                      step='0.001'
                      min='0.001'
                      max='1'
                      value={
                        Number.isFinite(tier.discounts[group.key] ?? 1)
                          ? (tier.discounts[group.key] ?? 1)
                          : ''
                      }
                      onChange={(event) =>
                        props.onDiscountChange(
                          index,
                          group.key,
                          event.target.value === ''
                            ? Number.NaN
                            : Number(event.target.value)
                        )
                      }
                      disabled={props.disabled || !group.published}
                      aria-invalid={invalid}
                    />
                    {invalid && (
                      <p className='text-destructive text-xs'>
                        {t('A higher tier cannot have a higher coefficient')}
                      </p>
                    )}
                  </div>
                )
              })}
            </div>
            {!group.published && (
              <p className='text-muted-foreground mt-3 text-xs'>
                {t(
                  'Unpublished groups temporarily have no extra discount. Their first published benefits also apply to eligible existing members.'
                )}
              </p>
            )}
          </div>
        ))}
      </div>
      {props.catalog.groups.length === 0 && (
        <p className='text-muted-foreground text-sm'>
          {t('Create billing groups in group settings first.')}
        </p>
      )}
    </section>
  )
}
