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
import { FolderOpen, LoaderCircle, RotateCw } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'

import { ImageGenerationCard } from './components/image-generation-card'
import { ImageStudioNav } from './components/image-studio-nav'
import { useDeleteImageGeneration, useImageGenerations } from './queries'
import type { ImageGeneration, ImageGenerationStatus } from './types'

export function ImageLibraryPage(props: {
  targetGenerationId?: number
  onClearTarget?: () => void
}) {
  const { t } = useTranslation()
  const [status, setStatus] = useState<ImageGenerationStatus | undefined>()
  const query = useImageGenerations(status ? { status } : {})
  const deleteMutation = useDeleteImageGeneration()
  const [deleteTarget, setDeleteTarget] = useState<ImageGeneration | null>(null)
  const targetRef = useRef<HTMLDivElement>(null)
  const scrolledRef = useRef<number | undefined>(undefined)
  const generations = useMemo(
    () => query.data?.pages.flatMap((page) => page.items) ?? [],
    [query.data]
  )
  const target = generations.find(
    (generation) => generation.id === props.targetGenerationId
  )

  useEffect(() => {
    if (!target || scrolledRef.current === target.id || !targetRef.current) {
      return
    }
    scrolledRef.current = target.id
    targetRef.current.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
  }, [target])

  const confirmDelete = async (): Promise<void> => {
    if (!deleteTarget) return
    try {
      await deleteMutation.mutateAsync(deleteTarget.id)
      if (deleteTarget.id === props.targetGenerationId) props.onClearTarget?.()
      toast.success(t('imageStudio.deleted'))
      setDeleteTarget(null)
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('imageStudio.deleteFailed')
      )
    }
  }

  return (
    <main
      id='content'
      className='flex size-full min-h-0 flex-col overflow-hidden'
    >
      <ImageStudioNav
        action={
          <Button
            size='icon-sm'
            variant='ghost'
            onClick={() => void query.refetch()}
            aria-label={t('Refresh')}
          >
            <RotateCw
              className={query.isFetching ? 'animate-spin' : undefined}
              aria-hidden='true'
            />
          </Button>
        }
      />
      <div className='min-h-0 flex-1 overflow-y-auto p-3 sm:p-4'>
        <div className='mb-4 flex justify-end'>
          <NativeSelect
            className='w-44'
            value={status ?? ''}
            aria-label={t('imageStudio.filterStatus')}
            onChange={(event) => {
              const next = event.target.value as ImageGenerationStatus | ''
              setStatus(next || undefined)
            }}
          >
            <NativeSelectOption value=''>
              {t('imageStudio.allStatuses')}
            </NativeSelectOption>
            {[
              'succeeded',
              'partial',
              'failed',
              'archive_failed',
              'unknown',
            ].map((value) => (
              <NativeSelectOption key={value} value={value}>
                {t(`imageStudio.status.${value}`)}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </div>
        {query.isLoading && (
          <div className='grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4'>
            {Array.from({ length: 8 }, (_, index) => (
              <Skeleton
                key={`image-library-skeleton-${index}`}
                className='aspect-[4/5] rounded-xl'
              />
            ))}
          </div>
        )}
        {query.isError && (
          <Empty className='min-h-64'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <FolderOpen aria-hidden='true' />
              </EmptyMedia>
              <EmptyTitle>{t('imageStudio.libraryFailed')}</EmptyTitle>
              <EmptyDescription>
                {t('imageStudio.libraryFailedDescription')}
              </EmptyDescription>
            </EmptyHeader>
            <Button variant='outline' onClick={() => void query.refetch()}>
              {t('Retry')}
            </Button>
          </Empty>
        )}
        {!query.isLoading && !query.isError && generations.length === 0 && (
          <Empty className='min-h-64'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <FolderOpen aria-hidden='true' />
              </EmptyMedia>
              <EmptyTitle>{t('imageStudio.libraryEmpty')}</EmptyTitle>
              <EmptyDescription>
                {t('imageStudio.libraryEmptyDescription')}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        {generations.length > 0 && (
          <div className='grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4'>
            {generations.map((generation) => (
              <div
                key={generation.id}
                ref={generation.id === target?.id ? targetRef : undefined}
              >
                <ImageGenerationCard
                  generation={generation}
                  highlighted={generation.id === target?.id}
                  onDelete={setDeleteTarget}
                />
              </div>
            ))}
          </div>
        )}
        {query.hasNextPage && (
          <div className='flex justify-center py-6'>
            <Button
              variant='outline'
              disabled={query.isFetchingNextPage}
              onClick={() => void query.fetchNextPage()}
            >
              {query.isFetchingNextPage && (
                <LoaderCircle className='animate-spin' aria-hidden='true' />
              )}
              {t('imageStudio.loadMore')}
            </Button>
          </div>
        )}
      </div>
      <AlertDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <FolderOpen aria-hidden='true' />
            </AlertDialogMedia>
            <AlertDialogTitle>{t('imageStudio.deleteTitle')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('imageStudio.deleteDescription')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={deleteMutation.isPending}
              onClick={(event) => {
                event.preventDefault()
                void confirmDelete()
              }}
            >
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </main>
  )
}
