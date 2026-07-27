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
import { SlidersHorizontal } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
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
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { useMediaQuery } from '@/hooks/use-media-query'

import { VideoComposer } from './components/video-composer'
import { VideoSampleGallery } from './components/video-sample-gallery'
import { VideoStudioNav } from './components/video-studio-nav'
import { useVideoSample } from './queries'
import type { VideoSample } from './types'

type VideoCreatePageProps = {
  initialSampleId?: number
}

export function VideoCreatePage(props: VideoCreatePageProps) {
  const { t } = useTranslation()
  const desktop = useMediaQuery('(min-width: 1180px)')
  const mobile = useMediaQuery('(max-width: 767px)')
  const initialSampleQuery = useVideoSample(props.initialSampleId)
  const composerButtonRef = useRef<HTMLButtonElement>(null)
  const [selectedSample, setSelectedSample] = useState<
    VideoSample | undefined
  >()
  const [composerOpen, setComposerOpen] = useState(false)

  useEffect(() => {
    if (initialSampleQuery.data) setSelectedSample(initialSampleQuery.data)
  }, [initialSampleQuery.data])

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

  const composer = <VideoComposer sample={selectedSample} />
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
          selectedSampleId={selectedSample?.id}
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
