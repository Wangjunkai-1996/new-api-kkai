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
import { ArrowUpRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { parseTags } from '@/features/pricing/lib/filters'
import { isTokenBasedModel } from '@/features/pricing/lib/model-helpers'
import { formatPrice, formatRequestPrice } from '@/features/pricing/lib/price'
import type { PricingModel } from '@/features/pricing/types'
import { getLobeIcon } from '@/lib/lobe-icon'

type ModelSquareTableProps = {
  models: PricingModel[]
  priceRate: number
  usdExchangeRate: number
  showRechargePrice: boolean
  selectedGroup: string
  onSelect: (model: PricingModel) => void
}

function formatContextLength(value?: number) {
  if (!value) return '—'
  if (value >= 1_000_000) return `${Math.round(value / 1_000_000)}M`
  return `${Math.round(value / 1_000)}K`
}

export function ModelSquareTable(props: ModelSquareTableProps) {
  const { t } = useTranslation()

  return (
    <div className='mt-6 overflow-x-auto rounded-[9px] border border-[#181818] bg-[#0d0d0d]'>
      <table className='w-full min-w-[1040px] border-collapse text-left'>
        <thead className='border-b border-white/[0.08] text-sm text-white/40'>
          <tr>
            <th className='px-7 py-5 font-normal'>{t('Model name')}</th>
            <th className='px-5 py-5 font-normal'>{t('Tags')}</th>
            <th className='px-5 py-5 font-normal'>{t('Context')}</th>
            <th className='px-5 py-5 font-normal'>{t('Billing')}</th>
            <th className='px-5 py-5 font-normal'>{t('Price')}</th>
            <th className='px-7 py-5 text-right font-normal'>
              {t('Online Experience')}
            </th>
          </tr>
        </thead>
        <tbody>
          {props.models.map((model) => {
            const iconKey = model.icon || model.vendor_icon
            const modelIcon = iconKey ? getLobeIcon(iconKey, 28) : null
            const tags = [
              ...(model.supported_endpoint_types || []),
              ...parseTags(model.tags),
            ].slice(0, 3)
            const price = isTokenBasedModel(model)
              ? `${formatPrice(model, 'input', 'M', props.showRechargePrice, props.priceRate, props.usdExchangeRate, props.selectedGroup)} /M · ${formatPrice(model, 'output', 'M', props.showRechargePrice, props.priceRate, props.usdExchangeRate, props.selectedGroup)} /M`
              : `${formatRequestPrice(model, props.showRechargePrice, props.priceRate, props.usdExchangeRate, props.selectedGroup)} / ${t('request')}`

            return (
              <tr
                key={model.id || model.model_name}
                className='group border-b border-white/[0.06] transition-colors last:border-b-0 hover:bg-white/[0.035]'
              >
                <td className='px-7 py-6'>
                  <button
                    type='button'
                    onClick={() => props.onSelect(model)}
                    className='flex items-center gap-4 text-left focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white'
                  >
                    <span className='flex size-10 shrink-0 items-center justify-center rounded-lg bg-white/[0.04]'>
                      {modelIcon || (
                        <span className='font-semibold'>
                          {model.model_name.charAt(0).toUpperCase()}
                        </span>
                      )}
                    </span>
                    <span>
                      <strong className='block font-mono text-[15px] text-white'>
                        {model.model_name}
                      </strong>
                      <span className='mt-1 block max-w-[280px] truncate text-xs text-white/35'>
                        {model.vendor_name || t('Unnamed')}
                      </span>
                    </span>
                  </button>
                </td>
                <td className='px-5 py-6'>
                  <div className='flex max-w-60 flex-wrap gap-1.5'>
                    {tags.length > 0 ? (
                      tags.map((tag) => (
                        <span
                          key={tag}
                          className='rounded-full border border-white/[0.08] px-2.5 py-1 text-xs text-white/55'
                        >
                          {tag}
                        </span>
                      ))
                    ) : (
                      <span className='text-white/25'>—</span>
                    )}
                  </div>
                </td>
                <td className='px-5 py-6 text-sm text-white/65'>
                  {formatContextLength(model.context_length)}
                </td>
                <td className='px-5 py-6 text-sm text-white/65'>
                  {isTokenBasedModel(model)
                    ? t('Token-based')
                    : t('Per Request')}
                </td>
                <td className='px-5 py-6 font-mono text-sm whitespace-nowrap text-white/75'>
                  {price}
                </td>
                <td className='px-7 py-6 text-right'>
                  <button
                    type='button'
                    onClick={() => props.onSelect(model)}
                    className='inline-flex items-center gap-2 rounded-full border border-white/[0.12] px-4 py-2 text-sm text-white/70 transition group-hover:border-white/25 group-hover:bg-white/[0.06] group-hover:text-white'
                  >
                    {t('Online Experience')}
                    <ArrowUpRight className='size-4' aria-hidden />
                  </button>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
