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
import { useQueryClient } from '@tanstack/react-query'
import { ChevronDown, Lightbulb, LoaderCircle } from 'lucide-react'
import { useReducedMotion } from 'motion/react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/auth-store'

import '@/styles/image-create.css'

import {
  ImageComposer,
  type ImageComposerActivity,
  type ImageComposerLoadRequest,
} from './components/image-composer'
import { ImageCreationResults } from './components/image-creation-results'
import { ImageSampleGallery } from './components/image-sample-gallery'
import { ImageStudioNav } from './components/image-studio-nav'
import { useImageTokenGate } from './hooks/use-image-token-gate'
import {
  getImageOutputReference,
  ImageOutputReferenceError,
} from './image-output-reference'
import {
  imageGenerationInputQueryOptions,
  imageGenerationQueryOptions,
  useImageSample,
  useImageSessionGenerations,
} from './queries'
import type { ImageAsset, ImageGeneration, ImageSample } from './types'

export function ImageCreatePage(props: { initialSampleId?: number }) {
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  return <ImageCreationWorkbench key={userId} {...props} userId={userId} />
}

function ImageCreationWorkbench(props: {
  initialSampleId?: number
  userId: number
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const reduceMotion = useReducedMotion()
  const tokenGate = useImageTokenGate()
  const sampleQuery = useImageSample(props.initialSampleId, tokenGate.tokenId)
  const [selectedSampleId, setSelectedSampleId] = useState<number>()
  const [loadRequest, setLoadRequest] = useState<ImageComposerLoadRequest>()
  const [sessionIds, setSessionIds] = useState<number[]>([])
  const [selectedId, setSelectedId] = useState<number>()
  const [selectedAssetId, setSelectedAssetId] = useState<number>()
  const [activity, setActivity] = useState<ImageComposerActivity>({
    phase: 'idle',
  })
  const [pendingLoad, setPendingLoad] = useState(false)
  const [loadError, setLoadError] = useState<string>()
  const [samplesOpen, setSamplesOpen] = useState(true)
  const revision = useRef(0)
  const editorRevision = useRef(0)
  const lastInitialSampleId = useRef<number | undefined>(undefined)
  const alive = useRef(true)
  const editController = useRef<AbortController | null>(null)
  const settingsRef = useRef<HTMLDivElement>(null)
  const resultsRef = useRef<HTMLDivElement>(null)
  const samplesRef = useRef<HTMLElement>(null)
  const queries = useImageSessionGenerations(sessionIds)
  const history = queries.flatMap((query) => (query.data ? [query.data] : []))
  const generation = history.find((item) => item.id === selectedId)
  const selectedQuery = queries[sessionIds.indexOf(selectedId ?? 0)]
  const previousGeneration = history.find(
    (item) =>
      item.id !== selectedId &&
      (item.status === 'succeeded' || item.status === 'partial') &&
      item.assets.some((asset) => asset.state === 'ready')
  )

  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
      revision.current += 1
      editController.current?.abort()
    }
  }, [])

  const selectSample = useCallback((sample: ImageSample): void => {
    editController.current?.abort()
    const nextRevision = ++revision.current
    setPendingLoad(false)
    setLoadError(undefined)
    setLoadRequest({ kind: 'sample', revision: nextRevision, sample })
  }, [])

  useEffect(() => {
    const sample = sampleQuery.data
    if (sample && lastInitialSampleId.current !== sample.id) {
      lastInitialSampleId.current = sample.id
      selectSample(sample)
    }
  }, [sampleQuery.data, selectSample])

  const submitted = (result: ImageGeneration): void => {
    if (!alive.current) return
    queryClient.setQueryData(
      imageGenerationQueryOptions(props.userId, result.id).queryKey,
      result
    )
    setSessionIds((ids) => [result.id, ...ids.filter((id) => id !== result.id)])
    setSelectedId(result.id)
    setSelectedAssetId(undefined)
    setSamplesOpen(false)
    resultsRef.current?.scrollIntoView?.({
      block: 'nearest',
      behavior: reduceMotion ? 'instant' : 'smooth',
    })
  }

  const regenerate = async (source: ImageGeneration): Promise<void> => {
    editController.current?.abort()
    const loadRevision = ++revision.current
    setPendingLoad(true)
    setLoadError(undefined)
    try {
      const input = await queryClient.fetchQuery(
        imageGenerationInputQueryOptions(props.userId, source.id)
      )
      if (!alive.current || revision.current !== loadRevision) return
      setLoadRequest({
        kind: 'regenerate',
        revision: loadRevision,
        generation: source,
        input,
      })
    } catch {
      if (alive.current && revision.current === loadRevision) {
        setLoadError('Unable to load the original generation settings.')
      }
    } finally {
      if (alive.current && revision.current === loadRevision) {
        setPendingLoad(false)
      }
    }
  }

  const editAsset = async (
    asset: ImageAsset,
    source: ImageGeneration
  ): Promise<void> => {
    editController.current?.abort()
    const controller = new AbortController()
    editController.current = controller
    const loadRevision = ++revision.current
    const requestedEditorRevision = editorRevision.current
    setPendingLoad(true)
    setLoadError(undefined)
    try {
      const capability = tokenGate.capability
      if (!capability) {
        throw new ImageOutputReferenceError('image_reference_invalid')
      }
      const file = await getImageOutputReference(
        asset,
        {
          maxBytes: capability.max_reference_bytes,
          maxTotalBytes: capability.max_reference_total_bytes,
        },
        controller.signal
      )
      if (!alive.current || revision.current !== loadRevision) return
      setLoadRequest({
        kind: 'edit-output',
        revision: loadRevision,
        file,
        generation: source,
        assetId: asset.id,
        expectedEditorRevision: requestedEditorRevision,
      })
    } catch (error) {
      if (
        !alive.current ||
        controller.signal.aborted ||
        revision.current !== loadRevision
      ) {
        return
      }
      let message =
        'Unable to prepare this image for editing. Your draft has been kept.'
      if (error instanceof ImageOutputReferenceError) {
        switch (error.code) {
          case 'image_edit_source_timeout':
            message =
              'Loading the original image timed out. Your draft has been kept.'
            break
          case 'image_asset_too_large':
          case 'image_reference_invalid':
          case 'invalid_image_asset':
            message =
              'This image cannot be used as a reference. Check its format and size.'
            break
          case 'image_asset_access_denied':
          case 'image_studio_resource_not_found':
            message = 'This image is no longer available for editing.'
            break
        }
      }
      setLoadError(message)
    } finally {
      if (alive.current && revision.current === loadRevision) {
        setPendingLoad(false)
      }
    }
  }

  const loadSettled = useCallback(
    (loadRevision: number, accepted: boolean): void => {
      if (loadRequest?.revision !== loadRevision) return
      if (accepted && loadRequest.kind === 'sample') {
        setSelectedSampleId(loadRequest.sample.id)
      }
      setLoadRequest(undefined)
      if (accepted) {
        settingsRef.current?.scrollIntoView?.({ block: 'nearest' })
      }
    },
    [loadRequest]
  )

  return (
    <main
      id='content'
      className='image-creation-workbench flex size-full min-h-0 flex-col overflow-hidden'
    >
      <ImageStudioNav />
      <div className='image-create-main'>
        <div className='image-create-pagebar'>
          <div>
            <p className='image-create-eyebrow'>
              {t('A LITTLE IDEA. ENDLESS POSSIBILITIES.')}
            </p>
            <h2>{t('Give every idea a next step.')}</h2>
          </div>
          <Button
            type='button'
            variant='ghost'
            className='image-create-button image-create-jump'
            onClick={() => {
              setSamplesOpen(true)
              samplesRef.current?.scrollIntoView?.({
                behavior: reduceMotion ? 'instant' : 'smooth',
              })
            }}
          >
            {t('Find inspiration')}
          </Button>
        </div>
        {sampleQuery.isError && (
          <Alert variant='destructive'>
            <AlertDescription>
              {t('imageStudio.samplesFailed')}
              <Button
                variant='ghost'
                onClick={() => void sampleQuery.refetch()}
              >
                {t('Retry')}
              </Button>
            </AlertDescription>
          </Alert>
        )}
        <div className='image-create-layout'>
          <div ref={settingsRef} className='image-create-settings'>
            <Button
              type='button'
              variant='ghost'
              className='image-create-mobile-jump'
              onClick={() =>
                resultsRef.current?.scrollIntoView?.({
                  behavior: reduceMotion ? 'instant' : 'smooth',
                  block: 'start',
                })
              }
            >
              {t('View results')}
              <ChevronDown aria-hidden='true' />
            </Button>
            <ImageComposer
              presentation='workbench'
              tokenGate={tokenGate}
              loadRequest={loadRequest}
              onLoadRequestSettled={loadSettled}
              onEditorRevisionChange={(value) => {
                editorRevision.current = value
              }}
              onActivityChange={setActivity}
              onSubmitted={submitted}
            />
          </div>
          <div ref={resultsRef} className='image-create-output'>
            <Button
              type='button'
              variant='ghost'
              className='image-create-mobile-jump'
              onClick={() =>
                settingsRef.current?.scrollIntoView?.({
                  behavior: reduceMotion ? 'instant' : 'smooth',
                  block: 'start',
                })
              }
            >
              {t('Back to settings')}
              <ChevronDown aria-hidden='true' className='rotate-180' />
            </Button>
            {loadError && (
              <Alert variant='destructive' className='image-create-load-error'>
                <AlertDescription>{t(loadError)}</AlertDescription>
              </Alert>
            )}
            {pendingLoad && (
              <p className='image-create-loading' role='status'>
                <LoaderCircle
                  aria-hidden='true'
                  className='animate-spin motion-reduce:animate-none'
                />
                {t('Preparing your settings...')}
              </p>
            )}
            {selectedQuery?.isError && (
              <Alert variant='destructive'>
                <AlertDescription>
                  {t(
                    'Unable to refresh this generation. Your images have been kept.'
                  )}
                  <Button
                    variant='ghost'
                    onClick={() => void selectedQuery.refetch()}
                  >
                    {t('Retry')}
                  </Button>
                </AlertDescription>
              </Alert>
            )}
            <ImageCreationResults
              generation={generation}
              previousGeneration={previousGeneration}
              history={history}
              selectedAssetId={selectedAssetId}
              activity={activity}
              pendingEdit={pendingLoad}
              checkingStatus={selectedQuery?.isFetching}
              onSelectGeneration={(id) => {
                setSelectedId(id)
                setSelectedAssetId(undefined)
              }}
              onSelectAsset={setSelectedAssetId}
              onRegenerate={(source) => void regenerate(source)}
              onEditAsset={(asset, source) => void editAsset(asset, source)}
              onCheckStatus={() => void selectedQuery?.refetch()}
              onExploreSamples={() => {
                setSamplesOpen(true)
                samplesRef.current?.scrollIntoView?.({ block: 'nearest' })
              }}
            />
            <section ref={samplesRef} className='image-create-inspiration'>
              <Button
                type='button'
                variant='ghost'
                className='image-create-inspiration-toggle'
                aria-expanded={samplesOpen}
                aria-controls='image-create-samples'
                onClick={() => setSamplesOpen((open) => !open)}
              >
                <Lightbulb aria-hidden='true' />
                {t('imageStudio.inspiration')}
                <ChevronDown aria-hidden='true' />
              </Button>
              {samplesOpen && (
                <div id='image-create-samples'>
                  <ImageSampleGallery
                    tokenId={tokenGate.tokenId}
                    selectedSampleId={selectedSampleId}
                    onSelect={selectSample}
                  />
                </div>
              )}
            </section>
          </div>
        </div>
      </div>
    </main>
  )
}
