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
import {
  ArrowUpRight,
  ImageIcon,
  Layers2,
  MoreHorizontal,
  RotateCcw,
  Sparkles,
} from 'lucide-react'
import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import type { ImageGeneration } from '../types'

export function ImageGenerationCard(props: {
  generation: ImageGeneration
  highlighted?: boolean
  onDetails: (generation: ImageGeneration) => void
  onRegenerate: (generation: ImageGeneration) => void
}) {
  const { t } = useTranslation()
  const reduceMotion = useReducedMotion()
  const readyAssets = props.generation.assets
    .filter((asset) => asset.state === 'ready')
    .sort((left, right) => left.position - right.position || left.id - right.id)
  const cover = readyAssets[0]
  const [naturalSize, setNaturalSize] = useState({ assetId: 0, ratio: 4 / 3 })
  const [loadedAssetId, setLoadedAssetId] = useState<number>()
  const submitting = props.generation.status === 'submitting'
  const retryable = props.generation.status === 'failed'
  const canRegenerate =
    retryable ||
    props.generation.status === 'succeeded' ||
    props.generation.status === 'partial'
  const ratio =
    cover && cover.width > 0 && cover.height > 0
      ? cover.width / cover.height
      : naturalSize.ratio

  return (
    <article
      className={cn(
        'image-work-card group bg-card overflow-hidden rounded-2xl border border-border/60 shadow-sm transition-[transform,box-shadow,border-color] duration-200 hover:-translate-y-0.5 hover:border-primary/35 hover:shadow-xl focus-within:border-primary/50 focus-within:ring-2 focus-within:ring-primary/20 motion-reduce:transform-none motion-reduce:transition-none',
        props.highlighted &&
          'ring-primary ring-2 ring-offset-2 ring-offset-background',
        submitting && 'image-work-card-pending'
      )}
    >
      <div
        className='bg-muted/40 relative overflow-hidden'
        style={{ aspectRatio: ratio }}
      >
        <AnimatePresence initial={false}>
          {cover?.content_url ? (
            <motion.button
              key={cover.id}
              type='button'
              aria-label={t('View image details')}
              onClick={() => props.onDetails(props.generation)}
              className='group/preview focus-visible:ring-primary absolute inset-0 size-full cursor-zoom-in overflow-hidden text-left outline-none focus-visible:ring-2 focus-visible:ring-inset'
              initial={reduceMotion ? false : { opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: reduceMotion ? 0 : 0.32 }}
            >
              {loadedAssetId !== cover.id && (
                <span
                  className='text-muted-foreground absolute inset-0 flex items-center justify-center'
                  aria-hidden='true'
                >
                  <ImageIcon className='size-5' />
                </span>
              )}
              <motion.img
                src={cover.thumbnail_url || cover.content_url}
                width={cover.width || undefined}
                height={cover.height || undefined}
                alt={props.generation.prompt}
                className='size-full object-contain transition-transform duration-500 ease-out group-hover/preview:scale-[1.025] group-focus-visible/preview:scale-[1.025] motion-reduce:transform-none motion-reduce:transition-none'
                loading='lazy'
                decoding='async'
                initial={false}
                animate={{ opacity: loadedAssetId === cover.id ? 1 : 0 }}
                transition={{ duration: reduceMotion ? 0 : 0.32 }}
                onLoad={(event) => {
                  const image = event.currentTarget
                  setLoadedAssetId(cover.id)
                  if (
                    (cover.width <= 0 || cover.height <= 0) &&
                    image.naturalWidth > 0 &&
                    image.naturalHeight > 0 &&
                    naturalSize.assetId !== cover.id
                  ) {
                    setNaturalSize({
                      assetId: cover.id,
                      ratio: image.naturalWidth / image.naturalHeight,
                    })
                  }
                }}
              />
              <span className='pointer-events-none absolute inset-0 bg-gradient-to-t from-black/35 via-transparent to-transparent opacity-0 transition-opacity duration-200 group-hover/preview:opacity-100 group-focus-visible/preview:opacity-100 motion-reduce:transition-none' />
              <span className='pointer-events-none absolute right-3 bottom-3 flex size-9 translate-y-1 items-center justify-center rounded-full bg-black/45 text-white opacity-0 backdrop-blur-sm transition-[opacity,transform] duration-200 group-hover/preview:translate-y-0 group-hover/preview:opacity-100 group-focus-visible/preview:translate-y-0 group-focus-visible/preview:opacity-100 motion-reduce:transform-none motion-reduce:transition-none'>
                <ArrowUpRight className='size-4' aria-hidden='true' />
              </span>
            </motion.button>
          ) : (
            <motion.button
              key='pending'
              type='button'
              className='focus-visible:ring-primary absolute inset-0 flex size-full flex-col items-center justify-center gap-3 px-4 text-center outline-none focus-visible:ring-2 focus-visible:ring-inset'
              onClick={() => props.onDetails(props.generation)}
              aria-label={t('View image details')}
              initial={false}
              exit={{ opacity: 0 }}
              transition={{ duration: reduceMotion ? 0 : 0.24 }}
            >
              <span
                className={cn(
                  'bg-background/65 text-muted-foreground flex size-12 items-center justify-center rounded-2xl border shadow-sm',
                  submitting && 'text-primary'
                )}
              >
                {submitting ? (
                  <Sparkles className='size-5' aria-hidden='true' />
                ) : (
                  <ImageIcon className='size-5' aria-hidden='true' />
                )}
              </span>
              <span className='text-muted-foreground max-w-full text-xs'>
                {props.generation.status === 'succeeded'
                  ? t('Preparing preview')
                  : t(`imageStudio.status.${props.generation.status}`)}
              </span>
              {props.generation.error_message && (
                <span className='text-muted-foreground line-clamp-2 text-xs'>
                  {props.generation.error_message}
                </span>
              )}
            </motion.button>
          )}
        </AnimatePresence>
        {props.generation.status !== 'succeeded' && cover && (
          <span className='pointer-events-none absolute top-3 left-3 rounded-full bg-black/65 px-2.5 py-1 text-[10px] font-medium text-white backdrop-blur-sm'>
            {t(`imageStudio.status.${props.generation.status}`)}
          </span>
        )}
        {props.generation.requested_count > 1 && (
          <span
            className='pointer-events-none absolute top-3 right-3 inline-flex items-center gap-1 rounded-full bg-black/65 px-2 py-1 text-[10px] font-medium text-white backdrop-blur-sm'
            aria-label={t('Images in this batch: {{count}}', {
              count: props.generation.requested_count,
            })}
          >
            <Layers2 className='size-3' aria-hidden='true' />
            {props.generation.requested_count}
          </span>
        )}
      </div>
      <div className='space-y-2 px-3 pt-3 pb-2.5'>
        <p
          className='truncate text-xs leading-5'
          title={props.generation.prompt}
        >
          {props.generation.prompt}
        </p>
        <div className='flex min-w-0 items-center justify-between gap-2'>
          <span className='text-muted-foreground hidden min-w-0 truncate text-[10px] sm:block'>
            {props.generation.model}
          </span>
          <div className='flex min-w-0 flex-1 items-center justify-end gap-0.5'>
            {canRegenerate && (
              <Button
                size='sm'
                variant='ghost'
                className='text-primary min-h-11 min-w-0 shrink px-1.5 text-xs sm:min-h-8'
                onClick={() => props.onRegenerate(props.generation)}
              >
                <RotateCcw className='size-3' aria-hidden='true' />
                <span className='truncate'>
                  {retryable ? t('Retry generation') : t('Generate again')}
                </span>
              </Button>
            )}
            <Button
              size='icon-sm'
              variant='ghost'
              className='min-h-11 min-w-9 sm:min-h-8 sm:min-w-8'
              onClick={() => props.onDetails(props.generation)}
              aria-label={t('View details')}
            >
              <MoreHorizontal aria-hidden='true' />
            </Button>
          </div>
        </div>
      </div>
    </article>
  )
}
