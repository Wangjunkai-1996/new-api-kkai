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
import { Link } from '@tanstack/react-router'
import { Check } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import type { SystemStatus } from '@/features/auth/types'
import { cn } from '@/lib/utils'

type LinkAiLegalConsentProps = {
  id: string
  status: SystemStatus | null
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  className?: string
}

export function LinkAiLegalConsent({
  id,
  status,
  checked,
  onCheckedChange,
  className,
}: LinkAiLegalConsentProps) {
  const { t } = useTranslation()
  const hasUserAgreement = Boolean(status?.user_agreement_enabled)
  const hasPrivacyPolicy = Boolean(status?.privacy_policy_enabled)

  if (!hasUserAgreement && !hasPrivacyPolicy) return null

  return (
    <label
      htmlFor={id}
      className={cn(
        'flex cursor-pointer items-center gap-[10px] px-4 text-sm leading-6 text-[#9b9b9b] sm:text-base',
        className
      )}
    >
      <input
        id={id}
        type='checkbox'
        checked={checked}
        onChange={(event) => onCheckedChange(event.target.checked)}
        className='peer sr-only'
      />
      <span className='flex h-[19px] w-[19px] shrink-0 items-center justify-center rounded-[4px] border border-[#eeeeee] text-black transition peer-checked:bg-white peer-focus-visible:ring-2 peer-focus-visible:ring-[#7258ce] peer-focus-visible:ring-offset-2 peer-focus-visible:ring-offset-black'>
        {checked && <Check className='h-4 w-4' strokeWidth={3} />}
      </span>
      <span>
        {t('I agree to the')}{' '}
        {hasUserAgreement && (
          <Link to='/user-agreement' className='text-[#eeeeee] hover:underline'>
            {t('Terms of Service')}
          </Link>
        )}
        {hasUserAgreement && hasPrivacyPolicy && <> {t('and')} </>}
        {hasPrivacyPolicy && (
          <Link to='/privacy-policy' className='text-[#eeeeee] hover:underline'>
            {t('Privacy Policy')}
          </Link>
        )}
      </span>
    </label>
  )
}
