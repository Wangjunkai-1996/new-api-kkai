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
import { useNavigate } from '@tanstack/react-router'
import { Download, Grid2X2, List, RotateCcw } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { FILTER_ALL, VIEW_MODES } from '@/features/pricing/constants'
import { useFilters } from '@/features/pricing/hooks/use-filters'
import { usePricingData } from '@/features/pricing/hooks/use-pricing-data'
import { cn } from '@/lib/utils'

import { LinkAiPublicHeader } from '../components/public-header'
import { ModelSquareCard } from './model-square-card'
import { ModelSquareTable } from './model-square-table'
import { ModelSquareToolbar } from './model-square-toolbar'

const PAGE_SIZE = 18
const SKELETON_IDS = ['first', 'second', 'third', 'fourth', 'fifth', 'sixth']

export function LinkAiModelSquarePage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [contextFilter, setContextFilter] = useState('all')
  const [page, setPage] = useState(1)
  const pricing = usePricingData()
  const filters = useFilters(pricing.models)

  const groups = useMemo(
    () =>
      Object.keys(pricing.usableGroup).filter(
        (group) => group && group !== 'auto'
      ),
    [pricing.usableGroup]
  )
  const contextModels = useMemo(() => {
    if (contextFilter === 'all') return filters.filteredModels
    const ranges: Record<string, [number, number]> = {
      '1-4k': [1_000, 4_000],
      '4-16k': [4_000, 16_000],
      '16-64k': [16_000, 64_000],
      '64-128k': [64_000, 128_000],
      '128-200k': [128_000, 200_000],
      '200k+': [200_000, Number.POSITIVE_INFINITY],
    }
    const [minimum, maximum] = ranges[contextFilter] || [0, Infinity]
    return filters.filteredModels.filter((model) => {
      const contextLength = model.context_length || 0
      return contextLength >= minimum && contextLength < maximum
    })
  }, [contextFilter, filters.filteredModels])
  const pageCount = Math.max(1, Math.ceil(contextModels.length / PAGE_SIZE))
  const currentPage = Math.min(page, pageCount)
  const visibleModels = contextModels.slice(
    (currentPage - 1) * PAGE_SIZE,
    currentPage * PAGE_SIZE
  )

  const downloadCatalog = () => {
    const rows = contextModels.map((model) => ({
      model: model.model_name,
      vendor: model.vendor_name || '',
      billing: model.quota_type === 0 ? 'token' : 'request',
      context: model.context_length || '',
    }))
    const blob = new Blob([JSON.stringify(rows, null, 2)], {
      type: 'application/json',
    })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = 'linkai-model-catalog.json'
    anchor.click()
    URL.revokeObjectURL(url)
  }

  let catalogContent
  if (pricing.isLoading) {
    catalogContent = (
      <div className='mt-6 grid grid-cols-1 gap-4 lg:grid-cols-2 2xl:grid-cols-3'>
        {SKELETON_IDS.map((id) => (
          <div
            key={id}
            className='skeleton-shimmer min-h-[351px] rounded-[9px] border border-[#181818]'
          />
        ))}
      </div>
    )
  } else if (visibleModels.length > 0) {
    catalogContent =
      filters.viewMode === VIEW_MODES.TABLE ? (
        <ModelSquareTable
          models={visibleModels}
          priceRate={pricing.priceRate}
          usdExchangeRate={pricing.usdExchangeRate}
          showRechargePrice={filters.showRechargePrice}
          selectedGroup={filters.groupFilter}
          onSelect={(model) =>
            navigate({
              to: '/pricing/$modelId',
              params: { modelId: model.model_name },
            })
          }
        />
      ) : (
        <div className='mt-6 grid grid-cols-1 gap-4 lg:grid-cols-2 2xl:grid-cols-3'>
          {visibleModels.map((model) => (
            <ModelSquareCard
              key={model.id || model.model_name}
              model={model}
              list={false}
              priceRate={pricing.priceRate}
              usdExchangeRate={pricing.usdExchangeRate}
              showRechargePrice={filters.showRechargePrice}
              selectedGroup={filters.groupFilter}
              onClick={() =>
                navigate({
                  to: '/pricing/$modelId',
                  params: { modelId: model.model_name },
                })
              }
            />
          ))}
        </div>
      )
  } else {
    catalogContent = (
      <div className='mt-6 flex min-h-80 flex-col items-center justify-center rounded-[9px] border border-[#181818] bg-[#0d0d0d] text-center'>
        <p className='text-lg text-white/65'>
          {t('No models match your current filters.')}
        </p>
        <button
          type='button'
          onClick={() => {
            filters.clearFilters()
            filters.clearSearch()
            setContextFilter(FILTER_ALL)
            setPage(1)
          }}
          className='mt-5 inline-flex items-center gap-2 rounded-xl border border-white/20 px-4 py-2 text-sm hover:bg-white/10'
        >
          <RotateCcw className='size-4' aria-hidden />
          {t('Clear filters')}
        </button>
      </div>
    )
  }

  return (
    <div className='min-h-svh bg-black text-white'>
      <LinkAiPublicHeader />
      <ModelSquareToolbar
        search={filters.searchInput}
        vendor={filters.vendorFilter}
        group={filters.groupFilter}
        tag={filters.tagFilter}
        quotaType={filters.quotaTypeFilter}
        context={contextFilter}
        vendors={pricing.vendors.map((vendor) => ({
          label: vendor.name,
          value: vendor.name,
        }))}
        groups={groups.map((group) => ({
          label: pricing.usableGroup[group]?.desc || group,
          value: group,
        }))}
        tags={filters.availableTags.map((tag) => ({ label: tag, value: tag }))}
        onSearchChange={(value) => {
          filters.setSearchInput(value)
          setPage(1)
        }}
        onVendorChange={(value) => {
          filters.setVendorFilter(value)
          setPage(1)
        }}
        onGroupChange={(value) => {
          filters.setGroupFilter(value)
          setPage(1)
        }}
        onTagChange={(value) => {
          filters.setTagFilter(value)
          setPage(1)
        }}
        onQuotaTypeChange={(value) => {
          filters.setQuotaTypeFilter(value)
          setPage(1)
        }}
        onContextChange={(value) => {
          setContextFilter(value)
          setPage(1)
        }}
      />

      <main className='mx-auto w-full max-w-[1920px] px-5 pb-16 sm:px-8 xl:px-[2.65vw]'>
        <header className='flex min-h-[264px] flex-col items-center justify-center text-center'>
          <h1 className='text-[clamp(2.4rem,3vw,2.875rem)] font-bold tracking-tight text-[#eee]'>
            {t('Model Square')}
          </h1>
          <p className='mt-3 text-base text-[#a1a1a1] sm:text-xl'>
            {t('Model prices are converted using the recharge exchange rate.')}
          </p>
        </header>

        <section aria-label={t('Model catalog')}>
          <div className='flex min-h-[92px] flex-wrap items-center gap-4 rounded-[9px] border border-[#181818] bg-[#0d0d0d] px-4 py-4 sm:px-6'>
            <div className='flex size-14 items-center justify-center rounded-xl bg-[#262626] text-xl font-semibold'>
              A
            </div>
            <div className='flex items-baseline gap-3'>
              <h2 className='text-xl text-[#f9f9f9]'>{t('All Vendors')}</h2>
              <span className='text-base text-[#606060]'>
                {t('{{count}} models', { count: contextModels.length })}
              </span>
            </div>
            <div className='ml-auto flex items-center gap-2'>
              <button
                type='button'
                onClick={downloadCatalog}
                className='flex size-11 items-center justify-center rounded-xl text-white/80 transition hover:bg-white/10 hover:text-white'
                aria-label={t('Download')}
              >
                <Download className='size-5' aria-hidden />
              </button>
              <button
                type='button'
                role='switch'
                aria-checked={filters.showRechargePrice}
                onClick={() =>
                  filters.setShowRechargePrice(!filters.showRechargePrice)
                }
                className='flex h-[45px] items-center gap-2 rounded-xl border border-[#404040] px-3 text-lg'
              >
                <span
                  className={cn(
                    'flex h-6 w-[42px] items-center rounded-full bg-[#262626] p-0.5 transition',
                    filters.showRechargePrice && 'bg-[#7357ff]'
                  )}
                >
                  <span
                    className={cn(
                      'size-5 rounded-full bg-white transition-transform',
                      filters.showRechargePrice && 'translate-x-[18px]'
                    )}
                  />
                </span>
                {t('Recharge Price')}
              </button>
              <div className='flex h-11 rounded-xl bg-[#262626] p-1'>
                <button
                  type='button'
                  onClick={() => filters.setViewMode(VIEW_MODES.CARD)}
                  className={cn(
                    'flex size-9 items-center justify-center rounded-[9px] text-white/35',
                    filters.viewMode === VIEW_MODES.CARD &&
                      'border border-[#404040] bg-[#262626] text-white'
                  )}
                  aria-label={t('Card view')}
                >
                  <Grid2X2 className='size-5' aria-hidden />
                </button>
                <button
                  type='button'
                  onClick={() => filters.setViewMode(VIEW_MODES.TABLE)}
                  className={cn(
                    'flex size-9 items-center justify-center rounded-[9px] text-white/35',
                    filters.viewMode === VIEW_MODES.TABLE &&
                      'border border-[#404040] bg-[#262626] text-white'
                  )}
                  aria-label={t('List view')}
                >
                  <List className='size-5' aria-hidden />
                </button>
              </div>
            </div>
          </div>

          {catalogContent}

          {pageCount > 1 && (
            <nav
              className='mt-10 flex items-center justify-center gap-3'
              aria-label={t('Pagination')}
            >
              <button
                type='button'
                disabled={currentPage === 1}
                onClick={() => setPage((value) => Math.max(1, value - 1))}
                className='rounded-lg border border-white/15 px-4 py-2 text-sm disabled:opacity-35'
              >
                {t('Previous')}
              </button>
              <span className='text-sm text-white/55'>
                {currentPage} / {pageCount}
              </span>
              <button
                type='button'
                disabled={currentPage === pageCount}
                onClick={() =>
                  setPage((value) => Math.min(pageCount, value + 1))
                }
                className='rounded-lg border border-white/15 px-4 py-2 text-sm disabled:opacity-35'
              >
                {t('Next')}
              </button>
            </nav>
          )}
        </section>
      </main>
    </div>
  )
}
