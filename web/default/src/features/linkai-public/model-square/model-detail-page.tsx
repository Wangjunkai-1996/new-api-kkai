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
import { Link, useParams } from '@tanstack/react-router'
import {
  ArrowLeft,
  Box,
  BrainCircuit,
  CalendarDays,
  Check,
  Copy,
  ExternalLink,
  Leaf,
  Sparkles,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { usePricingData } from '@/features/pricing/hooks/use-pricing-data'
import { parseTags } from '@/features/pricing/lib/filters'
import { isTokenBasedModel } from '@/features/pricing/lib/model-helpers'
import {
  formatFixedPrice,
  formatGroupPrice,
} from '@/features/pricing/lib/price'
import type { PricingModel } from '@/features/pricing/types'
import { getLobeIcon } from '@/lib/lobe-icon'

import { LinkAiPublicHeader } from '../components/public-header'
import { ModelAvailabilityChart } from './model-availability-chart'
import { ModelExperienceDrawer } from './model-experience-drawer'

function compactTokenCount(value?: number) {
  if (!value) return '—'
  if (value >= 1_000_000) return `${value / 1_000_000}M`
  if (value >= 1_000) return `${Math.round(value / 1_000)}K`
  return String(value)
}

function groupDescription(
  usableGroup: Record<string, { desc: string; ratio: number }>,
  group: string
) {
  const meta = usableGroup[group] as unknown
  if (typeof meta === 'string') return meta
  if (meta && typeof meta === 'object' && 'desc' in meta) {
    return String((meta as { desc?: unknown }).desc || group)
  }
  return group
}

function ModelMetric(props: {
  icon: React.ComponentType<{ className?: string }>
  label: string
  value: string
}) {
  const Icon = props.icon
  return (
    <div className='flex min-h-[105px] items-center gap-4 rounded-2xl border border-white/10 bg-[#111] px-6'>
      <Icon className='size-7 shrink-0 text-white/55' aria-hidden />
      <dl>
        <dt className='text-sm text-white/45'>{props.label}</dt>
        <dd className='mt-1 text-xl font-semibold text-[#eeeeee]'>
          {props.value}
        </dd>
      </dl>
    </div>
  )
}

function ModelGroupTable(props: {
  model: PricingModel
  groupRatio: Record<string, number>
  usableGroup: Record<string, { desc: string; ratio: number }>
  priceRate: number
  usdExchangeRate: number
}) {
  const { t } = useTranslation()
  const groups = props.model.enable_groups?.length
    ? props.model.enable_groups
    : ['default']

  return (
    <section className='overflow-hidden rounded-2xl border border-white/10 bg-[#0c0c0c]'>
      <header className='px-7 py-6'>
        <h2 className='text-lg text-[#eeeeee]'>{t('Available Groups')}</h2>
        <p className='mt-1 text-sm text-white/55'>
          {t('Prices vary by token group. Unit: million tokens (M)')}
        </p>
      </header>
      <div className='overflow-x-auto'>
        <table className='w-full min-w-[760px] border-collapse text-left text-sm'>
          <thead className='bg-[#111] text-white/75'>
            <tr>
              <th className='px-7 py-4 font-medium'>{t('Token Group')}</th>
              <th className='px-5 py-4 font-medium'>{t('Description')}</th>
              <th className='px-5 py-4 text-right font-medium'>
                {t('Group Ratio')}
              </th>
              <th className='px-5 py-4 text-right font-medium'>
                {t('Input Price')}
              </th>
              <th className='px-7 py-4 text-right font-medium'>
                {t('Output Price')}
              </th>
            </tr>
          </thead>
          <tbody>
            {groups.map((group) => {
              const ratio = props.groupRatio[group] ?? 1
              const inputPrice = isTokenBasedModel(props.model)
                ? formatGroupPrice(
                    props.model,
                    group,
                    'input',
                    'M',
                    false,
                    props.priceRate,
                    props.usdExchangeRate,
                    props.groupRatio
                  )
                : formatFixedPrice(
                    props.model,
                    group,
                    false,
                    props.priceRate,
                    props.usdExchangeRate,
                    props.groupRatio
                  )
              const outputPrice = isTokenBasedModel(props.model)
                ? formatGroupPrice(
                    props.model,
                    group,
                    'output',
                    'M',
                    false,
                    props.priceRate,
                    props.usdExchangeRate,
                    props.groupRatio
                  )
                : inputPrice
              return (
                <tr key={group} className='border-t border-white/[0.07]'>
                  <td className='px-7 py-4 text-white'>{group}</td>
                  <td className='px-5 py-4 text-white/55'>
                    {groupDescription(props.usableGroup, group)}
                  </td>
                  <td className='px-5 py-4 text-right'>{ratio}×</td>
                  <td className='px-5 py-4 text-right'>{inputPrice}/M</td>
                  <td className='px-7 py-4 text-right'>{outputPrice}/M</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </section>
  )
}

export function LinkAiModelDetailPage() {
  const { t } = useTranslation()
  const { modelId } = useParams({ from: '/pricing/$modelId/' })
  const pricing = usePricingData()
  const [experienceOpen, setExperienceOpen] = useState(false)
  const [copied, setCopied] = useState(false)
  const model = useMemo(
    () => pricing.models.find((item) => item.model_name === modelId),
    [modelId, pricing.models]
  )

  if (pricing.isLoading) {
    return (
      <div className='min-h-svh bg-black text-white'>
        <LinkAiPublicHeader />
        <main className='mx-auto max-w-[1368px] px-5 py-12 sm:px-8'>
          <div className='skeleton-shimmer h-9 w-80 rounded-lg' />
          <div className='mt-12 grid grid-cols-2 gap-4 lg:grid-cols-5'>
            {['a', 'b', 'c', 'd', 'e'].map((key) => (
              <div key={key} className='skeleton-shimmer h-28 rounded-2xl' />
            ))}
          </div>
        </main>
      </div>
    )
  }

  if (!model) {
    return (
      <div className='min-h-svh bg-black text-white'>
        <LinkAiPublicHeader />
        <main className='mx-auto flex min-h-[60vh] max-w-2xl flex-col items-center justify-center px-5 text-center'>
          <h1 className='text-3xl font-semibold'>{t('Model not found')}</h1>
          <p className='mt-3 text-white/55'>
            {t("The model you're looking for doesn't exist.")}
          </p>
          <Link
            to='/pricing'
            className='mt-7 rounded-full border border-white/20 px-6 py-3 hover:bg-white/10'
          >
            {t('Back to Models')}
          </Link>
        </main>
      </div>
    )
  }

  const iconKey = model.icon || model.vendor_icon
  const modelIcon = iconKey ? getLobeIcon(iconKey, 45) : null
  const tags = [
    ...(model.supported_endpoint_types || []),
    ...parseTags(model.tags),
  ].slice(0, 4)

  const copyLink = async () => {
    await navigator.clipboard.writeText(window.location.href)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1600)
  }

  return (
    <div className='min-h-svh bg-black text-[#eeeeee]'>
      <LinkAiPublicHeader />
      <main className='mx-auto w-full max-w-[1424px] px-5 pt-10 pb-24 sm:px-8 lg:pt-12'>
        <nav
          className='flex items-center gap-1 text-sm text-white/55'
          aria-label={t('Breadcrumb')}
        >
          <Link
            to='/pricing'
            className='inline-flex items-center gap-2 hover:text-white'
          >
            <ArrowLeft className='size-4' aria-hidden />
            {t('Back')}
          </Link>
          <span>&gt;</span>
          <span>{model.vendor_name || t('Other')}</span>
          <span>&gt;</span>
          <span className='text-white'>{model.model_name}</span>
        </nav>

        <section className='mt-12 flex flex-col gap-7 sm:flex-row sm:items-start'>
          <div className='flex min-w-0 flex-1 items-center gap-5'>
            <div className='flex size-20 shrink-0 items-center justify-center rounded-3xl border border-white/15 bg-[#171717]'>
              {modelIcon || (
                <span className='text-3xl font-semibold'>
                  {model.model_name.charAt(0).toUpperCase()}
                </span>
              )}
            </div>
            <div className='min-w-0'>
              <div className='flex items-center gap-3'>
                <h1 className='truncate text-2xl sm:text-3xl'>
                  {model.model_name}
                </h1>
                <button
                  type='button'
                  onClick={() =>
                    navigator.clipboard.writeText(model.model_name)
                  }
                  className='text-white/70 hover:text-white'
                  aria-label={t('Copy model name')}
                >
                  <Copy className='size-5' aria-hidden />
                </button>
              </div>
              <div className='mt-3 flex flex-wrap gap-2'>
                {(tags.length ? tags : [t('Text chat')]).map((tag) => (
                  <span
                    key={tag}
                    className='rounded-full border border-white/15 px-3 py-1 text-xs text-white/80'
                  >
                    {tag}
                  </span>
                ))}
              </div>
            </div>
          </div>
          <div className='flex flex-wrap gap-2 sm:justify-end'>
            <button
              type='button'
              onClick={() => setExperienceOpen(true)}
              className='inline-flex items-center gap-2 rounded-full border border-white/15 bg-[#111] px-5 py-3 transition hover:border-white/35 hover:bg-[#191919]'
            >
              <Sparkles className='size-4' aria-hidden />
              {t('Online Experience')}
            </button>
            <button
              type='button'
              onClick={copyLink}
              className='inline-flex items-center gap-2 rounded-full border border-white/15 bg-[#111] px-5 py-3 transition hover:border-white/35 hover:bg-[#191919]'
            >
              {copied ? (
                <Check className='size-4' aria-hidden />
              ) : (
                <Copy className='size-4' aria-hidden />
              )}
              {copied ? t('Copied') : t('Copy link')}
            </button>
          </div>
        </section>

        <section className='mt-9 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-5'>
          <ModelMetric
            icon={BrainCircuit}
            label={t('Context Length')}
            value={compactTokenCount(model.context_length)}
          />
          <ModelMetric
            icon={Leaf}
            label={t('Maximum Output')}
            value={compactTokenCount(model.max_output_tokens)}
          />
          <ModelMetric
            icon={CalendarDays}
            label={t('Release Date')}
            value={model.release_date || t('Not available')}
          />
          <ModelMetric
            icon={CalendarDays}
            label={t('Knowledge Cutoff')}
            value={model.knowledge_cutoff || t('Not available')}
          />
          <ModelMetric
            icon={Box}
            label={t('Model Ratio / Completion Ratio')}
            value={`${model.model_ratio} / ${model.completion_ratio}`}
          />
        </section>

        <section className='mt-6 rounded-2xl border border-white/10 bg-[#0c0c0c] px-7 py-7'>
          <h2 className='text-lg'>{t('Model Description')}</h2>
          <p className='mt-4 leading-8 text-white/75'>
            {model.description || t('No description available.')}
            {model.vendor_description && (
              <a
                href={model.vendor_description}
                target='_blank'
                rel='noreferrer'
                className='ml-2 inline-flex items-center gap-1 text-[#a895ff] hover:text-white'
              >
                {t('View provider details')}
                <ExternalLink className='size-3' aria-hidden />
              </a>
            )}
          </p>
        </section>

        <div className='mt-7 space-y-7'>
          <ModelGroupTable
            model={model}
            groupRatio={pricing.groupRatio}
            usableGroup={pricing.usableGroup}
            priceRate={pricing.priceRate}
            usdExchangeRate={pricing.usdExchangeRate}
          />
          <ModelAvailabilityChart model={model} />
        </div>
      </main>

      <ModelExperienceDrawer
        open={experienceOpen}
        model={model}
        priceRate={pricing.priceRate}
        usdExchangeRate={pricing.usdExchangeRate}
        onClose={() => setExperienceOpen(false)}
      />
    </div>
  )
}
