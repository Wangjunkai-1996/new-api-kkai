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
import { SlidersHorizontal } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { useMediaQuery } from '@/hooks/use-media-query'

import { ImageComposer } from './components/image-composer'
import { ImageSampleGallery } from './components/image-sample-gallery'
import { ImageStudioNav } from './components/image-studio-nav'
import { useImageTokenGate } from './hooks/use-image-token-gate'
import { useImageSample } from './queries'
import type { ImageGeneration, ImageSample } from './types'

export function ImageCreatePage(props: { initialSampleId?: number }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const desktop = useMediaQuery('(min-width: 1180px)')
  const tokenGate = useImageTokenGate()
  const initialSampleQuery = useImageSample(
    props.initialSampleId,
    tokenGate.tokenId
  )
  const [selectedSample, setSelectedSample] = useState<ImageSample>()
  const [composerOpen, setComposerOpen] = useState(false)

  useEffect(() => {
    if (initialSampleQuery.data) setSelectedSample(initialSampleQuery.data)
  }, [initialSampleQuery.data])

  const selectSample = (sample: ImageSample): void => {
    setSelectedSample(sample)
    if (!desktop) setComposerOpen(true)
  }

  const submitted = (generation: ImageGeneration): void => {
    setComposerOpen(false)
    void navigate({
      to: '/image-studio/library',
      search: { generation: generation.id },
    })
  }

  const composer = (
    <ImageComposer
      sample={selectedSample}
      tokenGate={tokenGate}
      onSubmitted={submitted}
    />
  )

  return (
    <main
      id='content'
      className='flex size-full min-h-0 flex-col overflow-hidden'
    >
      <ImageStudioNav
        action={
          desktop ? null : (
            <Button size='sm' onClick={() => setComposerOpen(true)}>
              <SlidersHorizontal aria-hidden='true' />
              {t('imageStudio.composer')}
            </Button>
          )
        }
      />
      <div className='flex min-h-0 flex-1'>
        <ImageSampleGallery
          tokenId={tokenGate.tokenId}
          selectedSampleId={selectedSample?.id}
          onSelect={selectSample}
        />
        {desktop && (
          <aside
            className='bg-background flex w-[clamp(390px,30vw,440px)] shrink-0 flex-col border-l'
            aria-label={t('imageStudio.composer')}
          >
            <div className='shrink-0 border-b px-5 py-3'>
              <h2 className='text-sm font-semibold'>
                {t('imageStudio.composer')}
              </h2>
            </div>
            {composer}
          </aside>
        )}
      </div>
      {!desktop && (
        <Sheet open={composerOpen} onOpenChange={setComposerOpen}>
          <SheetContent className='w-full sm:max-w-[440px]'>
            <SheetHeader className='border-b'>
              <SheetTitle>{t('imageStudio.composer')}</SheetTitle>
              <SheetDescription className='sr-only'>
                {t('imageStudio.composerDescription')}
              </SheetDescription>
            </SheetHeader>
            {composer}
          </SheetContent>
        </Sheet>
      )}
    </main>
  )
}
