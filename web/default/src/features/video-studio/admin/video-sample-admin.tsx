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
import { LoaderCircle, Plus, Trash2 } from 'lucide-react'
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

import { VideoAssetUploader } from '../components/video-asset-uploader'
import { VideoParameterFields } from '../components/video-parameter-fields'
import {
  useAdminVideoModels,
  useAdminVideoSamples,
  useDeleteAdminVideoSample,
  useSaveAdminVideoSample,
} from '../queries'
import {
  buildVideoSampleProfileState,
  createVideoSampleFormValues,
  parseVideoSampleForm,
  videoSampleFormSchema,
  type VideoSampleFormValues,
} from '../schemas'
import type { VideoAsset, VideoGenerationMode, VideoSample } from '../types'
import {
  getSampleReferenceAssets,
  getSampleVideoAsset,
  getVideoReferenceRoles,
  VIDEO_MODE_LABEL_KEYS,
  VIDEO_REFERENCE_ROLE_LABEL_KEYS,
} from '../video-domain'
import {
  VIDEO_SAMPLE_CATEGORIES,
  VIDEO_SAMPLE_CATEGORIES_ENABLED,
  VIDEO_SAMPLE_CATEGORY_LABEL_KEYS,
  type VideoSampleCategory,
} from '../video-sample-categories'
import { VideoAdminWorkspace } from './video-admin-workspace'

export function VideoSampleAdmin() {
  const { t } = useTranslation()
  const modelsQuery = useAdminVideoModels()
  const samplesQuery = useAdminVideoSamples()
  const saveMutation = useSaveAdminVideoSample()
  const deleteMutation = useDeleteAdminVideoSample()
  const [selected, setSelected] = useState<VideoSample | null>(null)
  const [videoAssets, setVideoAssets] = useState<VideoAsset[]>([])
  const [referenceAssets, setReferenceAssets] = useState<VideoAsset[]>([])
  const [deleteOpen, setDeleteOpen] = useState(false)
  const form = useForm<VideoSampleFormValues>({
    resolver: zodResolver(videoSampleFormSchema),
    defaultValues: createVideoSampleFormValues(),
  })
  const samples = useMemo(
    () => samplesQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [samplesQuery.data]
  )
  const selectedProfileId = form.watch('model_profile_id')
  const selectedMode = form.watch('mode')
  const selectedProfile = modelsQuery.data?.find(
    (model) => model.id === selectedProfileId
  )

  useEffect(() => {
    const profile = modelsQuery.data?.find(
      (candidate) => candidate.id === selected?.model_profile_id
    )
    form.reset(createVideoSampleFormValues(selected ?? undefined, profile))
    setVideoAssets(selected ? [getSampleVideoAsset(selected)] : [])
    setReferenceAssets(
      selected ? getSampleReferenceAssets(selected, profile) : []
    )
  }, [form, modelsQuery.data, selected])

  useEffect(() => {
    form.setValue('video_asset_id', videoAssets[0]?.id ?? 0, {
      shouldValidate: true,
    })
  }, [form, videoAssets])

  useEffect(() => {
    form.setValue(
      'reference_asset_ids',
      referenceAssets.map((asset) => asset.id),
      { shouldValidate: true }
    )
  }, [form, referenceAssets])

  const availableModes = useMemo<VideoGenerationMode[]>(
    () => selectedProfile?.specification.modes ?? [],
    [selectedProfile?.specification.modes]
  )
  const referenceRoles = selectedProfile
    ? getVideoReferenceRoles(selectedProfile, selectedMode)
    : []
  const referenceLimit = referenceRoles.length
  const referenceLabels = referenceRoles.map((role) =>
    t(VIDEO_REFERENCE_ROLE_LABEL_KEYS[role])
  )
  const usesVideoReference = referenceRoles.includes('reference_video')
  const videoAssetReady =
    videoAssets.length === 1 && videoAssets[0]?.state === 'ready'
  const referenceAssetsReady =
    referenceAssets.length === referenceLimit &&
    referenceAssets.every((asset) => asset.state === 'ready')
  const assetsReady = videoAssetReady && referenceAssetsReady
  const samplePrepared = Boolean(
    videoAssets[0]?.poster_url && videoAssets[0]?.preview_url
  )

  useEffect(() => {
    if (!selectedProfile) return
    if (!selectedProfile.specification.modes.includes(selectedMode)) {
      const normalized = buildVideoSampleProfileState(
        selectedProfile,
        undefined,
        form.getValues('parameters')
      )
      form.setValue('mode', normalized.mode, { shouldValidate: true })
      form.setValue('parameters', normalized.parameters, {
        shouldValidate: true,
      })
      form.setValue('reference_asset_ids', [], { shouldValidate: true })
      setReferenceAssets([])
      return
    }
    setReferenceAssets((assets) => assets.slice(0, referenceLimit))
  }, [form, referenceLimit, selectedMode, selectedProfile])

  const changeProfile = (profileId: number) => {
    const profile = modelsQuery.data?.find(
      (candidate) => candidate.id === profileId
    )
    if (!profile) {
      form.setValue('model_profile_id', 0, {
        shouldDirty: true,
        shouldValidate: true,
      })
      form.setValue('mode', 'text_to_video', { shouldDirty: true })
      form.setValue('parameters', {}, { shouldDirty: true })
      form.setValue('reference_asset_ids', [], { shouldDirty: true })
      setReferenceAssets([])
      return
    }

    const next = buildVideoSampleProfileState(profile)
    form.setValue('model_profile_id', next.model_profile_id, {
      shouldDirty: true,
      shouldValidate: true,
    })
    form.setValue('mode', next.mode, {
      shouldDirty: true,
      shouldValidate: true,
    })
    form.setValue('parameters', next.parameters, {
      shouldDirty: true,
      shouldValidate: true,
    })
    form.setValue('reference_asset_ids', [], {
      shouldDirty: true,
      shouldValidate: true,
    })
    setReferenceAssets([])
  }

  const changeMode = (mode: VideoGenerationMode) => {
    if (!selectedProfile) return
    const next = buildVideoSampleProfileState(
      selectedProfile,
      mode,
      form.getValues('parameters')
    )
    form.setValue('mode', next.mode, {
      shouldDirty: true,
      shouldValidate: true,
    })
    form.setValue('parameters', next.parameters, {
      shouldDirty: true,
      shouldValidate: true,
    })
    form.setValue('reference_asset_ids', [], {
      shouldDirty: true,
      shouldValidate: true,
    })
    setReferenceAssets([])
  }

  const submit = form.handleSubmit(async (values) => {
    if (!selectedProfile) {
      form.setError('model_profile_id', {
        message: 'videoStudio.validation.modelRequired',
      })
      return
    }
    const expectedReferenceCount = getVideoReferenceRoles(
      selectedProfile,
      values.mode
    ).length
    if (values.reference_asset_ids.length !== expectedReferenceCount) {
      form.setError('reference_asset_ids', {
        message:
          expectedReferenceCount === 2
            ? 'videoStudio.validation.twoFramesRequired'
            : 'videoStudio.validation.imageRequired',
      })
      return
    }
    try {
      const saved = await saveMutation.mutateAsync({
        id: selected?.id,
        values: parseVideoSampleForm(values),
      })
      setSelected(saved)
      toast.success(t('videoStudio.admin.sampleSaved'))
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : t('videoStudio.admin.saveFailed')
      form.setError('root', { message })
    }
  })

  const confirmDelete = async () => {
    if (!selected) return
    try {
      await deleteMutation.mutateAsync(selected.id)
      setSelected(null)
      setDeleteOpen(false)
      toast.success(t('videoStudio.admin.sampleDeleted'))
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
          {t('videoStudio.admin.samples')}
        </span>
        <Button
          size='icon-sm'
          variant='ghost'
          onClick={() => setSelected(null)}
          aria-label={t('videoStudio.admin.addSample')}
        >
          <Plus aria-hidden='true' />
        </Button>
      </div>
      <div className='min-h-0 flex-1 overflow-y-auto p-2'>
        {samplesQuery.isLoading && (
          <div className='flex justify-center py-8' role='status'>
            <LoaderCircle
              className='text-muted-foreground size-5 animate-spin motion-reduce:animate-none'
              aria-hidden='true'
            />
          </div>
        )}
        {samples.map((sample) => (
          <button
            key={sample.id}
            type='button'
            className={cn(
              'hover:bg-muted flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left transition-colors',
              selected?.id === sample.id && 'bg-muted'
            )}
            onClick={() => setSelected(sample)}
          >
            <span
              className={cn(
                'size-2 shrink-0 rounded-full',
                sample.status === 'published' ? 'bg-success' : 'bg-warning'
              )}
              aria-hidden='true'
            />
            <span className='min-w-0 flex-1'>
              <span className='block truncate text-sm font-medium'>
                {sample.title || sample.prompt}
              </span>
              <span className='text-muted-foreground block truncate text-xs'>
                {sample.model_display_name || sample.model}
              </span>
            </span>
          </button>
        ))}
        {samplesQuery.hasNextPage && (
          <Button
            className='mt-2 w-full'
            variant='ghost'
            size='sm'
            disabled={samplesQuery.isFetchingNextPage}
            onClick={() => samplesQuery.fetchNextPage()}
          >
            {samplesQuery.isFetchingNextPage && (
              <LoaderCircle
                className='animate-spin motion-reduce:animate-none'
                aria-hidden='true'
              />
            )}
            {t('videoStudio.loadMore')}
          </Button>
        )}
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
              ? t('videoStudio.admin.editSample')
              : t('videoStudio.admin.addSample')}
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
              disabled={saveMutation.isPending || !assetsReady}
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
              name='model_profile_id'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('videoStudio.model')}</FormLabel>
                  <FormControl>
                    <NativeSelect
                      className='w-full'
                      value={field.value}
                      onChange={(event) =>
                        changeProfile(Number(event.target.value))
                      }
                    >
                      <NativeSelectOption value={0}>
                        {t('videoStudio.admin.selectModel')}
                      </NativeSelectOption>
                      {modelsQuery.data?.map((model) => (
                        <NativeSelectOption key={model.id} value={model.id}>
                          {model.display_name}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='mode'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('videoStudio.mode')}</FormLabel>
                  <FormControl>
                    <NativeSelect
                      className='w-full'
                      value={field.value}
                      disabled={!selectedProfile}
                      onChange={(event) =>
                        changeMode(event.target.value as VideoGenerationMode)
                      }
                    >
                      {!selectedProfile && (
                        <NativeSelectOption value='text_to_video'>
                          {t('videoStudio.admin.selectModelFirst')}
                        </NativeSelectOption>
                      )}
                      {availableModes.map((mode) => (
                        <NativeSelectOption key={mode} value={mode}>
                          {t(VIDEO_MODE_LABEL_KEYS[mode])}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='title'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('videoStudio.admin.title')}</FormLabel>
                  <FormControl>
                    <Input {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            {VIDEO_SAMPLE_CATEGORIES_ENABLED && (
              <FormField
                control={form.control}
                name='category'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('videoStudio.admin.category')}</FormLabel>
                    <FormControl>
                      <NativeSelect
                        className='w-full'
                        value={field.value}
                        onChange={(event) =>
                          field.onChange(
                            event.target.value as VideoSampleCategory
                          )
                        }
                      >
                        {VIDEO_SAMPLE_CATEGORIES.map((category) => (
                          <NativeSelectOption key={category} value={category}>
                            {t(VIDEO_SAMPLE_CATEGORY_LABEL_KEYS[category])}
                          </NativeSelectOption>
                        ))}
                      </NativeSelect>
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}
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
            name='prompt'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('videoStudio.prompt')}</FormLabel>
                <FormControl>
                  <Textarea {...field} className='min-h-28' />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          {selectedProfile && (
            <div className='space-y-3'>
              <h4 className='text-sm font-medium'>
                {t('videoStudio.parameters')}
              </h4>
              <VideoParameterFields profile={selectedProfile} />
            </div>
          )}
          <div className='space-y-2'>
            <span className='text-sm font-medium'>
              {t('videoStudio.admin.sampleVideo')}
            </span>
            <VideoAssetUploader
              assets={videoAssets}
              onAssetsChange={setVideoAssets}
              purpose='sample'
              maxFiles={1}
              accept={['video/mp4', 'video/webm', 'video/quicktime']}
              label={t('videoStudio.admin.uploadVideo')}
              compact
              adminUpload
            />
            <FormMessage />
          </div>
          {referenceLimit > 0 && (
            <div className='space-y-2'>
              <span className='text-sm font-medium'>
                {t('videoStudio.admin.references')}
              </span>
              <VideoAssetUploader
                key={`${selectedProfileId}-${selectedMode}-${referenceLimit}-${usesVideoReference ? 'video' : 'image'}`}
                assets={referenceAssets}
                onAssetsChange={setReferenceAssets}
                purpose={usesVideoReference ? 'reference_video' : 'reference'}
                maxFiles={referenceLimit}
                accept={
                  usesVideoReference
                    ? ['video/mp4', 'video/webm', 'video/quicktime']
                    : ['image/jpeg', 'image/png', 'image/webp']
                }
                label={t(
                  usesVideoReference
                    ? 'videoStudio.addVideo'
                    : 'videoStudio.addImage'
                )}
                assetLabels={referenceLabels}
                compact
                adminUpload
              />
            </div>
          )}
          <FormField
            control={form.control}
            name='status'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between gap-3 border-t pt-4'>
                <FormLabel>{t('videoStudio.admin.published')}</FormLabel>
                <FormControl>
                  <Switch
                    checked={field.value === 'published'}
                    disabled={!samplePrepared && field.value !== 'published'}
                    onCheckedChange={(checked) =>
                      field.onChange(checked ? 'published' : 'draft')
                    }
                  />
                </FormControl>
              </FormItem>
            )}
          />
          {form.formState.errors.video_asset_id?.message && (
            <p className='text-destructive text-xs' role='alert'>
              {t(form.formState.errors.video_asset_id.message)}
            </p>
          )}
          {form.formState.errors.reference_asset_ids?.message && (
            <p className='text-destructive text-xs' role='alert'>
              {t(form.formState.errors.reference_asset_ids.message)}
            </p>
          )}
          {!assetsReady &&
            (videoAssets.length > 0 || referenceAssets.length > 0) && (
              <p className='text-muted-foreground text-xs' role='status'>
                {t('videoStudio.admin.waitForAssets')}
              </p>
            )}
          {assetsReady && !samplePrepared && (
            <p className='text-muted-foreground text-xs' role='status'>
              {t('videoStudio.admin.saveDraftForPreview')}
            </p>
          )}
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
              {t('videoStudio.admin.deleteSample')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t('videoStudio.admin.deleteSampleDescription')}
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
