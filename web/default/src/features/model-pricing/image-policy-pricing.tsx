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

import { Button } from '@/components/ui/button'
import type { ImagePricingPolicy } from '@/features/image-studio/admin/image-pricing-policy'

import { formatPricingAmount, type PricingCurrency } from './currency'

export function ImagePolicyPricing(props: {
  model: ImagePricingPolicy['models'][string]
  currency: PricingCurrency
}) {
  const { t } = useTranslation()
  return (
    <section
      className='space-y-4 rounded-lg border p-4'
      aria-label={t('imageStudio.admin.pricing')}
    >
      <p className='text-muted-foreground text-sm'>
        {t(
          'Image resolution pricing is managed in Image Studio. These prices take precedence over legacy model prices.'
        )}
      </p>
      <dl className='grid grid-cols-3 gap-3'>
        {Object.entries(props.model.tiers).map(([tier, pricing]) => (
          <div key={tier}>
            <dt className='text-muted-foreground text-xs uppercase'>{tier}</dt>
            <dd className='mt-1 font-mono text-sm'>
              {formatPricingAmount(pricing.unit_price, props.currency)} /{' '}
              {t('image')}
            </dd>
          </div>
        ))}
      </dl>
      <Button
        render={<a href='/system-settings/image-studio' />}
        variant='outline'
      >
        {t('imageStudio.admin.pricing')}
      </Button>
    </section>
  )
}
