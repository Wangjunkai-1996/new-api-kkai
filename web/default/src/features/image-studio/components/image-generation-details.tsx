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
  Download,
  ImageIcon,
  RotateCcw,
  RotateCw,
  Trash2,
  X,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { toIntlLocale } from '@/i18n/languages'
import { formatBillingCurrencyFromUSD } from '@/lib/currency'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import type { ImageGeneration } from '../types'

export function ImageGenerationDetails(props: {
  generation: ImageGeneration | null
  refreshing: boolean
  onClose: () => void
  onRegenerate: (generation: ImageGeneration) => void
  onDelete: (generation: ImageGeneration) => void
  onRefresh: () => void
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.language)
  const currency = useSystemConfigStore((state) => state.config.currency)
  const quotaPerUnit =
    currency.quotaPerUnit > 0
      ? currency.quotaPerUnit
      : DEFAULT_CURRENCY_CONFIG.quotaPerUnit
  const generation = props.generation
  const readyAssets =
    generation?.assets
      .filter((asset) => asset.state === 'ready' && asset.content_url)
      .sort(
        (left, right) => left.position - right.position || left.id - right.id
      ) ?? []
  const canRegenerate =
    generation && ['succeeded', 'partial', 'failed'].includes(generation.status)

  return (
    <Dialog
      open={generation !== null}
      onOpenChange={(open) => !open && props.onClose()}
    >
      <DialogContent
        showCloseButton={false}
        className='flex max-h-[calc(100dvh-1.5rem)] w-[calc(100vw-1.5rem)] max-w-none flex-col gap-0 overflow-hidden p-0 motion-reduce:animate-none sm:max-w-[1120px]'
      >
        <DialogHeader className='shrink-0 border-b px-5 py-4 pr-14'>
          <DialogTitle>{t('Image details')}</DialogTitle>
          <DialogDescription>
            {t('All images and settings from this generation.')}
          </DialogDescription>
        </DialogHeader>
        <DialogClose
          render={
            <Button
              size='icon'
              variant='ghost'
              className='absolute top-3 right-3 min-h-11 min-w-11'
              aria-label={t('Close')}
            />
          }
        >
          <X aria-hidden='true' />
        </DialogClose>
        {generation && (
          <div className='flex min-h-0 flex-1 flex-col overflow-y-auto lg:flex-row lg:overflow-hidden'>
            <div className='bg-muted/20 min-w-0 flex-1 space-y-5 p-4 lg:overflow-y-auto lg:p-6'>
              {readyAssets.length === 0 && (
                <div className='text-muted-foreground flex min-h-64 flex-col items-center justify-center gap-3 rounded-xl border border-dashed'>
                  <ImageIcon className='size-8' aria-hidden='true' />
                  <span>{t(`imageStudio.status.${generation.status}`)}</span>
                </div>
              )}
              {readyAssets.map((asset, index) => (
                <figure key={asset.id} className='space-y-2'>
                  <a
                    href={asset.content_url}
                    target='_blank'
                    rel='noreferrer'
                    aria-label={`${t('Preview')} ${String(index + 1)}`}
                    className='focus-visible:ring-primary block overflow-hidden rounded-xl outline-none focus-visible:ring-2'
                  >
                    <img
                      src={asset.content_url}
                      alt={generation.prompt}
                      width={asset.width || undefined}
                      height={asset.height || undefined}
                      className='mx-auto max-h-[74dvh] w-full rounded-xl object-contain'
                      loading='lazy'
                      decoding='async'
                    />
                  </a>
                  <figcaption className='text-muted-foreground flex items-center justify-between gap-2 text-xs'>
                    <span>
                      {index + 1} / {readyAssets.length}
                      {asset.width > 0 && asset.height > 0
                        ? ` · ${String(asset.width)} × ${String(asset.height)}`
                        : ''}
                    </span>
                    <Button
                      variant='outline'
                      size='sm'
                      className='min-h-11 sm:min-h-8'
                      render={
                        <a
                          href={asset.download_url || asset.content_url}
                          aria-label={`${t('imageStudio.download')} ${String(index + 1)}`}
                        />
                      }
                    >
                      <Download aria-hidden='true' />
                      {t('imageStudio.download')}
                    </Button>
                  </figcaption>
                </figure>
              ))}
            </div>
            <aside className='flex shrink-0 flex-col border-t lg:w-[340px] lg:overflow-y-auto lg:border-t-0 lg:border-l'>
              <div className='flex-1 space-y-5 p-5'>
                <div className='flex flex-wrap items-center gap-2'>
                  <Badge
                    variant={
                      generation.status === 'failed' ||
                      generation.status === 'archive_failed'
                        ? 'destructive'
                        : 'secondary'
                    }
                  >
                    {t(`imageStudio.status.${generation.status}`)}
                  </Badge>
                  <span className='text-muted-foreground text-xs'>
                    {t('imageStudio.outputCount', {
                      ready: generation.succeeded_count,
                      total: generation.requested_count,
                    })}
                  </span>
                </div>
                {generation.error_message && (
                  <p className='text-destructive bg-destructive/5 rounded-lg p-3 text-xs break-words'>
                    {generation.error_message}
                  </p>
                )}
                <section className='space-y-2'>
                  <h3 className='text-xs font-medium'>{t('Prompt')}</h3>
                  <p className='text-sm leading-relaxed break-words whitespace-pre-wrap'>
                    {generation.prompt}
                  </p>
                </section>
                <dl className='space-y-3 text-xs'>
                  <div className='flex justify-between gap-4'>
                    <dt className='text-muted-foreground'>{t('Model')}</dt>
                    <dd className='text-right break-all'>{generation.model}</dd>
                  </div>
                  <div className='flex justify-between gap-4'>
                    <dt className='text-muted-foreground'>{t('Created at')}</dt>
                    <dd className='text-right'>
                      {new Date(generation.created_at * 1000).toLocaleString(
                        locale
                      )}
                    </dd>
                  </div>
                  <div className='flex justify-between gap-4'>
                    <dt className='text-muted-foreground'>{t('Quota used')}</dt>
                    <dd>
                      {formatBillingCurrencyFromUSD(
                        generation.final_quota / quotaPerUnit,
                        {
                          digitsLarge: 2,
                          digitsSmall: 4,
                          abbreviate: false,
                        }
                      )}
                    </dd>
                  </div>
                  {Object.entries(generation.parameters).map(([key, value]) => (
                    <div key={key} className='flex justify-between gap-4'>
                      <dt className='text-muted-foreground break-all'>{key}</dt>
                      <dd className='text-right break-all'>{String(value)}</dd>
                    </div>
                  ))}
                </dl>
              </div>
              <div className='bg-background sticky bottom-0 space-y-2 border-t p-4'>
                {canRegenerate && (
                  <Button
                    className='min-h-11 w-full'
                    onClick={() => props.onRegenerate(generation)}
                  >
                    <RotateCcw aria-hidden='true' />
                    {generation.status === 'failed'
                      ? t('Retry generation')
                      : t('Generate again')}
                  </Button>
                )}
                {(generation.status === 'submitting' ||
                  generation.status === 'unknown') && (
                  <Button
                    variant='outline'
                    className='min-h-11 w-full'
                    disabled={props.refreshing}
                    onClick={props.onRefresh}
                  >
                    <RotateCw
                      className={
                        props.refreshing
                          ? 'animate-spin motion-reduce:animate-none'
                          : undefined
                      }
                      aria-hidden='true'
                    />
                    {t('Check status')}
                  </Button>
                )}
                <Button
                  variant='ghost'
                  className='text-muted-foreground hover:text-destructive min-h-11 w-full'
                  disabled={generation.status === 'submitting'}
                  onClick={() => props.onDelete(generation)}
                >
                  <Trash2 aria-hidden='true' />
                  {t('Delete this batch')}
                </Button>
              </div>
            </aside>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
