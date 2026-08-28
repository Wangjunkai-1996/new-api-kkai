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

import { parseTags } from '@/features/pricing/lib/filters'
import { isTokenBasedModel } from '@/features/pricing/lib/model-helpers'
import { formatPrice, formatRequestPrice } from '@/features/pricing/lib/price'
import type { PricingModel } from '@/features/pricing/types'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'

type ModelSquareCardProps = {
  model: PricingModel
  list: boolean
  priceRate: number
  usdExchangeRate: number
  showRechargePrice: boolean
  selectedGroup: string
  onClick: () => void
}

export function ModelSquareCard(props: ModelSquareCardProps) {
  const { t } = useTranslation()
  const iconKey = props.model.icon || props.model.vendor_icon
  const modelIcon = iconKey ? getLobeIcon(iconKey, props.list ? 26 : 32) : null
  const tags = parseTags(props.model.tags)
  const endpoints = props.model.supported_endpoint_types || []
  const displayTags = [...endpoints.slice(0, 2), ...tags.slice(0, 1)]
  const group = props.model.enable_groups?.[0]
  let contextLabel = '1M'
  if (props.model.context_length) {
    contextLabel =
      props.model.context_length >= 1_000_000
        ? `${Math.round(props.model.context_length / 1_000_000)}M`
        : `${Math.round(props.model.context_length / 1000)}K`
  }

  const price = isTokenBasedModel(props.model)
    ? `${formatPrice(props.model, 'input', 'M', props.showRechargePrice, props.priceRate, props.usdExchangeRate, props.selectedGroup)} /M · ${formatPrice(props.model, 'output', 'M', props.showRechargePrice, props.priceRate, props.usdExchangeRate, props.selectedGroup)} /M`
    : `${formatRequestPrice(props.model, props.showRechargePrice, props.priceRate, props.usdExchangeRate, props.selectedGroup)} / ${t('request')}`

  return (
    <button
      type='button'
      onClick={props.onClick}
      className={cn(
        'group flex w-full border border-[#181818] bg-[#0d0d0d] text-left text-[#eee] transition duration-300 hover:-translate-y-1 hover:border-white/25 hover:bg-[#121212] hover:shadow-[0_22px_70px_-35px_rgba(116,87,255,0.65)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-white motion-reduce:transform-none motion-reduce:transition-none',
        props.list
          ? 'min-h-32 items-center gap-5 rounded-xl p-5'
          : 'min-h-[351px] flex-col rounded-[9px] px-6 pt-14 pb-7 sm:px-9'
      )}
    >
      <div
        className={cn(
          'flex items-center',
          props.list ? 'shrink-0 gap-4' : 'flex-col self-center text-center'
        )}
      >
        <div className='flex size-10 items-center justify-center text-white'>
          {modelIcon || (
            <span className='text-lg font-semibold'>
              {props.model.model_name.charAt(0).toUpperCase()}
            </span>
          )}
        </div>
        <div className={cn(props.list ? '' : 'mt-6')}>
          <h2 className='font-mono text-xl font-bold'>
            {props.model.model_name}
          </h2>
          <p className='mt-3 text-[15px] text-[#eee]'>{price}</p>
        </div>
      </div>

      <p
        className={cn(
          'text-[15px] leading-5 text-[#939393]',
          props.list ? 'line-clamp-2 flex-1' : 'mt-10 line-clamp-2 min-h-10'
        )}
      >
        {props.model.description || t('No description available.')}
      </p>

      <div
        className={cn(
          'flex flex-wrap items-center gap-1.5 text-sm',
          props.list ? 'max-w-[34%] justify-end' : 'mt-auto pt-8'
        )}
      >
        {group && (
          <span className='rounded-full bg-[#262626] px-3 py-1'>{group}</span>
        )}
        <span className='rounded-full border border-[#181818] px-3 py-1'>
          {isTokenBasedModel(props.model) ? t('Token-based') : t('Per Request')}
        </span>
        <span className={cn('ml-auto')} />
        {displayTags.map((tag) => (
          <span
            key={tag}
            className='rounded-full border border-[#181818] px-3 py-1 text-[#dcdcdc]'
          >
            {tag}
          </span>
        ))}
        {tags.length + endpoints.length > displayTags.length && (
          <span className='rounded-full border border-[#181818] px-3 py-1 text-[#dcdcdc]'>
            +{tags.length + endpoints.length - displayTags.length}
          </span>
        )}
        <span className='rounded-full bg-[#262626] px-3 py-1 text-[#dcdcdc]'>
          {contextLabel}
        </span>
      </div>
    </button>
  )
}
