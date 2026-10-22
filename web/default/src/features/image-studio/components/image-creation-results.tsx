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
  ChevronDown,
  CircleAlert,
  CircleCheck,
  Download,
  ImageIcon,
  ImagePlus,
  Lightbulb,
  Maximize2,
  RotateCw,
  Sparkles,
} from 'lucide-react'
import { useReducedMotion } from 'motion/react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { toIntlLocale } from '@/i18n/languages'
import { formatBillingCurrencyFromUSD } from '@/lib/currency'
import { signedStudioMediaUrl, useStudioMediaUrls } from '@/lib/studio-media'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { getImageGenerationErrorKey } from '../image-generation-error'
import type { ImageAsset, ImageGeneration } from '../types'

export type ImageCreationResultsProps = {
  generation?: ImageGeneration
  previousGeneration?: ImageGeneration
  history: ImageGeneration[]
  selectedAssetId?: number
  onSelectGeneration: (id: number) => void
  onSelectAsset: (id: number) => void
  onRegenerate: (generation: ImageGeneration) => void
  onEditAsset: (asset: ImageAsset, generation: ImageGeneration) => void
  onCheckStatus: (generation: ImageGeneration) => void
  onExploreSamples?: () => void
  pendingEdit?: boolean
  checkingStatus?: boolean
  activity?: {
    phase: 'idle' | 'preparing' | 'uploading' | 'validating' | 'submitting'
    progress?: number
  }
}

// The keyed image owns its decoding lifecycle; an old decode cannot reveal a
// newly selected image. The parent remembers revealed assets across switches.
function CreationImage(props: {
  asset: ImageAsset
  source: string
  prompt: string
  revealed: Set<number>
  onOpen?: () => void
  onRetry: () => void
  onDecoded?: (source: string) => void
}) {
  const { t } = useTranslation()
  const reduceMotion = useReducedMotion()
  const imageRef = useRef<HTMLImageElement>(null)
  const [decoded, setDecoded] = useState(false)
  const [failed, setFailed] = useState(false)
  const [revealing, setRevealing] = useState(false)
  const [view, setView] = useState<'fit' | 'actual'>('fit')
  const [naturalSize, setNaturalSize] = useState({ width: 0, height: 0 })
  const width = naturalSize.width || props.asset.width
  const height = naturalSize.height || props.asset.height
  const ImageSurface = props.onOpen ? 'button' : 'div'

  return (
    <>
      <div
        className='image-create-preview-viewport'
        data-view={view}
        role='region'
        aria-label={t('Image details')}
        tabIndex={view === 'actual' ? 0 : undefined}
      >
        <ImageSurface
          type={props.onOpen ? 'button' : undefined}
          className='image-create-image-open'
          aria-label={props.onOpen ? t('View image details') : undefined}
          disabled={props.onOpen ? !decoded || failed : undefined}
          onClick={props.onOpen}
          aria-busy={!decoded && !failed}
        >
          <img
            ref={imageRef}
            src={props.source}
            alt={props.prompt}
            width={width || undefined}
            height={height || undefined}
            decoding='async'
            className={cn(
              'image-create-main-image',
              revealing && 'is-revealing'
            )}
            style={{ opacity: decoded && !failed ? 1 : 0 }}
            onAnimationEnd={() => setRevealing(false)}
            onLoad={async (event) => {
              const image = event.currentTarget
              const source = props.source
              try {
                await image.decode?.()
              } catch {
                if (
                  imageRef.current === image &&
                  image.getAttribute('src') === source
                ) {
                  setFailed(true)
                }
                return
              }
              if (
                imageRef.current !== image ||
                image.getAttribute('src') !== source
              ) {
                return
              }
              setNaturalSize({
                width: image.naturalWidth,
                height: image.naturalHeight,
              })
              setFailed(false)
              setDecoded(true)
              setRevealing(!reduceMotion && !props.revealed.has(props.asset.id))
              props.revealed.add(props.asset.id)
              props.onDecoded?.(source)
            }}
            onError={() => setFailed(true)}
          />
        </ImageSurface>
      </div>
      {!decoded && !failed && (
        <span className='image-create-preview-loading' role='status'>
          <ImageIcon aria-hidden='true' />
          {t('Preparing preview')}
        </span>
      )}
      {failed && (
        <div className='image-create-empty-result' role='status'>
          <ImageIcon aria-hidden='true' />
          <strong>{t('Preview unavailable')}</strong>
          <Button
            className='image-create-button'
            variant='outline'
            onClick={props.onRetry}
          >
            <RotateCw aria-hidden='true' />
            {t('Retry')}
          </Button>
        </div>
      )}
      <div className='image-create-preview-tools'>
        {width > 0 && height > 0 && (
          <span className='image-create-stage-label'>
            <ImageIcon aria-hidden='true' />
            {width} × {height}
          </span>
        )}
        <div
          className='image-create-view-controls'
          role='group'
          aria-label={t('Image details')}
        >
          <button
            type='button'
            aria-pressed={view === 'fit'}
            disabled={!decoded || failed}
            onClick={() => setView('fit')}
          >
            {t('Fit image')}
          </button>
          <button
            type='button'
            aria-label={t('Actual size')}
            title={t('Actual size')}
            aria-pressed={view === 'actual'}
            disabled={!decoded || failed}
            onClick={() => setView('actual')}
          >
            100%
          </button>
          {decoded && !failed && props.onOpen && (
            <button
              type='button'
              className='image-create-zoom'
              onClick={props.onOpen}
              aria-label={t('View image details')}
            >
              <Maximize2 aria-hidden='true' />
            </button>
          )}
        </div>
      </div>
    </>
  )
}

export function ImageCreationResults(props: ImageCreationResultsProps) {
  const { t, i18n } = useTranslation()
  const resultsRef = useRef<HTMLElement>(null)
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const currency = useSystemConfigStore((state) => state.config.currency)
  const quotaPerUnit =
    currency.quotaPerUnit > 0
      ? currency.quotaPerUnit
      : DEFAULT_CURRENCY_CONFIG.quotaPerUnit
  const locale = toIntlLocale(i18n.language)
  const generation = props.generation
  const [lightboxAsset, setLightboxAsset] = useState<number>()
  const [previewAttempt, setPreviewAttempt] = useState(0)
  const [presentedImage, setPresentedImage] = useState<{
    userId: number
    assetId: number
    source: string
  }>()
  const [revealedScope, setRevealedScope] = useState({
    userId,
    assets: new Set<number>(),
  })
  if (revealedScope.userId !== userId) {
    setRevealedScope({ userId, assets: new Set<number>() })
  }
  const mediaPaths = useMemo(
    () =>
      [
        ...props.history,
        ...[generation, props.previousGeneration].filter(
          (item): item is ImageGeneration => !!item
        ),
      ].flatMap((item) =>
        item.assets.flatMap((asset) => [
          asset.content_url,
          asset.thumbnail_url,
          asset.download_url,
        ])
      ),
    [generation, props.history, props.previousGeneration]
  )
  const mediaQuery = useStudioMediaUrls(mediaPaths)
  const [mediaSnapshot, setMediaSnapshot] = useState({
    userId,
    source: mediaQuery.data?.urls,
    urls: mediaQuery.data?.urls ?? {},
  })
  if (
    mediaSnapshot.userId !== userId ||
    mediaSnapshot.source !== mediaQuery.data?.urls
  ) {
    setMediaSnapshot({
      userId,
      source: mediaQuery.data?.urls,
      urls:
        mediaQuery.data?.urls ??
        (mediaSnapshot.userId === userId ? mediaSnapshot.urls : {}),
    })
  }
  const urls = mediaSnapshot.userId === userId ? mediaSnapshot.urls : undefined
  const assets =
    generation?.assets
      .filter((asset) => asset.state === 'ready')
      .sort(
        (left, right) => left.position - right.position || left.id - right.id
      ) ?? []
  const selectedAsset =
    assets.find((asset) => asset.id === props.selectedAssetId) ?? assets[0]
  const previousAsset = props.previousGeneration?.assets
    .filter((asset) => asset.state === 'ready')
    .sort(
      (left, right) => left.position - right.position || left.id - right.id
    )[0]
  const showingPrevious = !selectedAsset && !!previousAsset
  const displayAsset = selectedAsset ?? previousAsset
  const displayGeneration = showingPrevious
    ? props.previousGeneration
    : generation
  const source = signedStudioMediaUrl(displayAsset?.content_url, urls)
  const holdingPrevious =
    !!displayAsset &&
    !!presentedImage &&
    presentedImage.userId === userId &&
    presentedImage.assetId !== displayAsset.id
  const download = signedStudioMediaUrl(
    selectedAsset?.download_url || selectedAsset?.content_url,
    urls
  )
  const activeSubmission = !!props.activity && props.activity.phase !== 'idle'
  const pending = generation?.status === 'submitting' || activeSubmission
  useEffect(() => {
    if (!pending) return
    const section = resultsRef.current
    if (!section) return
    const updateVisibility = () => {
      section.dataset.pageHidden = String(document.hidden)
    }
    updateVisibility()
    document.addEventListener('visibilitychange', updateVisibility)
    return () => {
      document.removeEventListener('visibilitychange', updateVisibility)
      delete section.dataset.pageHidden
    }
  }, [pending])
  const unknown = generation?.status === 'unknown'
  const errorKey = generation ? getImageGenerationErrorKey(generation) : null
  const canRegenerate =
    !!generation &&
    ['succeeded', 'partial', 'failed'].includes(generation.status) &&
    !pending
  const currentNumber = props.history.findIndex(
    (item) => item.id === generation?.id
  )
  const hasFailure =
    generation?.status === 'failed' ||
    generation?.status === 'archive_failed' ||
    unknown
  const previewUnavailable = !!displayAsset && !source && !mediaQuery.isPending
  const lightboxOpen =
    !!displayAsset && lightboxAsset === displayAsset.id && !!source
  let stateTitle = t('Your next creation starts here')
  let stateDescription = t(
    'Describe an idea. Your results will appear here, ready for the next step.'
  )
  if (pending) {
    stateTitle = t('Creating your next image')
    stateDescription = t(
      'Waiting for the model. Your previous results are saved.'
    )
    if (props.activity?.phase === 'preparing') {
      stateDescription = t('Preparing reference images')
    }
    if (props.activity?.phase === 'uploading') {
      stateDescription = t('Uploading reference images')
      if (
        typeof props.activity.progress === 'number' &&
        Number.isFinite(props.activity.progress)
      ) {
        stateDescription += ` · ${Math.round(Math.max(0, Math.min(100, props.activity.progress)))}%`
      }
    }
    if (props.activity?.phase === 'validating') {
      stateDescription = t('Validating generation settings')
    }
    if (props.activity?.phase === 'submitting') {
      stateDescription = t('Your image request is being processed')
    }
  } else if (unknown) {
    stateTitle = t('imageStudio.status.unknown')
    stateDescription = t(
      'The generation result could not be confirmed. Please contact support before submitting again.'
    )
  } else if (hasFailure) {
    stateTitle = t('No available result from this creation')
    stateDescription = errorKey
      ? t(errorKey)
      : t('Your inputs are preserved. Adjust the settings and try again.')
  } else if (displayAsset) {
    stateTitle = t(
      previewUnavailable ? 'Preview unavailable' : 'Preparing preview'
    )
    stateDescription = ''
  } else if (generation) {
    stateTitle = t('No available result from this creation')
    stateDescription = errorKey ? t(errorKey) : ''
  }

  return (
    <section
      ref={resultsRef}
      className='image-create-results'
      aria-label={t('Current creation results')}
    >
      <div className='image-create-result-heading'>
        <div>
          <span
            className={cn(
              'image-create-status-dot',
              pending && 'is-pending',
              hasFailure && 'is-error'
            )}
            aria-hidden='true'
          />
          <strong title={generation?.prompt}>
            {generation?.prompt || t('Current creation results')}
          </strong>
          {currentNumber >= 0 && (
            <span className='image-create-version'>
              #{props.history.length - currentNumber}
            </span>
          )}
        </div>
        <span className='image-create-result-status' aria-live='polite'>
          {activeSubmission && t('Creating your next image')}
          {generation && !activeSubmission && (
            <>
              {t(`imageStudio.status.${generation.status}`)} ·{' '}
              {t('imageStudio.outputCount', {
                ready: generation.succeeded_count,
                total: generation.requested_count,
              })}
            </>
          )}
        </span>
      </div>
      <div
        className={cn('image-create-stage', showingPrevious && 'has-previous')}
      >
        <div className='image-create-stage-halo' aria-hidden='true' />
        {holdingPrevious && (
          <img
            className='image-create-transition-image'
            src={presentedImage.source}
            alt=''
            aria-hidden='true'
          />
        )}
        {displayAsset && displayGeneration && source && (
          <CreationImage
            key={`${userId}:${displayAsset.id}:${previewAttempt}`}
            asset={displayAsset}
            source={source}
            prompt={displayGeneration.prompt}
            revealed={revealedScope.assets}
            onOpen={() => setLightboxAsset(displayAsset.id)}
            onRetry={() => {
              setPreviewAttempt((value) => value + 1)
              void mediaQuery.refetch()
            }}
            onDecoded={(decodedSource) =>
              setPresentedImage({
                userId,
                assetId: displayAsset.id,
                source: decodedSource,
              })
            }
          />
        )}
        {(showingPrevious ||
          holdingPrevious ||
          (activeSubmission && !!source)) && (
          <span className='image-create-previous-label'>
            {t('Previous result')}
          </span>
        )}
        {(pending || hasFailure || !source) && (
          <div
            className={cn(
              'image-create-empty-result',
              pending && source && 'image-create-task-card',
              hasFailure && source && !pending && 'image-create-task-overlay'
            )}
            role='status'
            aria-live='polite'
          >
            {pending ? (
              <span className='image-create-orbit' aria-hidden='true'>
                <Sparkles aria-hidden='true' />
              </span>
            ) : (
              <span className='image-create-state-icon'>
                {hasFailure ? (
                  <CircleAlert aria-hidden='true' />
                ) : (
                  <ImageIcon aria-hidden='true' />
                )}
              </span>
            )}
            <strong>{stateTitle}</strong>
            {stateDescription && <span>{stateDescription}</span>}
            {previewUnavailable && !pending && !hasFailure && (
              <Button
                className='image-create-button'
                variant='outline'
                onClick={() => void mediaQuery.refetch()}
              >
                {t('Retry')}
              </Button>
            )}
            {!generation && !pending && props.onExploreSamples && (
              <Button
                className='image-create-button'
                variant='outline'
                onClick={props.onExploreSamples}
              >
                <Lightbulb aria-hidden='true' />
                {t('Find inspiration')}
              </Button>
            )}
          </div>
        )}
      </div>
      {assets.length > 1 && (
        <div
          className='image-create-asset-strip'
          aria-label={t('Images in this batch: {{count}}', {
            count: assets.length,
          })}
        >
          {assets.map((asset, index) => {
            const thumbnail = signedStudioMediaUrl(
              asset.thumbnail_url || asset.content_url,
              urls
            )
            return (
              <button
                key={asset.id}
                type='button'
                aria-pressed={selectedAsset?.id === asset.id}
                aria-label={t('Select image {{number}}', { number: index + 1 })}
                onClick={() => props.onSelectAsset(asset.id)}
              >
                {thumbnail ? (
                  <img src={thumbnail} alt='' loading='lazy' decoding='async' />
                ) : (
                  <ImageIcon aria-hidden='true' />
                )}
                <span>
                  {index + 1} / {assets.length}
                </span>
              </button>
            )
          })}
        </div>
      )}
      {generation && (
        <div className='image-create-result-actions'>
          {selectedAsset && (
            <Button
              className='image-create-button'
              variant='outline'
              disabled={props.pendingEdit || pending || holdingPrevious}
              onClick={() => props.onEditAsset(selectedAsset, generation)}
            >
              <ImagePlus aria-hidden='true' />
              {t('Use this image to edit')}
            </Button>
          )}
          {canRegenerate && (
            <Button
              className='image-create-button image-create-quiet'
              variant='ghost'
              disabled={props.pendingEdit}
              onClick={() => props.onRegenerate(generation)}
            >
              <RotateCw aria-hidden='true' />
              {generation.status === 'failed'
                ? t('Retry generation')
                : t('Generate again')}
            </Button>
          )}
          {(generation.status === 'submitting' || unknown) && (
            <Button
              className='image-create-button image-create-quiet'
              variant='ghost'
              disabled={props.checkingStatus}
              onClick={() => props.onCheckStatus(generation)}
            >
              <RotateCw aria-hidden='true' />
              {t('Check status')}
            </Button>
          )}
          {download && (
            <a
              className='image-create-button image-create-quiet image-create-download'
              href={download}
              download
              aria-label={t('imageStudio.download')}
            >
              <Download aria-hidden='true' />
              <span className='sr-only'>{t('imageStudio.download')}</span>
            </a>
          )}
        </div>
      )}
      {generation && !pending && (
        <div
          className={cn('image-create-task-feedback', !!errorKey && 'is-error')}
          role='status'
        >
          {errorKey ? (
            <CircleAlert aria-hidden='true' />
          ) : (
            <CircleCheck aria-hidden='true' />
          )}
          <span>
            {errorKey
              ? t(errorKey)
              : t('Your results stay here. Keep exploring and creating.')}
          </span>
        </div>
      )}
      {props.history.length > 0 && (
        <>
          <div className='image-create-history-heading'>
            <span>{t('This creation session')}</span>
            <span>
              {t('Creations: {{count}}', { count: props.history.length })}
            </span>
          </div>
          <div
            className='image-create-history'
            aria-label={t('This creation session')}
          >
            {props.history.map((item, index) => {
              const cover = item.assets
                .filter((asset) => asset.state === 'ready')
                .sort(
                  (left, right) =>
                    left.position - right.position || left.id - right.id
                )[0]
              const thumbnail = signedStudioMediaUrl(
                cover?.thumbnail_url || cover?.content_url,
                urls
              )
              return (
                <button
                  key={item.id}
                  type='button'
                  className='image-create-history-item'
                  aria-pressed={generation?.id === item.id}
                  aria-label={t('View creation {{number}}: {{status}}', {
                    number: props.history.length - index,
                    status: t(`imageStudio.status.${item.status}`),
                  })}
                  onClick={() => props.onSelectGeneration(item.id)}
                >
                  {thumbnail ? (
                    <img
                      src={thumbnail}
                      alt=''
                      loading='lazy'
                      decoding='async'
                    />
                  ) : (
                    <span className='image-create-history-placeholder'>
                      <ImageIcon aria-hidden='true' />
                    </span>
                  )}
                  <span>
                    <span>#{props.history.length - index}</span>
                    <span>{t(`imageStudio.status.${item.status}`)}</span>
                  </span>
                </button>
              )
            })}
          </div>
        </>
      )}
      {generation && (
        <details className='image-create-task-details'>
          <summary>
            {t('Creation details')}
            <ChevronDown aria-hidden='true' />
          </summary>
          <dl>
            <div>
              <dt>{t('Model')}</dt>
              <dd>{generation.model}</dd>
            </div>
            <div>
              <dt>{t('Requested images')}</dt>
              <dd>{generation.requested_count}</dd>
            </div>
            <div>
              <dt>{t('Quota used')}</dt>
              <dd>
                {formatBillingCurrencyFromUSD(
                  generation.final_quota / quotaPerUnit,
                  { digitsLarge: 2, digitsSmall: 4, abbreviate: false }
                )}
              </dd>
            </div>
            <div>
              <dt>{t('Created at')}</dt>
              <dd>
                {new Date(generation.created_at * 1000).toLocaleString(locale)}
              </dd>
            </div>
            {generation.request_id && (
              <div>
                <dt>{t('Request ID')}</dt>
                <dd>{generation.request_id}</dd>
              </div>
            )}
            {Object.entries(generation.parameters).map(([key, value]) => (
              <div key={key}>
                <dt>{key}</dt>
                <dd>{String(value)}</dd>
              </div>
            ))}
          </dl>
          <p className='image-create-task-prompt'>{generation.prompt}</p>
        </details>
      )}
      <Dialog
        open={lightboxOpen}
        onOpenChange={(open) => {
          if (!open) setLightboxAsset(undefined)
        }}
      >
        <DialogContent className='image-create-lightbox'>
          <DialogHeader>
            <DialogTitle>{t('Image details')}</DialogTitle>
            <DialogDescription>{displayGeneration?.prompt}</DialogDescription>
          </DialogHeader>
          {source && displayGeneration && displayAsset && (
            <div className='image-creation-workbench image-create-detail-preview'>
              <div className='image-create-stage'>
                <CreationImage
                  key={`${userId}:${displayAsset.id}:${previewAttempt}:details`}
                  asset={displayAsset}
                  source={source}
                  prompt={displayGeneration.prompt}
                  revealed={revealedScope.assets}
                  onRetry={() => {
                    setPreviewAttempt((value) => value + 1)
                    void mediaQuery.refetch()
                  }}
                />
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </section>
  )
}
