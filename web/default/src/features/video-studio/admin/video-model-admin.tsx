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
import { zodResolver } from '@hookform/resolvers/zod'
import { isAxiosError } from 'axios'
import { LoaderCircle, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
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
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

import {
  useAdminVideoModelCandidates,
  useAdminVideoModels,
  useDeleteAdminVideoModel,
  useSaveAdminVideoModel,
} from '../queries'
import {
  canToggleVideoModelEnabled,
  createVideoModelProfileFormValues,
  filterVideoModelCandidates,
  getVideoModelCandidateLabel,
  getVideoModelPreset,
  parseVideoModelProfileForm,
  VIDEO_MODEL_DESCRIPTION_MAX_LENGTH,
  VIDEO_MODEL_DISPLAY_NAME_MAX_LENGTH,
  VIDEO_MODEL_PROVIDER_LABEL_MAX_LENGTH,
  VIDEO_MODEL_SORT_ORDER_MAX,
  VIDEO_MODEL_SORT_ORDER_MIN,
  videoModelProfileFormSchema,
  type VideoModelProfileFormValues,
} from '../schemas'
import type { VideoModelProfile, VideoStudioApiError } from '../types'
import { VideoAdminWorkspace } from './video-admin-workspace'
import { VideoModelSpecEditor } from './video-model-spec-editor'

export function VideoModelAdmin() {
  const { t } = useTranslation()
  const modelsQuery = useAdminVideoModels()
  const candidatesQuery = useAdminVideoModelCandidates()
  const saveMutation = useSaveAdminVideoModel()
  const deleteMutation = useDeleteAdminVideoModel()
  const [selected, setSelected] = useState<VideoModelProfile | null>(null)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const form = useForm<VideoModelProfileFormValues>({
    resolver: zodResolver(videoModelProfileFormSchema),
    defaultValues: createVideoModelProfileFormValues(),
  })
  const modelName = form.watch('model')
  const candidatePreset = getVideoModelPreset(modelName)
  const candidateQueriesReady =
    modelsQuery.isSuccess && candidatesQuery.isSuccess
  const candidateQueriesFailed = modelsQuery.isError || candidatesQuery.isError
  const candidateQueriesLoading =
    !candidateQueriesReady && !candidateQueriesFailed
  const candidates = useMemo(
    () =>
      candidateQueriesReady
        ? filterVideoModelCandidates(
            candidatesQuery.data ?? [],
            modelsQuery.data ?? []
          )
        : [],
    [candidateQueriesReady, candidatesQuery.data, modelsQuery.data]
  )
  const candidateAvailable =
    candidateQueriesReady && candidates.includes(modelName)
  const canSave = selected ? Boolean(candidatePreset) : candidateAvailable
  const selectedModelState = selected
    ? (modelsQuery.data?.find((model) => model.id === selected.id) ?? selected)
    : null
  const hasPublishedSample = selectedModelState?.has_published_sample === true
  const canToggleEnabled = canToggleVideoModelEnabled(selectedModelState)

  useEffect(() => {
    form.reset(createVideoModelProfileFormValues(selected ?? undefined))
  }, [form, selected])

  useEffect(() => {
    if (
      selected ||
      !candidateQueriesReady ||
      !modelName ||
      candidates.includes(modelName)
    ) {
      return
    }
    form.reset(createVideoModelProfileFormValues())
  }, [candidateQueriesReady, candidates, form, modelName, selected])

  const startCreate = () => {
    setSelected(null)
    form.reset(createVideoModelProfileFormValues())
  }

  const submit = form.handleSubmit(async (values) => {
    if (!selected && !candidateAvailable) {
      form.setError('model', {
        message: 'videoStudio.admin.modelPresetUnavailable',
      })
      return
    }
    try {
      const parsed = parseVideoModelProfileForm(values, selected ?? undefined)
      if (parsed.enabled && !hasPublishedSample) {
        form.setError('root', {
          message: t('videoStudio.admin.enableRequiresPublishedSample'),
        })
        return
      }
      const saved = await saveMutation.mutateAsync({
        id: selected?.id,
        values: parsed,
      })
      setSelected(saved)
      toast.success(t('videoStudio.admin.modelSaved'))
    } catch (error) {
      const responseError = isAxiosError<VideoStudioApiError>(error)
        ? error.response?.data
        : undefined
      const message =
        responseError?.code === 'video_model_needs_sample'
          ? t('videoStudio.admin.enableRequiresPublishedSample')
          : responseError?.message ||
            (error instanceof Error
              ? error.message
              : t('videoStudio.admin.saveFailed'))
      form.setError('root', { message })
    }
  })

  const confirmDelete = async () => {
    if (!selected) return
    try {
      await deleteMutation.mutateAsync(selected.id)
      setSelected(null)
      setDeleteOpen(false)
      toast.success(t('videoStudio.admin.modelDeleted'))
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('videoStudio.admin.deleteFailed')
      )
    }
  }

  const list = (
    <div className='flex h-full min-h-0 flex-col'>
      <div className='flex items-center justify-between border-b px-3 py-2.5'>
        <span className='text-sm font-semibold'>
          {t('videoStudio.admin.models')}
        </span>
        <Button
          size='icon-sm'
          variant='ghost'
          onClick={startCreate}
          aria-label={t('videoStudio.admin.addModel')}
        >
          <Plus aria-hidden='true' />
        </Button>
      </div>
      <div className='min-h-0 flex-1 overflow-y-auto p-2'>
        {modelsQuery.isLoading && (
          <div className='flex justify-center py-8' role='status'>
            <LoaderCircle
              className='text-muted-foreground size-5 animate-spin motion-reduce:animate-none'
              aria-hidden='true'
            />
          </div>
        )}
        {modelsQuery.data?.map((model) => (
          <button
            key={model.id}
            type='button'
            className={cn(
              'hover:bg-muted flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left transition-colors',
              selected?.id === model.id && 'bg-muted'
            )}
            onClick={() => setSelected(model)}
          >
            <span
              className={cn(
                'size-2 shrink-0 rounded-full',
                model.enabled ? 'bg-success' : 'bg-muted-foreground/40'
              )}
              aria-hidden='true'
            />
            <span className='min-w-0 flex-1'>
              <span className='block truncate text-sm font-medium'>
                {model.display_name}
              </span>
            </span>
          </button>
        ))}
      </div>
    </div>
  )

  const editor = (
    <Form {...form}>
      <form
        className='flex h-full min-h-0 flex-col'
        onSubmit={submit}
        noValidate
      >
        <div className='flex items-center justify-between border-b px-4 py-2.5'>
          <h3 className='text-sm font-semibold'>
            {selected
              ? t('videoStudio.admin.editModel')
              : t('videoStudio.admin.addModel')}
          </h3>
          <div className='flex gap-1'>
            {selected && (
              <Button
                type='button'
                size='icon-sm'
                variant='ghost'
                onClick={() => setDeleteOpen(true)}
                aria-label={t('videoStudio.delete')}
              >
                <Trash2 aria-hidden='true' />
              </Button>
            )}
            <Button
              type='submit'
              size='sm'
              disabled={saveMutation.isPending || !canSave}
            >
              {saveMutation.isPending && (
                <LoaderCircle
                  className='animate-spin motion-reduce:animate-none'
                  aria-hidden='true'
                />
              )}
              {t('videoStudio.admin.save')}
            </Button>
          </div>
        </div>

        <div className='min-h-0 flex-1 space-y-4 overflow-y-auto p-4'>
          <div className='grid gap-4 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='model'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('videoStudio.admin.model')}</FormLabel>
                  <FormControl>
                    {selected ? (
                      <Input
                        {...field}
                        value={getVideoModelCandidateLabel(field.value)}
                        readOnly
                      />
                    ) : (
                      <NativeSelect
                        className='w-full'
                        value={field.value}
                        disabled={!candidateQueriesReady}
                        onChange={(event) => {
                          const candidate = event.target.value
                          form.reset(
                            createVideoModelProfileFormValues(
                              undefined,
                              candidate
                            )
                          )
                        }}
                      >
                        <NativeSelectOption value=''>
                          {candidateQueriesLoading
                            ? t('videoStudio.admin.loadingModelCandidates')
                            : t('videoStudio.admin.selectModelCandidate')}
                        </NativeSelectOption>
                        {candidates.map((candidate) => (
                          <NativeSelectOption key={candidate} value={candidate}>
                            {getVideoModelCandidateLabel(candidate)}
                          </NativeSelectOption>
                        ))}
                      </NativeSelect>
                    )}
                  </FormControl>
                  <FormMessage />
                  {!selected && candidateQueriesFailed && (
                    <div className='flex items-center justify-between gap-3'>
                      <p className='text-destructive text-xs' role='alert'>
                        {t('videoStudio.admin.modelCandidatesFailed')}
                      </p>
                      <Button
                        type='button'
                        size='sm'
                        variant='outline'
                        disabled={
                          modelsQuery.isFetching || candidatesQuery.isFetching
                        }
                        onClick={() => {
                          void Promise.all([
                            modelsQuery.refetch(),
                            candidatesQuery.refetch(),
                          ])
                        }}
                      >
                        <RefreshCw
                          className={cn(
                            (modelsQuery.isFetching ||
                              candidatesQuery.isFetching) &&
                              'animate-spin motion-reduce:animate-none'
                          )}
                          aria-hidden='true'
                        />
                        {t('videoStudio.retry')}
                      </Button>
                    </div>
                  )}
                  {!selected &&
                    candidateQueriesReady &&
                    candidates.length === 0 && (
                      <p className='text-muted-foreground text-xs'>
                        {t('videoStudio.admin.noModelCandidates')}
                      </p>
                    )}
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='display_name'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('videoStudio.admin.displayName')}</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      disabled={!canSave}
                      maxLength={VIDEO_MODEL_DISPLAY_NAME_MAX_LENGTH}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='provider_label'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('videoStudio.admin.provider')}</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      disabled={!canSave}
                      maxLength={VIDEO_MODEL_PROVIDER_LABEL_MAX_LENGTH}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='sort_order'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('videoStudio.admin.sort')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      {...field}
                      disabled={!canSave}
                      min={VIDEO_MODEL_SORT_ORDER_MIN}
                      max={VIDEO_MODEL_SORT_ORDER_MAX}
                      onChange={(event) =>
                        field.onChange(event.target.valueAsNumber)
                      }
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
          <FormField
            control={form.control}
            name='description'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('videoStudio.admin.description')}</FormLabel>
                <FormControl>
                  <Textarea
                    {...field}
                    disabled={!canSave}
                    maxLength={VIDEO_MODEL_DESCRIPTION_MAX_LENGTH}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <VideoModelSpecEditor />
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between gap-3 border-t pt-4'>
                <div className='space-y-1'>
                  <FormLabel>{t('videoStudio.admin.enabled')}</FormLabel>
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      selected
                        ? 'videoStudio.admin.enableRequiresPublishedSample'
                        : 'videoStudio.admin.enableAfterCreate'
                    )}
                  </p>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    disabled={!canToggleEnabled && !field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />
          {form.formState.errors.root?.message && (
            <p className='text-destructive text-xs' role='alert'>
              {form.formState.errors.root.message}
            </p>
          )}
        </div>
      </form>
    </Form>
  )

  return (
    <>
      <VideoAdminWorkspace list={list} editor={editor} />
      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <Trash2 aria-hidden='true' />
            </AlertDialogMedia>
            <AlertDialogTitle>
              {t('videoStudio.admin.deleteModel')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t('videoStudio.admin.deleteModelDescription')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('videoStudio.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={deleteMutation.isPending}
              onClick={() => void confirmDelete()}
            >
              {t('videoStudio.delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
