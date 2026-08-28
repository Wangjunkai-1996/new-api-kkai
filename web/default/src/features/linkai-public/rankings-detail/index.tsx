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
import { Medal, Search, Trophy } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { useRankings } from '@/features/rankings/hooks/use-rankings'
import type { ModelRanking } from '@/features/rankings/types'
import { cn } from '@/lib/utils'

import { LinkAiPublicFooter } from '../components/public-footer'
import { LinkAiPublicHeader } from '../components/public-header'
import { ARENA_CATEGORIES, type ArenaCategory } from '../rankings/rankings-data'

type DetailPeriod = 'month' | 'year'

type DetailRow = {
  rank: number
  model: string
  score: string
  votes: string
  confidence: string
  organization: string
}

const DETAIL_TITLES: Record<string, string> = {
  text: 'Text Models Arena',
  web: 'Web Development Arena',
  vision: 'Vision Models Arena',
  'text-to-image': 'Text to Image Arena',
  'image-editing': 'Image Editing Arena',
  search: 'Search Models Arena',
  'text-to-video': 'Text to Video Arena',
  'image-to-video': 'Image to Video Arena',
  code: 'Coding Assistant Arena',
}

function inferOrganization(model: string) {
  const normalized = model.toLowerCase()
  if (normalized.includes('gemini') || normalized.includes('imagen')) {
    return 'Google'
  }
  if (normalized.includes('claude')) return 'Anthropic'
  if (
    normalized.includes('gpt') ||
    normalized.includes('codex') ||
    normalized.includes('sora')
  ) {
    return 'OpenAI'
  }
  if (normalized.includes('grok')) return 'xAI'
  if (normalized.includes('qwen')) return 'Alibaba'
  if (normalized.includes('deepseek')) return 'DeepSeek'
  if (normalized.includes('perplexity')) return 'Perplexity'
  if (normalized.includes('midjourney')) return 'Midjourney'
  return '—'
}

function toPreviewRows(category: ArenaCategory): DetailRow[] {
  return category.rows.map((row, index) => ({
    rank: index + 1,
    model: row.model,
    score: row.score,
    votes: '—',
    confidence: '—',
    organization: inferOrganization(row.model),
  }))
}

function toLiveRows(models: ModelRanking[]): DetailRow[] {
  return models.map((model) => ({
    rank: model.rank,
    model: model.model_name,
    score: new Intl.NumberFormat().format(model.total_tokens),
    votes: `${(model.share * 100).toFixed(1)}%`,
    confidence: `${model.growth_pct >= 0 ? '+' : ''}${model.growth_pct.toFixed(1)}%`,
    organization: model.vendor || '—',
  }))
}

function RankMark(props: { rank: number }) {
  if (props.rank > 3) {
    return <span className='text-white/80 tabular-nums'>{props.rank}</span>
  }

  let color = 'text-[#c98758]'
  if (props.rank === 1) color = 'text-[#f4c95d]'
  if (props.rank === 2) color = 'text-[#c8ced8]'
  return <Medal className={cn('size-7', color)} aria-label={`${props.rank}`} />
}

function SummaryIcon(props: { icon: 'total' | 'top' | 'score' }) {
  if (props.icon === 'total') {
    return <span className='text-xl font-semibold'>#</span>
  }
  if (props.icon === 'top') {
    return <Trophy className='size-6' aria-hidden />
  }
  return <Medal className='size-6' aria-hidden />
}

function SummaryCard(props: {
  value: string
  label: string
  icon: 'total' | 'top' | 'score'
}) {
  return (
    <article className='flex min-h-[170px] flex-col justify-between rounded-xl border border-white/10 bg-[#0d0d0d] p-6 sm:min-h-[210px] sm:p-8'>
      <div className='flex size-14 items-center justify-center rounded-full bg-white/[0.06] text-white/75'>
        <SummaryIcon icon={props.icon} />
      </div>
      <div>
        <p className='truncate text-[clamp(1.8rem,2.25vw,2.875rem)] font-semibold tracking-tight text-[#eee]'>
          {props.value}
        </p>
        <p className='mt-2 text-base text-white/45 sm:text-lg'>{props.label}</p>
      </div>
    </article>
  )
}

export function LinkAiRankingDetailPage(props: {
  categoryId: string
  initialPeriod?: DetailPeriod
}) {
  const { t } = useTranslation()
  const [period, setPeriod] = useState<DetailPeriod>(
    props.initialPeriod || 'month'
  )
  const [query, setQuery] = useState('')
  const isLive = props.categoryId === 'live-usage'
  const rankings = useRankings(period)
  const category = ARENA_CATEGORIES.find((item) => item.id === props.categoryId)
  const liveModels = useMemo(
    () => rankings.data?.data.models || [],
    [rankings.data]
  )
  const rows = useMemo(() => {
    if (isLive) return toLiveRows(liveModels)
    if (category) return toPreviewRows(category)
    return []
  }, [category, isLive, liveModels])
  const normalizedQuery = query.trim().toLowerCase()
  const visibleRows = useMemo(
    () =>
      normalizedQuery
        ? rows.filter(
            (row) =>
              row.model.toLowerCase().includes(normalizedQuery) ||
              row.organization.toLowerCase().includes(normalizedQuery)
          )
        : rows,
    [normalizedQuery, rows]
  )
  const topRow = rows[0]
  const totalValue = isLive
    ? `${liveModels.length}`
    : `${category?.total || rows.length}`
  const scoreValue = topRow?.score || '—'
  let title = t(DETAIL_TITLES[props.categoryId] || 'Model Rankings')
  if (isLive) {
    title =
      period === 'month'
        ? t('Monthly Usage Ranking')
        : t('Yearly Usage Ranking')
  }
  const description = isLive
    ? t('Ranked by real token usage collected during the selected period.')
    : t(
        'Explore top AI models ranked by community votes and performance metrics.'
      )

  return (
    <div className='min-h-svh bg-black text-white'>
      <LinkAiPublicHeader />

      <div className='sticky top-[104px] z-40 border-b border-white/10 bg-black/95 backdrop-blur-xl lg:top-[135px]'>
        <div className='mx-auto flex min-h-16 max-w-[1576px] flex-col gap-3 px-5 py-2 sm:px-8 xl:px-0 2xl:flex-row 2xl:items-center 2xl:justify-between'>
          <nav
            aria-label={t('Ranking categories')}
            className='no-scrollbar flex min-w-0 flex-1 gap-7 overflow-x-auto py-2'
          >
            <Link
              to='/rankings'
              className='shrink-0 text-lg text-white/55 transition hover:text-white'
            >
              {t('Overview')}
            </Link>
            {ARENA_CATEGORIES.map((item) => (
              <Link
                key={item.id}
                to='/rankings/$categoryId'
                params={{ categoryId: item.id }}
                search={{ period }}
                className={cn(
                  'shrink-0 text-lg transition hover:text-white',
                  item.id === props.categoryId ? 'text-white' : 'text-white/55'
                )}
              >
                {t(item.label)}
              </Link>
            ))}
          </nav>

          <label className='flex h-12 w-full shrink-0 items-center gap-2 rounded-[9px] border border-[#181818] bg-[#0a0a0a] px-4 2xl:w-[369px]'>
            <Search className='size-5 text-[#606060]' aria-hidden />
            <span className='sr-only'>{t('Search models')}</span>
            <input
              type='search'
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t('Search models...')}
              className='min-w-0 flex-1 bg-transparent text-lg text-white outline-none placeholder:text-[#606060]'
            />
          </label>
        </div>
      </div>

      <main className='mx-auto w-full max-w-[1576px] px-5 pb-36 sm:px-8 xl:px-0'>
        <header className='flex min-h-[330px] flex-col items-center justify-center py-16 text-center sm:min-h-[390px]'>
          <h1 className='text-[clamp(2.8rem,4.5vw,4.5rem)] font-bold tracking-tight text-[#eee]'>
            {title}
          </h1>
          <p className='mt-5 max-w-[900px] text-base leading-relaxed text-[#a1a1a1] sm:text-xl'>
            {description}
          </p>

          {isLive ? (
            <div className='mt-7 inline-flex rounded-full border border-white/15 bg-[#0d0d0d] p-1'>
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
          ) : (
            <p className='mt-7 rounded-full border border-[#7458ff]/35 bg-[#7458ff]/10 px-4 py-2 text-sm text-[#aa99ff]'>
              {t('Figma preview data — not live analytics')}
            </p>
          )}
        </header>

        <section
          aria-label={t('Ranking summary')}
          className='grid gap-5 md:grid-cols-3'
        >
          <SummaryCard
            icon='total'
            value={totalValue}
            label={
              isLive ? t('Models with recorded usage') : t('Available models')
            }
          />
          <SummaryCard
            icon='top'
            value={topRow?.model || '—'}
            label={t('Ranked first')}
          />
          <SummaryCard
            icon='score'
            value={scoreValue}
            label={isLive ? t('Most tokens') : t('Highest score')}
          />
        </section>

        <section className='mt-8 overflow-hidden rounded-xl border border-white/10 bg-[#0d0d0d]'>
          <div className='overflow-x-auto'>
            <table className='w-full min-w-[980px] border-collapse'>
              <thead>
                <tr className='h-[76px] border-b border-white/10 text-left text-base font-semibold text-white/55'>
                  <th className='w-24 px-8'>#</th>
                  <th className='px-5'>{t('Model')}</th>
                  <th className='px-5'>{isLive ? t('Tokens') : t('Score')}</th>
                  <th className='px-5'>{isLive ? t('Share') : t('Votes')}</th>
                  <th className='px-5'>
                    {isLive ? t('Growth') : t('Confidence interval')}
                  </th>
                  <th className='px-5 pr-8'>
                    {isLive ? t('Provider') : t('Organization')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {visibleRows.map((row) => (
                  <tr
                    key={`${row.rank}-${row.model}`}
                    className='h-[82px] border-b border-white/[0.07] text-base text-[#e6e6e6] last:border-0 hover:bg-white/[0.025]'
                  >
                    <td className='px-8'>
                      <RankMark rank={row.rank} />
                    </td>
                    <td className='px-5 font-semibold'>{row.model}</td>
                    <td className='px-5 tabular-nums'>{row.score}</td>
                    <td className='px-5 text-white/65 tabular-nums'>
                      {row.votes}
                    </td>
                    <td className='px-5 text-white/65 tabular-nums'>
                      {row.confidence}
                    </td>
                    <td className='px-5 pr-8 text-white/65'>
                      {row.organization}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {rankings.isLoading && isLive && (
            <p className='px-8 py-14 text-center text-white/45'>
              {t('Loading rankings...')}
            </p>
          )}
          {!rankings.isLoading && visibleRows.length === 0 && (
            <p className='px-8 py-20 text-center text-white/45'>
              {t('No models match your current filters.')}
            </p>
          )}

          <footer className='flex flex-col gap-2 border-t border-white/10 px-8 py-5 text-sm text-white/35 sm:flex-row sm:items-center sm:justify-between'>
            <span>
              {isLive
                ? t('Source: OmniToken live usage API')
                : t('Source: Figma design preview')}
            </span>
            <span>
              {t('Updated at {{time}}', {
                time:
                  (isLive
                    ? rankings.data?.data.models_history?.points?.at(-1)?.label
                    : category?.updatedAt) || t('Latest'),
              })}
            </span>
          </footer>
        </section>
      </main>

      <LinkAiPublicFooter />
    </div>
  )
}
