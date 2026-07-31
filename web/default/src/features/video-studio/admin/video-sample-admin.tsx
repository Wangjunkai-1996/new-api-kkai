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
import { useEffect, useMemo, useRef, useState } from 'react'
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
import {
  useAdminVideoAsset,
  useAdminVideoSample,
  useAdminVideoModels,
  useAdminVideoSamples,
  useDeleteAdminVideoSample,
  useSaveAdminVideoSample,
} from '../queries'
import {
  buildVideoSampleProfileState,
  createVideoSampleFormValues,
  getUnsupportedVideoSampleDuration,
  getVideoSampleParametersForAsset,
  parseVideoSampleForm,
  VIDEO_PROMPT_MAX_LENGTH,
  VIDEO_SAMPLE_SORT_ORDER_MAX,
  VIDEO_SAMPLE_SORT_ORDER_MIN,
  VIDEO_SAMPLE_TITLE_MAX_LENGTH,
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
  const initializedSampleIdRef = useRef<number | null | undefined>(undefined)
  const initializedSampleProfileIdRef = useRef(0)
  const analyzedAssetSignatureRef = useRef('')
  const titledAssetIdRef = useRef(0)
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
  const sampleVideoAsset = videoAssets[0]
  const sampleVideoAssetHasMetadata = Boolean(
    sampleVideoAsset &&
    sampleVideoAsset.width > 0 &&
    sampleVideoAsset.height > 0 &&
    sampleVideoAsset.duration_seconds > 0
  )
  const sampleVideoAssetQuery = useAdminVideoAsset(
    selected?.video_asset_id,
    Boolean(
      selected &&
      sampleVideoAsset?.id === selected.video_asset_id &&
      !sampleVideoAssetHasMetadata
    )
  )
  const waitingForPreparedSample = Boolean(
    selected &&
    sampleVideoAsset?.id === selected.video_asset_id &&
    (!sampleVideoAsset.poster_url || !sampleVideoAsset.preview_url)
  )
  const sampleDetailQuery = useAdminVideoSample(
    selected?.id ?? 0,
    waitingForPreparedSample
  )

  useEffect(() => {
    const sampleId = selected?.id ?? null
    const profile = modelsQuery.data?.find(
      (candidate) => candidate.id === selected?.model_profile_id
    )
    const sameSample = initializedSampleIdRef.current === sampleId
    if (
      sameSample &&
      (!selected || initializedSampleProfileIdRef.current > 0 || !profile)
    ) {
      return
    }
    if (selected && !profile && modelsQuery.isPending) return
    initializedSampleIdRef.current = sampleId
    initializedSampleProfileIdRef.current = profile?.id ?? 0
    analyzedAssetSignatureRef.current = ''
    titledAssetIdRef.current = 0
    form.reset(createVideoSampleFormValues(selected ?? undefined, profile))
    setVideoAssets(selected ? [getSampleVideoAsset(selected)] : [])
    setReferenceAssets(
      selected ? getSampleReferenceAssets(selected, profile) : []
    )
  }, [form, modelsQuery.data, modelsQuery.isPending, selected])

  useEffect(() => {
    const hydrated = sampleVideoAssetQuery.data
    if (
      !selected ||
      !hydrated ||
      hydrated.id !== selected.video_asset_id ||
      hydrated.id !== sampleVideoAsset?.id
    ) {
      return
    }
    setVideoAssets((assets) => {
      const current = assets[0]
      if (assets.length !== 1 || current?.id !== hydrated.id) return assets
      return [
        {
          ...hydrated,
          content_url: hydrated.content_url || current.content_url,
          poster_url: hydrated.poster_url || current.poster_url,
          preview_url: hydrated.preview_url || current.preview_url,
        },
      ]
    })
  }, [sampleVideoAsset?.id, sampleVideoAssetQuery.data, selected])

  useEffect(() => {
    form.setValue('video_asset_id', sampleVideoAsset?.id ?? 0, {
      shouldValidate: form.formState.submitCount > 0,
    })
  }, [form, form.formState.submitCount, sampleVideoAsset?.id])

  useEffect(() => {
    form.setValue(
      'reference_asset_ids',
      referenceAssets.map((asset) => asset.id),
      { shouldValidate: form.formState.submitCount > 0 }
    )
  }, [form, form.formState.submitCount, referenceAssets])

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
  const unsupportedVideoDuration =
    selectedProfile && sampleVideoAsset?.state === 'ready'
      ? getUnsupportedVideoSampleDuration(selectedProfile, sampleVideoAsset)
      : undefined
  const videoAssetReady =
    videoAssets.length === 1 &&
    sampleVideoAsset?.state === 'ready' &&
    sampleVideoAssetHasMetadata
  const referenceAssetsReady =
    referenceAssets.length === referenceLimit &&
    referenceAssets.every((asset) => asset.state === 'ready')
  const assetsInspected = videoAssetReady && referenceAssetsReady
  const assetsReady = assetsInspected && unsupportedVideoDuration === undefined
  const samplePrepared = Boolean(
    sampleVideoAsset?.poster_url && sampleVideoAsset?.preview_url
  )

  useEffect(() => {
    const durationError = 'videoStudio.validation.sampleDurationUnsupported'
    const currentError = form.getFieldState('video_asset_id').error?.message
    if (unsupportedVideoDuration !== undefined) {
      form.setError('video_asset_id', { message: durationError })
      return
    }
    if (currentError === durationError) {
      form.clearErrors('video_asset_id')
    }
  }, [form, unsupportedVideoDuration])

  useEffect(() => {
    const refreshed = sampleDetailQuery.data
    if (!refreshed || refreshed.video_asset_id !== sampleVideoAsset?.id) return
    setVideoAssets((assets) => {
      let changed = false
      const next = assets.map((asset) => {
        if (asset.id !== refreshed.video_asset_id) return asset
        const contentUrl = refreshed.video_url || asset.content_url
        const posterUrl = refreshed.poster_url || asset.poster_url
        const previewUrl = refreshed.preview_url || asset.preview_url
        if (
          contentUrl === asset.content_url &&
          posterUrl === asset.poster_url &&
          previewUrl === asset.preview_url
        ) {
          return asset
        }
        changed = true
        return {
          ...asset,
          content_url: contentUrl,
          poster_url: posterUrl,
          preview_url: previewUrl,
        }
      })
      return changed ? next : assets
    })
  }, [sampleDetailQuery.data, sampleVideoAsset?.id])

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

  useEffect(() => {
    if (selected || sampleVideoAsset?.state !== 'ready') return
    if (titledAssetIdRef.current === sampleVideoAsset.id) return
    titledAssetIdRef.current = sampleVideoAsset.id
    if (form.getValues('title').trim()) return
    const title = sampleVideoAsset.original_filename
      .replace(/\.[^.]+$/, '')
      .trim()
      .slice(0, VIDEO_SAMPLE_TITLE_MAX_LENGTH)
    if (title) {
      form.setValue('title', title, {
        shouldDirty: true,
        shouldValidate: true,
      })
    }
  }, [form, sampleVideoAsset, selected])

  useEffect(() => {
    if (!selectedProfile || sampleVideoAsset?.state !== 'ready') {
      return
    }
    const signature = `${sampleVideoAsset.id}:${sampleVideoAsset.duration_seconds}:${sampleVideoAsset.width}:${sampleVideoAsset.height}:${selectedProfile.id}:${selectedMode}`
    if (analyzedAssetSignatureRef.current === signature) return
    analyzedAssetSignatureRef.current = signature
    const parameters = getVideoSampleParametersForAsset(
      selectedProfile,
      selectedMode,
      sampleVideoAsset,
      form.getValues('parameters')
    )
    const current = form.getValues('parameters')
    const currentKeys = Object.keys(current)
    const parameterKeys = Object.keys(parameters)
    const changed =
      currentKeys.length !== parameterKeys.length ||
      parameterKeys.some((key) => !Object.is(current[key], parameters[key]))
    if (!changed) return
    form.setValue('parameters', parameters, {
      shouldDirty: true,
      shouldValidate: form.formState.submitCount > 0,
    })
  }, [
    form,
    form.formState.submitCount,
    sampleVideoAsset,
    selectedMode,
    selectedProfile,
  ])

  const detectedVideoSummary =
    sampleVideoAsset?.state === 'ready' &&
    sampleVideoAsset.width > 0 &&
    sampleVideoAsset.height > 0 &&
    sampleVideoAsset.duration_seconds > 0
      ? t('videoStudio.admin.videoDetected', {
          duration: sampleVideoAsset.duration_seconds
            .toFixed(1)
            .replace(/\.0$/, ''),
          width: sampleVideoAsset.width,
          height: sampleVideoAsset.height,
        })
      : ''

  const startCreate = () => {
    initializedSampleIdRef.current = null
    initializedSampleProfileIdRef.current = 0
    analyzedAssetSignatureRef.current = ''
    titledAssetIdRef.current = 0
    setSelected(null)
    form.reset(createVideoSampleFormValues())
    setVideoAssets([])
    setReferenceAssets([])
  }

  const changeVideoAssets = (assets: VideoAsset[]) => {
    const nextAssetId = assets[0]?.id ?? 0
    form.setValue('video_asset_id', nextAssetId, {
      shouldValidate: form.formState.submitCount > 0,
    })
    if (selected && nextAssetId !== selected.video_asset_id) {
      form.setValue('status', 'draft', {
        shouldDirty: true,
        shouldValidate: false,
      })
    }
    setVideoAssets(assets)
  }

  const changeReferenceAssets = (assets: VideoAsset[]) => {
    form.setValue(
      'reference_asset_ids',
      assets.map((asset) => asset.id),
      { shouldValidate: form.formState.submitCount > 0 }
    )
    setReferenceAssets(assets)
  }

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

    const next = buildVideoSampleProfileState(
      profile,
      form.getValues('mode'),
      form.getValues('parameters')
    )
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
    if (!sampleVideoAsset || sampleVideoAsset.state !== 'ready') {
      form.setError('video_asset_id', {
        message: 'videoStudio.validation.sampleVideoRequired',
      })
      return
    }
    if (!sampleVideoAssetHasMetadata) {
      form.setError('video_asset_id', {
        message: 'videoStudio.admin.waitForAssets',
      })
      return
    }
    if (
      getUnsupportedVideoSampleDuration(selectedProfile, sampleVideoAsset) !==
      undefined
    ) {
      form.setError('video_asset_id', {
        message: 'videoStudio.validation.sampleDurationUnsupported',
      })
      return
    }
    const referenceAssetIds = referenceAssets.map((asset) => asset.id)
    const normalized = buildVideoSampleProfileState(
      selectedProfile,
      values.mode,
      values.parameters,
      referenceAssetIds
    )
    const expectedReferenceRoles = getVideoReferenceRoles(
      selectedProfile,
      normalized.mode
    )
    if (referenceAssetIds.length !== expectedReferenceRoles.length) {
      let message = 'videoStudio.validation.imageRequired'
      if (expectedReferenceRoles.length === 2) {
        message = 'videoStudio.validation.twoFramesRequired'
      } else if (expectedReferenceRoles.includes('reference_video')) {
        message = 'videoStudio.validation.videoRequired'
      }
      form.setError('reference_asset_ids', {
        message,
      })
      return
    }
    if (referenceAssets.some((asset) => asset.state !== 'ready')) {
      form.setError('reference_asset_ids', {
        message: 'videoStudio.admin.waitForAssets',
      })
      return
    }
    const parameters = getVideoSampleParametersForAsset(
      selectedProfile,
      normalized.mode,
      sampleVideoAsset,
      values.parameters
    )
    const videoReplaced = Boolean(
      selected && sampleVideoAsset.id !== selected.video_asset_id
    )
    const submittedValues: VideoSampleFormValues = {
      ...values,
      model_profile_id: selectedProfile.id,
      mode: normalized.mode,
      parameters,
      reference_asset_ids: referenceAssetIds,
      video_asset_id: sampleVideoAsset.id,
      status: videoReplaced ? 'draft' : values.status,
    }
    form.setValue('parameters', parameters)
    if (videoReplaced) form.setValue('status', 'draft')
    try {
      const saved = await saveMutation.mutateAsync({
        id: selected?.id,
        values: parseVideoSampleForm(submittedValues),
      })
      initializedSampleIdRef.current = saved.id
      initializedSampleProfileIdRef.current = selectedProfile.id
      form.reset(createVideoSampleFormValues(saved, selectedProfile))
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
      startCreate()
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
          onClick={startCreate}
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
              disabled={
                saveMutation.isPending || !selectedProfile || !assetsReady
              }
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
          <div className='space-y-2'>
            <span className='text-sm font-medium'>
              {t('videoStudio.admin.sampleVideo')}
            </span>
            <VideoAssetUploader
              assets={videoAssets}
              onAssetsChange={changeVideoAssets}
              purpose='sample'
              maxFiles={1}
              accept={['video/mp4', 'video/webm', 'video/quicktime']}
              label={t('videoStudio.admin.uploadVideo')}
              compact
              adminUpload
            />
            {detectedVideoSummary && (
              <p className='text-muted-foreground text-xs tabular-nums'>
                {detectedVideoSummary}
              </p>
            )}
            {form.formState.errors.video_asset_id?.message && (
              <p className='text-destructive text-xs' role='alert'>
                {t(form.formState.errors.video_asset_id.message)}
              </p>
            )}
          </div>
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
                    {selectedProfile && availableModes.length === 1 ? (
                      <Input
                        value={t(VIDEO_MODE_LABEL_KEYS[field.value])}
                        readOnly
                        aria-readonly='true'
                      />
                    ) : (
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
                    )}
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
                  <FormLabel>{t('videoStudio.admin.sampleTitle')}</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      maxLength={VIDEO_SAMPLE_TITLE_MAX_LENGTH}
                    />
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
                      min={VIDEO_SAMPLE_SORT_ORDER_MIN}
                      max={VIDEO_SAMPLE_SORT_ORDER_MAX}
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
                  <Textarea
                    {...field}
                    maxLength={VIDEO_PROMPT_MAX_LENGTH}
                    className='min-h-28'
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          {referenceLimit > 0 && (
            <div className='space-y-2'>
              <span className='text-sm font-medium'>
                {t(
                  usesVideoReference
                    ? 'videoStudio.admin.referenceVideo'
                    : 'videoStudio.admin.referenceImages'
                )}
              </span>
              <VideoAssetUploader
                key={`${selectedProfileId}-${selectedMode}-${referenceLimit}-${usesVideoReference ? 'video' : 'image'}`}
                assets={referenceAssets}
                onAssetsChange={changeReferenceAssets}
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
              {form.formState.errors.reference_asset_ids?.message && (
                <p className='text-destructive text-xs' role='alert'>
                  {t(form.formState.errors.reference_asset_ids.message)}
                </p>
              )}
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
          {!assetsInspected &&
            (videoAssets.length > 0 || referenceAssets.length > 0) && (
              <p className='text-muted-foreground text-xs' role='status'>
                {t('videoStudio.admin.waitForAssets')}
              </p>
            )}
          {assetsReady && !samplePrepared && (
            <p className='text-muted-foreground text-xs' role='status'>
              {t(
                waitingForPreparedSample
                  ? 'videoStudio.admin.preparingPreview'
                  : 'videoStudio.admin.saveDraftForPreview'
              )}
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
