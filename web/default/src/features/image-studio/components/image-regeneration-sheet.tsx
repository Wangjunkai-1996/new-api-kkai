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
import { useQuery } from '@tanstack/react-query'
import { LoaderCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { privateUserQueryKey } from '@/lib/private-query-cache'
import { useAuthStore } from '@/stores/auth-store'

import { getImageGenerationInput } from '../api'
import { useImageTokenGate } from '../hooks/use-image-token-gate'
import type { ImageGeneration } from '../types'
import { ImageComposer } from './image-composer'

function ImageRegenerationForm(props: {
  generation: ImageGeneration
  onSubmitted: (generation: ImageGeneration) => void
}) {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const tokenGate = useImageTokenGate()
  const inputQuery = useQuery({
    queryKey: privateUserQueryKey(
      userId,
      'image-studio',
      'generation-input',
      props.generation.id
    ),
    queryFn: () => getImageGenerationInput(props.generation.id),
    enabled: userId > 0,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    meta: { errorToast: false },
  })

  if (inputQuery.isPending) {
    return (
      <div
        className='flex flex-1 items-center justify-center p-6'
        role='status'
      >
        <LoaderCircle
          className='mr-2 size-4 animate-spin motion-reduce:animate-none'
          aria-hidden='true'
        />
        {t('Loading generation settings...')}
      </div>
    )
  }

  if (inputQuery.isError || !inputQuery.data) {
    return (
      <div className='p-5'>
        <Alert variant='destructive'>
          <AlertDescription>
            <p>{t('Unable to load the original generation settings.')}</p>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => void inputQuery.refetch()}
            >
              {t('Retry')}
            </Button>
          </AlertDescription>
        </Alert>
      </div>
    )
  }

  return (
    <ImageComposer
      reuseGeneration={props.generation}
      reuseInput={inputQuery.data}
      persistDraft={false}
      tokenGate={tokenGate}
      onSubmitted={props.onSubmitted}
    />
  )
}

export function ImageRegenerationSheet(props: {
  generation: ImageGeneration | null
  onClose: () => void
  onSubmitted: (generation: ImageGeneration) => void
}) {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  return (
    <Sheet
      open={props.generation !== null}
      onOpenChange={(open) => !open && props.onClose()}
    >
      <SheetContent className='w-full gap-0 motion-reduce:translate-x-0 motion-reduce:transform-none motion-reduce:transition-none sm:max-w-[480px]'>
        <SheetHeader className='shrink-0 border-b pr-12'>
          <SheetTitle>{t('Generate again')}</SheetTitle>
          <SheetDescription>
            {t(
              'Each submission creates a new image task at the current price. Your original images are kept.'
            )}
          </SheetDescription>
        </SheetHeader>
        {props.generation && (
          <ImageRegenerationForm
            key={`${userId}:${props.generation.id}`}
            generation={props.generation}
            onSubmitted={props.onSubmitted}
          />
        )}
      </SheetContent>
    </Sheet>
  )
}
