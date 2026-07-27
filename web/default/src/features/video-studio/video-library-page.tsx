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
import { FolderOpen, LoaderCircle, RotateCw, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
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
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'

import { VideoGenerationCard } from './components/video-generation-card'
import { VideoStudioNav } from './components/video-studio-nav'
import { useDeleteVideoGeneration, useVideoGenerations } from './queries'
import type { VideoGeneration } from './types'

export function VideoLibraryPage() {
  const { t } = useTranslation()
  const generationsQuery = useVideoGenerations({ status: 'ready' })
  const deleteMutation = useDeleteVideoGeneration()
  const [deleteTarget, setDeleteTarget] = useState<VideoGeneration | null>(null)
  const generations = useMemo(
    () => generationsQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [generationsQuery.data]
  )

  const confirmDelete = async () => {
    if (!deleteTarget) return
    try {
      await deleteMutation.mutateAsync(deleteTarget.id)
      toast.success(t('videoStudio.deleted'))
      setDeleteTarget(null)
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('videoStudio.deleteFailed')
      )
    }
  }

  return (
    <main
      id='content'
      className='flex size-full min-h-0 flex-col overflow-hidden'
    >
      <VideoStudioNav
        action={
          <Button
            size='icon-sm'
            variant='ghost'
            onClick={() => generationsQuery.refetch()}
            aria-label={t('videoStudio.refresh')}
          >
            <RotateCw aria-hidden='true' />
          </Button>
        }
      />
      <div className='min-h-0 flex-1 overflow-y-auto p-3 sm:p-4'>
        {generationsQuery.isLoading && generations.length === 0 && (
          <div className='grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4'>
            {Array.from({ length: 8 }, (_, index) => (
              <Skeleton
                key={`library-skeleton-${index}`}
                className='aspect-[4/5] w-full rounded-lg'
              />
            ))}
          </div>
        )}

        {generationsQuery.isError && generations.length === 0 && (
          <Empty className='min-h-72 border'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <FolderOpen aria-hidden='true' />
              </EmptyMedia>
              <EmptyTitle>{t('videoStudio.libraryFailed')}</EmptyTitle>
              <EmptyDescription>
                {t('videoStudio.libraryFailedDescription')}
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button
                variant='outline'
                onClick={() => generationsQuery.refetch()}
              >
                <RotateCw aria-hidden='true' />
                {t('videoStudio.retry')}
              </Button>
            </EmptyContent>
          </Empty>
        )}

        {!generationsQuery.isLoading &&
          !generationsQuery.isError &&
          generations.length === 0 && (
            <Empty className='min-h-72 border'>
              <EmptyHeader>
                <EmptyMedia variant='icon'>
                  <FolderOpen aria-hidden='true' />
                </EmptyMedia>
                <EmptyTitle>{t('videoStudio.libraryEmpty')}</EmptyTitle>
                <EmptyDescription>
                  {t('videoStudio.libraryEmptyDescription')}
                </EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button render={<a href='/video-studio/create' />}>
                  {t('videoStudio.create')}
                </Button>
              </EmptyContent>
            </Empty>
          )}

        {generations.length > 0 && (
          <div className='grid grid-cols-1 items-start gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4'>
            {generations.map((generation) => (
              <VideoGenerationCard
                key={generation.id}
                generation={generation}
                onDelete={setDeleteTarget}
              />
            ))}
          </div>
        )}

        {generationsQuery.hasNextPage && (
          <div className='flex justify-center pt-5'>
            <Button
              variant='outline'
              onClick={() => generationsQuery.fetchNextPage()}
              disabled={generationsQuery.isFetchingNextPage}
            >
              {generationsQuery.isFetchingNextPage && (
                <LoaderCircle
                  className='animate-spin motion-reduce:animate-none'
                  aria-hidden='true'
                />
              )}
              {t('videoStudio.loadMore')}
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
              <Trash2 aria-hidden='true' />
            </AlertDialogMedia>
            <AlertDialogTitle>{t('videoStudio.deleteTitle')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('videoStudio.deleteDescription')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('videoStudio.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={deleteMutation.isPending}
              onClick={() => void confirmDelete()}
            >
              {deleteMutation.isPending && (
                <LoaderCircle
                  className='animate-spin motion-reduce:animate-none'
                  aria-hidden='true'
                />
              )}
              {t('videoStudio.delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </main>
  )
}
