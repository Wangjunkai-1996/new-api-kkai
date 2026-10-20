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

import { useSystemConfigStore } from '@/stores/system-config-store'

import { formatRequestPrice } from '../lib/price'
import type { PricingModel } from '../types'

export function ImageResolutionPrices(props: {
  model: PricingModel
  showRechargePrice?: boolean
  priceRate?: number
  usdExchangeRate?: number
  selectedGroup?: string
  className?: string
}) {
  const { t } = useTranslation()
  useSystemConfigStore((state) => state.config.currency)
  return (
    <dl className={props.className ?? 'grid grid-cols-3 gap-3'}>
      {Object.entries(props.model.image_pricing_policy?.tiers ?? {}).map(
        ([tier, pricing]) => (
          <div key={tier} className='min-w-0'>
            <dt className='text-muted-foreground text-xs uppercase'>{tier}</dt>
            <dd className='mt-1 font-mono text-sm tabular-nums'>
              {formatRequestPrice(
                {
                  ...props.model,
                  quota_type: 1,
                  model_price: pricing.unit_price,
                },
                props.showRechargePrice,
                props.priceRate,
                props.usdExchangeRate,
                props.selectedGroup
              )}{' '}
              / {t('image')}
            </dd>
          </div>
        )
      )}
    </dl>
  )
}
