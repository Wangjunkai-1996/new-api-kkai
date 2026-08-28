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
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import type { ArenaCategory } from './rankings-data'

export function ArenaRankingCard(props: {
  category: ArenaCategory
  period: 'month' | 'year'
}) {
  const { t } = useTranslation()
  const topRow = props.category.rows[0]

  return (
    <article
      id={`arena-${props.category.id}`}
      className='flex min-h-[520px] scroll-mt-[215px] flex-col rounded-xl border border-[#181818] bg-[#0f0f0f] px-6 py-8 text-[#e8e8e8] sm:min-h-[591px] sm:px-[26px] sm:py-[30px]'
    >
      <header>
        <h2 className='text-2xl font-bold text-[#eee]'>
          {t(props.category.label)}
        </h2>
        <p className='mt-1.5 w-fit rounded-full bg-[#262626] px-3 py-1 text-sm leading-5'>
          {t('Updated at {{time}}', { time: props.category.updatedAt })}
        </p>
        <dl className='mt-5 space-y-4 text-lg'>
          <div className='flex min-w-0 gap-1'>
            <dt className='shrink-0 font-bold'>{t('Top model')}:</dt>
            <dd className='truncate'>{topRow?.model}</dd>
          </div>
          <div className='flex gap-1'>
            <dt className='font-bold'>
              {t(props.category.metricLabel || 'Score')}:
            </dt>
            <dd>{topRow?.score}</dd>
          </div>
          <div className='flex gap-1'>
            <dt className='font-bold'>{t('Total models')}:</dt>
            <dd>{props.category.total}</dd>
          </div>
        </dl>
      </header>

      <div className='mt-6 grid grid-cols-[1fr_auto] border-b border-white/10 pb-3 text-base font-bold'>
        <span># {t('Model')}</span>
        <span>{t(props.category.metricLabel || 'Score')}</span>
      </div>
      <ol>
        {props.category.rows.map((row, index) => (
          <li
            key={row.model}
            className='grid min-h-[52px] grid-cols-[minmax(0,1fr)_auto] items-center gap-4 border-b border-white/10 text-base font-semibold'
          >
            <span className='truncate'>
              {index + 1}&nbsp;&nbsp;{row.model}
            </span>
            <span className='whitespace-nowrap'>{row.score}</span>
          </li>
        ))}
      </ol>

      <Link
        to='/rankings/$categoryId'
        params={{ categoryId: props.category.id }}
        search={{ period: props.period }}
        className='mt-auto flex w-fit items-center gap-2 pt-6 text-base font-bold transition hover:gap-3 hover:text-white'
      >
        {t('View all')}
        <ArrowRight className='size-4' aria-hidden />
      </Link>
    </article>
  )
}
