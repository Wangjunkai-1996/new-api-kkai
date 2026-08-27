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
import { Download, ImageIcon, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter } from '@/components/ui/card'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

import type { ImageGeneration } from '../types'

const statusVariant = (
  status: ImageGeneration['status']
): 'default' | 'secondary' | 'destructive' | 'outline' => {
  if (status === 'succeeded') return 'default'
  if (status === 'failed' || status === 'archive_failed') return 'destructive'
  if (status === 'partial' || status === 'unknown') return 'outline'
  return 'secondary'
}

export function ImageGenerationCard(props: {
  generation: ImageGeneration
  highlighted?: boolean
  onDelete: (generation: ImageGeneration) => void
}) {
  const { t } = useTranslation()
  const readyAssets = props.generation.assets
    .filter((asset) => asset.state === 'ready' && asset.content_url)
    .sort((left, right) => left.position - right.position || left.id - right.id)
  let assetGridClass = 'grid-cols-1 grid-rows-1'
  if (readyAssets.length === 2) {
    assetGridClass = 'grid-cols-2 grid-rows-1'
  } else if (readyAssets.length >= 3) {
    assetGridClass = 'grid-cols-2 grid-rows-2'
  }
  return (
    <Card
      className={cn(
        'overflow-hidden p-0',
        props.highlighted && 'ring-primary ring-2 ring-offset-2'
      )}
    >
      <div
        className={cn(
          'bg-muted grid aspect-square gap-0.5 overflow-hidden',
          assetGridClass
        )}
      >
        {readyAssets.length === 0 && (
          <div className='text-muted-foreground flex size-full flex-col items-center justify-center gap-2'>
            <ImageIcon className='size-8' aria-hidden='true' />
            <span className='text-xs'>
              {t(`imageStudio.status.${props.generation.status}`)}
            </span>
          </div>
        )}
        {readyAssets.map((asset, index) => {
          const previewLabel = `${t('Preview')} ${String(index + 1)}`
          return (
            <Tooltip key={asset.id}>
              <TooltipTrigger
                render={
                  <a
                    href={asset.content_url}
                    target='_blank'
                    rel='noreferrer'
                    aria-label={previewLabel}
                    className={cn(
                      'group relative min-h-0 min-w-0 overflow-hidden',
                      readyAssets.length === 3 && index === 0 && 'row-span-2'
                    )}
                  />
                }
              >
                <img
                  src={asset.thumbnail_url || asset.content_url}
                  alt={props.generation.prompt}
                  className='size-full object-cover transition-transform group-hover:scale-[1.02]'
                  loading='lazy'
                />
              </TooltipTrigger>
              <TooltipContent>{previewLabel}</TooltipContent>
            </Tooltip>
          )
        })}
      </div>
      <CardContent className='space-y-2 px-4 pt-4'>
        <div className='flex items-center justify-between gap-2'>
          <Badge variant={statusVariant(props.generation.status)}>
            {t(`imageStudio.status.${props.generation.status}`)}
          </Badge>
          <span className='text-muted-foreground truncate text-xs'>
            {props.generation.model}
          </span>
        </div>
        <p className='line-clamp-3 text-sm'>{props.generation.prompt}</p>
        {props.generation.error_message && (
          <p className='text-destructive line-clamp-2 text-xs'>
            {props.generation.error_message}
          </p>
        )}
        <div className='text-muted-foreground flex justify-between text-xs'>
          <span>
            {t('imageStudio.outputCount', {
              ready: props.generation.succeeded_count,
              total: props.generation.requested_count,
            })}
          </span>
          <span>
            {new Date(props.generation.created_at * 1000).toLocaleString()}
          </span>
        </div>
      </CardContent>
      <CardFooter className='justify-between px-3 pb-3'>
        <div className='flex gap-1'>
          {readyAssets.map((asset, index) => {
            const downloadLabel = `${t('imageStudio.download')} ${String(index + 1)}`
            return (
              <Tooltip key={asset.id}>
                <TooltipTrigger
                  render={
                    <Button
                      size='icon-sm'
                      variant='ghost'
                      render={
                        <a
                          href={asset.download_url || asset.content_url}
                          aria-label={downloadLabel}
                        />
                      }
                    />
                  }
                >
                  <Download aria-hidden='true' />
                </TooltipTrigger>
                <TooltipContent>{downloadLabel}</TooltipContent>
              </Tooltip>
            )
          })}
        </div>
        <Button
          size='icon-sm'
          variant='ghost'
          disabled={props.generation.status === 'submitting'}
          onClick={() => props.onDelete(props.generation)}
          aria-label={t('Delete')}
        >
          <Trash2 aria-hidden='true' />
        </Button>
      </CardFooter>
    </Card>
  )
}
