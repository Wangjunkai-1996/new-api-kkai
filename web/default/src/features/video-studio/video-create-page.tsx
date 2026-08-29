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
import { AxiosError } from 'axios'
import {
  CircleAlert,
  LoaderCircle,
  RotateCw,
  SlidersHorizontal,
} from 'lucide-react'
import { type ReactNode, useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerHeader,
  DrawerTitle,
} from '@/components/ui/drawer'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { useMediaQuery } from '@/hooks/use-media-query'

import { VideoComposer } from './components/video-composer'
import { VideoSampleGallery } from './components/video-sample-gallery'
import { VideoStudioEntryGate } from './components/video-studio-entry-gate'
import { VideoStudioNav } from './components/video-studio-nav'
import { useVideoTokenGate } from './hooks/use-video-token-gate'
import { useVideoModels, useVideoSample } from './queries'
import type {
  VideoSample,
  VideoStudioApiError,
  VideoSubmissionReceipt,
} from './types'
import { getVideoTokenErrorKind } from './video-token-access'

type VideoCreatePageProps = {
  initialSampleId?: number
}

const normalizeVideoTaskId = (...candidates: unknown[]): string | undefined => {
  for (const candidate of candidates) {
    if (
      typeof candidate === 'string' &&
      candidate.length <= 128 &&
      /^task_[0-9A-Za-z_-]+$/.test(candidate)
    ) {
      return candidate
    }
  }
  return undefined
}

export function VideoCreatePage(props: VideoCreatePageProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const desktop = useMediaQuery('(min-width: 1180px)')
  const mobile = useMediaQuery('(max-width: 767px)')
  const videoTokenGate = useVideoTokenGate()
  const modelsQuery = useVideoModels(videoTokenGate.tokenId)
  const initialSampleQuery = useVideoSample(
    props.initialSampleId,
    videoTokenGate.tokenId
  )
  const composerButtonRef = useRef<HTMLButtonElement>(null)
  const [selectedSampleOverride, setSelectedSample] = useState<
    VideoSample | undefined
  >()
  const selectedSample = selectedSampleOverride ?? initialSampleQuery.data
  const [composerOpen, setComposerOpen] = useState(false)
  const blockAndRecheckVideoToken = videoTokenGate.blockAndRecheck
  const markVideoTokenHealthy = videoTokenGate.markTokenHealthy

  useEffect(() => {
    if (!modelsQuery.isError) return
    const responseError =
      modelsQuery.error instanceof AxiosError
        ? (modelsQuery.error.response?.data as VideoStudioApiError | undefined)
        : undefined
    blockAndRecheckVideoToken(getVideoTokenErrorKind(responseError?.code))
  }, [blockAndRecheckVideoToken, modelsQuery.error, modelsQuery.isError])

  useEffect(() => {
    if (!modelsQuery.isSuccess || !videoTokenGate.tokenId) return
    markVideoTokenHealthy(videoTokenGate.tokenId)
  }, [markVideoTokenHealthy, modelsQuery.isSuccess, videoTokenGate.tokenId])

  useEffect(() => {
    if (!initialSampleQuery.isError) return
    const responseError =
      initialSampleQuery.error instanceof AxiosError
        ? (initialSampleQuery.error.response?.data as
            | VideoStudioApiError
            | undefined)
        : undefined
    blockAndRecheckVideoToken(getVideoTokenErrorKind(responseError?.code))
  }, [
    blockAndRecheckVideoToken,
    initialSampleQuery.error,
    initialSampleQuery.isError,
  ])

  const trySample = (sample: VideoSample) => {
    setSelectedSample(sample)
    if (!desktop) setComposerOpen(true)
  }

  const handleComposerOpenChange = (open: boolean) => {
    setComposerOpen(open)
    if (!open) {
      window.requestAnimationFrame(() => composerButtonRef.current?.focus())
    }
  }

  const navigateToLibrary = useCallback(
    (...taskIdCandidates: unknown[]) => {
      const task = normalizeVideoTaskId(...taskIdCandidates)
      void navigate({
        to: '/video-studio/library',
        search: task ? { task } : {},
      })
    },
    [navigate]
  )

  const handleSubmitted = useCallback(
    (receipt: VideoSubmissionReceipt) => {
      navigateToLibrary(receipt.task_id, receipt.id)
    },
    [navigateToLibrary]
  )

  const models = modelsQuery.data ?? []
  let entryState: ReactNode = null
  if (!videoTokenGate.tokenId) {
    entryState = <VideoStudioEntryGate gate={videoTokenGate} />
  } else if (modelsQuery.isError && !modelsQuery.data) {
    entryState = (
      <Empty className='min-h-80 rounded-none' role='alert'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <CircleAlert className='text-destructive' aria-hidden='true' />
          </EmptyMedia>
          <EmptyTitle>{t('videoStudio.workspace.prepareFailed')}</EmptyTitle>
          <EmptyDescription>
            {t('videoStudio.workspace.prepareFailedDescription')}
          </EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button
            type='button'
            size='sm'
            disabled={modelsQuery.isFetching}
            onClick={() => void modelsQuery.refetch()}
          >
            <RotateCw data-icon='inline-start' aria-hidden='true' />
            {t('videoStudio.retry')}
          </Button>
        </EmptyContent>
      </Empty>
    )
  } else if (
    !modelsQuery.data ||
    (models.length === 0 && modelsQuery.isFetching)
  ) {
    entryState = (
      <Empty className='min-h-80 rounded-none' role='status'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <LoaderCircle
              className='animate-spin motion-reduce:animate-none'
              aria-hidden='true'
            />
          </EmptyMedia>
          <EmptyTitle>{t('videoStudio.workspace.preparing')}</EmptyTitle>
          <EmptyDescription>
            {t('videoStudio.workspace.preparingDescription')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else if (models.length === 0) {
    entryState = (
      <Empty className='min-h-80 rounded-none' role='alert'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <CircleAlert className='text-destructive' aria-hidden='true' />
          </EmptyMedia>
          <EmptyTitle>{t('videoStudio.noModels')}</EmptyTitle>
        </EmptyHeader>
        <EmptyContent>
          <Button
            type='button'
            size='sm'
            disabled={modelsQuery.isFetching}
            onClick={() => void modelsQuery.refetch()}
          >
            <RotateCw data-icon='inline-start' aria-hidden='true' />
            {t('videoStudio.retry')}
          </Button>
        </EmptyContent>
      </Empty>
    )
  }

  if (entryState) {
    return (
      <main
        id='content'
        className='flex size-full min-h-0 flex-col overflow-hidden'
      >
        <VideoStudioNav />
        {entryState}
      </main>
    )
  }

  const composer = (
    <VideoComposer
      models={models}
      sample={selectedSample}
      videoTokenGate={videoTokenGate}
      onSubmitted={handleSubmitted}
      onSubmissionUnknown={navigateToLibrary}
    />
  )
  const composerAction = desktop ? null : (
    <Button
      ref={composerButtonRef}
      size='sm'
      aria-label={t('videoStudio.create')}
      onClick={() => setComposerOpen(true)}
    >
      <SlidersHorizontal aria-hidden='true' />
      <span className='hidden sm:inline'>{t('videoStudio.create')}</span>
    </Button>
  )

  return (
    <main
      id='content'
      className='flex size-full min-h-0 flex-col overflow-hidden'
    >
      <VideoStudioNav action={composerAction} />
      <div className='flex min-h-0 flex-1'>
        <VideoSampleGallery
          models={models}
          tokenId={videoTokenGate.tokenId}
          selectedSampleId={selectedSample?.id}
          onTokenError={blockAndRecheckVideoToken}
          onTrySample={trySample}
        />
        {desktop && (
          <aside
            className='bg-background flex w-[clamp(384px,29vw,420px)] shrink-0 flex-col border-l'
            aria-label={t('videoStudio.composer')}
          >
            <div className='shrink-0 border-b px-5 py-3'>
              <h2 className='text-sm font-semibold'>
                {t('videoStudio.composer')}
              </h2>
            </div>
            {composer}
          </aside>
        )}
      </div>

      {!desktop && mobile && (
        <Drawer open={composerOpen} onOpenChange={handleComposerOpenChange}>
          <DrawerContent className='h-[92dvh] max-h-[92dvh]'>
            <DrawerHeader className='border-b text-left'>
              <DrawerTitle>{t('videoStudio.composer')}</DrawerTitle>
              <DrawerDescription className='sr-only'>
                {t('videoStudio.composerDescription')}
              </DrawerDescription>
            </DrawerHeader>
            {composer}
          </DrawerContent>
        </Drawer>
      )}

      {!desktop && !mobile && (
        <Sheet open={composerOpen} onOpenChange={handleComposerOpenChange}>
          <SheetContent className='w-full sm:max-w-[420px]'>
            <SheetHeader className='border-b'>
              <SheetTitle>{t('videoStudio.composer')}</SheetTitle>
              <SheetDescription className='sr-only'>
                {t('videoStudio.composerDescription')}
              </SheetDescription>
            </SheetHeader>
            {composer}
          </SheetContent>
        </Sheet>
      )}
    </main>
  )
}
