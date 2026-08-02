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
import { ImageIcon, LoaderCircle } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

import { useImageSamples } from '../queries'
import type { ImageSample } from '../types'

export function ImageSampleGallery(props: {
  tokenId: number | null
  selectedSampleId?: number
  onSelect: (sample: ImageSample) => void
}) {
  const { t } = useTranslation()
  const query = useImageSamples(props.tokenId)
  const samples = useMemo(
    () => query.data?.pages.flatMap((page) => page.items) ?? [],
    [query.data]
  )

  return (
    <section className='min-w-0 flex-1 overflow-y-auto p-3 sm:p-4'>
      <div className='mb-3'>
        <h2 className='text-sm font-semibold'>
          {t('imageStudio.inspiration')}
        </h2>
        <p className='text-muted-foreground text-xs'>
          {t('imageStudio.inspirationDescription')}
        </p>
      </div>
      {query.isLoading && (
        <div className='grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-4'>
          {Array.from({ length: 8 }, (_, index) => (
            <Skeleton
              key={`image-sample-skeleton-${index}`}
              className='aspect-square rounded-xl'
            />
          ))}
        </div>
      )}
      {query.isError && (
        <Empty className='min-h-64'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <ImageIcon aria-hidden='true' />
            </EmptyMedia>
            <EmptyTitle>{t('imageStudio.samplesFailed')}</EmptyTitle>
            <EmptyDescription>
              {t('imageStudio.samplesFailedDescription')}
            </EmptyDescription>
          </EmptyHeader>
          <Button variant='outline' onClick={() => void query.refetch()}>
            {t('Retry')}
          </Button>
        </Empty>
      )}
      {!query.isLoading && !query.isError && samples.length === 0 && (
        <Empty className='min-h-64'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <ImageIcon aria-hidden='true' />
            </EmptyMedia>
            <EmptyTitle>{t('imageStudio.noSamples')}</EmptyTitle>
            <EmptyDescription>
              {t('imageStudio.noSamplesDescription')}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
      {samples.length > 0 && (
        <div className='grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-4'>
          {samples.map((sample) => {
            const imageUrl =
              sample.asset.thumbnail_url || sample.asset.content_url
            return (
              <button
                key={sample.id}
                type='button'
                className={cn(
                  'group bg-muted relative aspect-square overflow-hidden rounded-xl border text-left transition-shadow hover:shadow-md',
                  props.selectedSampleId === sample.id &&
                    'ring-primary ring-2 ring-offset-2'
                )}
                onClick={() => props.onSelect(sample)}
              >
                {imageUrl && (
                  <img
                    src={imageUrl}
                    alt={sample.title}
                    className='size-full object-cover transition-transform group-hover:scale-[1.02]'
                    loading='lazy'
                  />
                )}
                <span className='absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/80 to-transparent px-3 pt-8 pb-2 text-sm font-medium text-white'>
                  {sample.title}
                </span>
              </button>
            )
          })}
        </div>
      )}
      {query.hasNextPage && (
        <div className='flex justify-center py-5'>
          <Button
            variant='outline'
            disabled={query.isFetchingNextPage}
            onClick={() => void query.fetchNextPage()}
          >
            {query.isFetchingNextPage && (
              <LoaderCircle
                className='animate-spin motion-reduce:animate-none'
                aria-hidden='true'
              />
            )}
            {t('imageStudio.loadMore')}
          </Button>
        </div>
      )}
    </section>
  )
}
