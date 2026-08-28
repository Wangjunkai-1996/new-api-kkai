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
import { Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { useRankings } from '@/features/rankings/hooks/use-rankings'
import type { RankingPeriod } from '@/features/rankings/types'
import { cn } from '@/lib/utils'

import { LinkAiPublicFooter } from '../components/public-footer'
import { LinkAiPublicHeader } from '../components/public-header'
import { ArenaRankingCard } from './ranking-card'
import { ARENA_CATEGORIES } from './rankings-data'

export function LinkAiRankingsPage() {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const [period, setPeriod] =
    useState<Extract<RankingPeriod, 'month' | 'year'>>('month')
  const rankings = useRankings(period)
  const normalizedQuery = query.trim().toLowerCase()
  const liveCategory = useMemo(() => {
    const snapshot = rankings.data?.data
    if (!snapshot?.models?.length) return null
    const updatedAt =
      snapshot.models_history?.points?.at(-1)?.label || t('Latest')
    return {
      id: 'live-usage',
      label:
        period === 'month' ? 'Monthly Usage Ranking' : 'Yearly Usage Ranking',
      updatedAt,
      total: snapshot.models.length,
      metricLabel: 'Tokens',
      rows: snapshot.models.slice(0, 5).map((model) => ({
        model: model.model_name,
        score: new Intl.NumberFormat().format(model.total_tokens),
      })),
    }
  }, [period, rankings.data, t])
  const allCategories = useMemo(
    () =>
      liveCategory ? [liveCategory, ...ARENA_CATEGORIES] : ARENA_CATEGORIES,
    [liveCategory]
  )
  const categories = useMemo(() => {
    if (!normalizedQuery) return allCategories
    return allCategories.filter(
      (category) =>
        t(category.label).toLowerCase().includes(normalizedQuery) ||
        category.rows.some((row) =>
          row.model.toLowerCase().includes(normalizedQuery)
        )
    )
  }, [allCategories, normalizedQuery, t])

  const scrollToCategory = (id: string) => {
    document
      .querySelector(`#arena-${id}`)
      ?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  return (
    <div className='min-h-svh bg-black text-white'>
      <LinkAiPublicHeader />
      <div className='sticky top-[104px] z-40 border-b border-white/10 bg-black/95 backdrop-blur-xl lg:top-[135px]'>
        <div className='mx-auto flex min-h-16 max-w-[1920px] flex-col gap-3 px-5 py-2 sm:px-8 xl:px-[3.38vw] 2xl:flex-row 2xl:items-center 2xl:justify-between'>
          <nav
            aria-label={t('Ranking categories')}
            className='no-scrollbar flex min-w-0 flex-1 gap-7 overflow-x-auto py-2'
          >
            <button
              type='button'
              className='shrink-0 text-lg text-[#eee] hover:text-white'
              onClick={() => window.scrollTo({ top: 0, behavior: 'smooth' })}
            >
              {t('Overview')}
            </button>
            {allCategories.map((category) => (
              <button
                key={category.id}
                type='button'
                className='shrink-0 text-lg text-[#eee] transition hover:text-white/65'
                onClick={() => scrollToCategory(category.id)}
              >
                {t(category.label)}
              </button>
            ))}
          </nav>

          <label className='flex h-12 w-full shrink-0 items-center gap-2 rounded-[9px] border border-[#181818] bg-[#0a0a0a] px-4 2xl:w-[369px]'>
            <Search className='size-5 text-[#606060]' aria-hidden />
            <span className='sr-only'>{t('Search models')}</span>
            <input
              type='search'
              value={query}
              data-arena-search='rankings'
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t('Search models...')}
              className='min-w-0 flex-1 bg-transparent text-lg text-white outline-none placeholder:text-[#606060]'
            />
          </label>
        </div>
      </div>

      <main>
        <header className='flex min-h-[232px] flex-col items-center justify-center px-5 text-center'>
          <h1 className='text-[clamp(2.4rem,3vw,2.875rem)] font-bold text-[#eee]'>
            {t('Model Rankings')}
          </h1>
          <p className='mt-4 max-w-[1160px] text-base leading-relaxed text-[#a1a1a1] sm:text-xl'>
            {t(
              'See how leading models perform across text, image, vision, and more. This page provides snapshots of each arena, with deeper insights available in dedicated tabs.'
            )}
          </p>
          <div className='mt-6 inline-flex rounded-full border border-white/15 bg-[#0d0d0d] p-1'>
            {(['month', 'year'] as const).map((value) => (
              <button
                key={value}
                type='button'
                aria-pressed={period === value}
                onClick={() => setPeriod(value)}
                className={cn(
                  'rounded-full px-6 py-2 text-sm text-white/50 transition hover:text-white',
                  period === value && 'bg-white text-black hover:text-black'
                )}
              >
                {value === 'month' ? t('This Month') : t('This Year')}
              </button>
            ))}
          </div>
          {rankings.isLoading && (
            <p className='mt-3 text-sm text-white/40'>
              {t('Loading rankings...')}
            </p>
          )}
          {rankings.isError && (
            <p className='mt-3 text-sm text-red-300/75'>
              {t('Ranking data is temporarily unavailable.')}
            </p>
          )}
        </header>

        <section
          aria-label={t('Model Rankings')}
          className='mx-auto grid w-full max-w-[1576px] grid-cols-1 gap-x-[50px] gap-y-16 px-5 pb-40 sm:px-8 xl:grid-cols-2 xl:gap-y-44 xl:px-0 xl:pb-[260px]'
        >
          {categories.map((category) => (
            <ArenaRankingCard
              key={category.id}
              category={category}
              period={period}
            />
          ))}
          {categories.length === 0 && (
            <p className='col-span-full py-32 text-center text-lg text-white/55'>
              {t('No models match your current filters.')}
            </p>
          )}
        </section>
      </main>

      <LinkAiPublicFooter />
    </div>
  )
}
