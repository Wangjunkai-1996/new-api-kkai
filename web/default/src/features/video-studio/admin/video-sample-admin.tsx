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
import { useConsoleContract } from '@/hooks/use-console-contract'

import { zodResolver } from '@hookform/resolvers/zod'
import { isAxiosError } from 'axios'
import { LoaderCircle, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
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
import type {
  VideoAsset,
  VideoComposerReference,
  VideoGenerationMode,
  VideoSample,
  VideoStudioApiError,
} from '../types'
import {
  getSampleReferenceAssets,
  getSampleVideoAsset,
  VIDEO_MODE_LABEL_KEYS,
} from '../video-domain'
import {
  VIDEO_SAMPLE_CATEGORIES,
  VIDEO_SAMPLE_CATEGORY_LABEL_KEYS,
  type VideoSampleCategory,
} from '../video-sample-categories'
import { VideoAdminWorkspace } from './video-admin-workspace'
import { validateVideoReferenceMedia } from '../video-reference-validation'

const referenceMimeKind = (asset: VideoAsset): 'image' | 'video' | 'audio' => {
  if (asset.mime_type.startsWith('audio/')) return 'audio'
  if (asset.mime_type.startsWith('video/')) return 'video'
  return 'image'
}

const buildReferenceBindings = (
  assets: VideoAsset[],
  mode: VideoGenerationMode
): VideoSampleFormValues['reference_assets'] => {
  const bindings: VideoSampleFormValues['reference_assets'] = []
  for (const kind of ['image', 'video', 'audio'] as const) {
    let role: VideoSampleFormValues['reference_assets'][number]['role']
    if (kind === 'image') role = 'reference_image'
    else if (kind === 'video') role = 'reference_video'
    else role = 'reference_audio'
    assets
      .filter((asset) => referenceMimeKind(asset) === kind)
      .forEach((asset, position) => {
        if (kind === 'image' && mode === 'first_frame_to_video') {
          bindings.push({ asset_id: asset.id, role: 'first_frame', position })
        } else if (kind === 'image' && mode === 'first_last_frame') {
          bindings.push({
            asset_id: asset.id,
            role: position === 0 ? 'first_frame' : 'last_frame',
            position: Math.max(0, position - 1),
          })
        } else {
          bindings.push({ asset_id: asset.id, role, position })
        }
      })
  }
  return bindings
}

export function VideoSampleAdmin() {
  const { t } = useTranslation()
  const { contract } = useConsoleContract()
  const categoriesEnabled = contract?.capabilities.includes('video_sample_categories') ?? false
  const modelsQuery = useAdminVideoModels()
  const samplesQuery = useAdminVideoSamples()
  const saveMutation = useSaveAdminVideoSample()
  const deleteMutation = useDeleteAdminVideoSample()
  const [selected, setSelected] = useState<VideoSample | null>(null)
  const [videoAssets, setVideoAssets] = useState<VideoAsset[]>([])
  const [referenceAssets, setReferenceAssets] = useState<VideoAsset[]>([])
  const [missingReferences, setMissingReferences] = useState<VideoSampleFormValues['reference_assets']>([])
  const [deleteOpen, setDeleteOpen] = useState(false)
  const initializedSampleIdRef = useRef<number | null | undefined>(undefined)
  const initializedSampleProfileIdRef = useRef(0)
  const analyzedAssetSignatureRef = useRef('')
  const titledAssetIdRef = useRef(0)
  const preparingAssetIdRef = useRef(0)
  const [preparationNow, setPreparationNow] = useState(() => Date.now())
  const form = useForm<VideoSampleFormValues>({
    resolver: zodResolver(videoSampleFormSchema),
    defaultValues: createVideoSampleFormValues(),
  })
  const samples = useMemo(
    () => samplesQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [samplesQuery.data]
  )
  const selectedProfileId = useWatch({ control: form.control, name: 'model_profile_id' })
  const selectedMode = useWatch({ control: form.control, name: 'mode' })
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
    Boolean(selected && sampleVideoAsset?.id === selected.video_asset_id)
  )
  const waitingForPreparedSample = Boolean(
    selected &&
    sampleVideoAsset?.id === selected.video_asset_id &&
    sampleVideoAsset.state !== 'failed' &&
    sampleVideoAsset.state !== 'deleting' &&
    sampleVideoAsset.state !== 'deleted' &&
    (!sampleVideoAsset.poster_url || !sampleVideoAsset.preview_url)
  )
  useEffect(() => {
    if (!waitingForPreparedSample) return
    const updateNow = () => setPreparationNow(Date.now())
    updateNow()
    const timer = window.setInterval(updateNow, 1_000)
    return () => window.clearInterval(timer)
  }, [
    sampleVideoAsset?.id,
    sampleVideoAsset?.updated_at,
    waitingForPreparedSample,
  ])
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
    // oxlint-disable-next-line react/set-state-in-effect -- Hydrate the editor when an async catalog selection resolves.
    setVideoAssets(selected ? [getSampleVideoAsset(selected)] : [])
    setReferenceAssets(
      selected ? getSampleReferenceAssets(selected, profile) : []
    )
    setMissingReferences(
      (selected?.reference_assets ?? []).filter((reference) => !reference.asset) as VideoSampleFormValues['reference_assets']
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
    // oxlint-disable-next-line react/set-state-in-effect -- Synchronize externally prepared media metadata without resetting form edits.
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
      'reference_assets',
      [...buildReferenceBindings(referenceAssets, selectedMode), ...missingReferences],
      {
      shouldValidate: form.formState.submitCount > 0,
      }
    )
  }, [form, form.formState.submitCount, referenceAssets, selectedMode, missingReferences])

  const availableModes = useMemo<VideoGenerationMode[]>(
    () => selectedProfile?.specification.modes ?? [],
    [selectedProfile?.specification.modes]
  )
  const referencePolicy = selectedProfile?.specification.reference_policy
  const referenceLimit = referencePolicy?.max_total_count ?? 0
  const unsupportedVideoDuration =
    selectedProfile && sampleVideoAsset?.state === 'ready'
      ? getUnsupportedVideoSampleDuration(selectedProfile, sampleVideoAsset)
      : undefined
  const videoAssetReady =
    videoAssets.length === 1 &&
    sampleVideoAsset?.state === 'ready' &&
    sampleVideoAssetHasMetadata
  const referenceBindings = buildReferenceBindings(referenceAssets, selectedMode)
  const composerReferences: VideoComposerReference[] = referenceAssets.flatMap((asset) => {
    const binding = buildReferenceBindings([asset], selectedMode)[0]
    return binding
      ? [{
          client_id: `sample-${asset.id}`,
          asset_id: asset.id,
          role: binding.role,
          position: binding.position,
          included: true,
          asset,
        }]
      : []
  })
  const referenceMediaError = selectedProfile && selectedMode === 'reference_to_video'
    ? validateVideoReferenceMedia(composerReferences, selectedProfile)
    : null
  const referenceAssetsReady =
    missingReferences.length === 0 &&
    referenceBindings.length <= referenceLimit &&
    referenceAssets.every((asset) => asset.state === 'ready') &&
    referenceMediaError === null &&
    (selectedMode === 'text_to_video' ||
      !referencePolicy?.requires_visual ||
      referenceBindings.some(
        (binding) =>
          binding.role === 'reference_image' || binding.role === 'reference_video'
      )) &&
    (!referencePolicy?.requires_video ||
      referenceBindings.some((binding) => binding.role === 'reference_video'))
  const assetsInspected = videoAssetReady && referenceAssetsReady
  const assetsReady = assetsInspected && unsupportedVideoDuration === undefined
  const samplePrepared = Boolean(
    sampleVideoAsset?.poster_url && sampleVideoAsset?.preview_url
  )
  const preparationElapsedSeconds =
    waitingForPreparedSample && sampleVideoAsset
      ? Math.max(
          0,
          Math.floor(preparationNow / 1_000) - sampleVideoAsset.updated_at
        )
      : 0
  const preparationTakingLong = preparationElapsedSeconds >= 60

  useEffect(() => {
    const assetId = sampleVideoAsset?.id ?? 0
    if (waitingForPreparedSample) {
      preparingAssetIdRef.current = assetId
      return
    }
    if (
      assetId > 0 &&
      samplePrepared &&
      preparingAssetIdRef.current === assetId
    ) {
      toast.success(t('videoStudio.admin.previewReady'))
    }
    preparingAssetIdRef.current = 0
  }, [samplePrepared, sampleVideoAsset?.id, t, waitingForPreparedSample])

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
    setMissingReferences([])
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
      form.setValue('reference_assets', buildReferenceBindings(referenceAssets, 'text_to_video'), { shouldDirty: true })
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
    form.setValue('reference_assets', buildReferenceBindings(referenceAssets, next.mode), {
      shouldDirty: true,
      shouldValidate: true,
    })
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
    form.setValue('reference_assets', buildReferenceBindings(referenceAssets, next.mode), {
      shouldDirty: true,
      shouldValidate: true,
    })
  }

  const submit = (event: React.FormEvent<HTMLFormElement>) => form.handleSubmit(async (values) => {
    if (!selectedProfile) {
      form.setError('model_profile_id', {
        message: 'videoStudio.validation.modelRequired',
      })
      return
    }
    if (missingReferences.length > 0) {
      form.setError('reference_assets', {
        message: 'videoStudio.validation.referenceUnavailable',
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
    const referenceBindings = buildReferenceBindings(referenceAssets, values.mode)
    if (values.mode === 'text_to_video' && referenceBindings.length > 0) {
      form.setError('reference_assets', {
        message: 'videoStudio.validation.textModeHasReferences',
      })
      return
    }
    if (
      selectedProfile.specification.reference_policy?.max_total_count != null &&
      referenceBindings.length >
        selectedProfile.specification.reference_policy.max_total_count
    ) {
      form.setError('reference_assets', {
        message: 'videoStudio.validation.referenceLimitExceeded',
      })
      return
    }
    const normalized = buildVideoSampleProfileState(
      selectedProfile,
      values.mode,
      values.parameters,
      referenceBindings as VideoSampleFormValues['reference_assets']
    )
    if (
      values.mode === 'first_frame_to_video' &&
      referenceBindings.filter((binding) => binding.role === 'first_frame').length !== 1
    ) {
      form.setError('reference_assets', {
        message: 'videoStudio.validation.imageRequired',
      })
      return
    }
    if (
      values.mode === 'first_last_frame' &&
      (referenceBindings.filter((binding) => binding.role === 'first_frame').length !== 1 ||
        referenceBindings.filter((binding) => binding.role === 'last_frame').length !== 1)
    ) {
      form.setError('reference_assets', {
        message: 'videoStudio.validation.twoFramesRequired',
      })
      return
    }
    if (referenceMediaError) {
      form.setError('reference_assets', { message: referenceMediaError })
      return
    }
    if (values.mode !== 'text_to_video' && selectedProfile.specification.reference_policy?.requires_visual &&
      !referenceBindings.some((binding) => binding.role === 'reference_image' || binding.role === 'reference_video')) {
      form.setError('reference_assets', { message: 'videoStudio.validation.imageRequired' })
      return
    }
    if (referenceAssets.some((asset) => asset.state !== 'ready')) {
      form.setError('reference_assets', {
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
      reference_assets: referenceBindings as VideoSampleFormValues['reference_assets'],
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
      setVideoAssets((assets) => {
        const current = assets[0]
        if (assets.length !== 1 || current?.id !== saved.video_asset_id) {
          return assets
        }
        return [
          {
            ...current,
            scope: 'catalog',
            content_url: saved.video_url || current.content_url,
            poster_url: saved.poster_url || current.poster_url,
            preview_url: saved.preview_url || current.preview_url,
            updated_at: saved.updated_at,
          },
        ]
      })
      toast.success(t('videoStudio.admin.sampleSaved'))
    } catch (error) {
      const responseError = isAxiosError<VideoStudioApiError>(error)
        ? error.response?.data
        : undefined
      const message =
        responseError?.message ||
        (error instanceof Error
          ? error.message
          : t('videoStudio.admin.saveFailed'))
      form.setError('root', { message })
    }
  })(event)

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
              key={`sample-video-${selected?.id ?? 'new'}`}
              assets={videoAssets}
              onAssetsChange={changeVideoAssets}
              purpose='sample'
              maxFiles={1}
              accept={['video/mp4', 'video/webm', 'video/quicktime']}
              label={t('videoStudio.admin.uploadVideo')}
              assetLabels={[t('videoStudio.admin.sampleVideo')]}
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
            {categoriesEnabled && (
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
          {missingReferences.length > 0 && (
            <div className='space-y-2' role='alert'>
              <p className='text-destructive text-sm'>
                {t('videoStudio.referenceUnavailable')}
              </p>
              {missingReferences.map((reference) => (
                <div key={`${reference.role}-${reference.position}-${reference.asset_id}`} className='flex items-center justify-between gap-2 rounded-md border px-3 py-2'>
                  <span className='text-muted-foreground text-sm'>#{reference.asset_id}</span>
                  <Button type='button' size='sm' variant='outline' onClick={() => setMissingReferences((references) => references.filter((item) => item !== reference))}>
                    {t('videoStudio.removeReference')}
                  </Button>
                </div>
              ))}
            </div>
          )}
          {referenceLimit > 0 && (
            <div className='space-y-2'>
              <span className='text-sm font-medium'>
                {t('videoStudio.admin.references')}
              </span>
              {referencePolicy?.image && (
                <VideoAssetUploader
                  key={`${selected?.id ?? 'new'}-ref-image`}
                  assets={referenceAssets.filter((asset) => referenceMimeKind(asset) === 'image')}
                  onAssetsChange={(assets) => changeReferenceAssets([
                    ...referenceAssets.filter((asset) => referenceMimeKind(asset) !== 'image'),
                    ...assets,
                  ])}
                  purpose='reference'
                  maxFiles={referencePolicy.image.max_count}
                  accept={referencePolicy.image.allowed_mime_types}
                  label={t('videoStudio.admin.referenceImages')}
                  compact
                  adminUpload
                />
              )}
              {referencePolicy?.video && (
                <VideoAssetUploader
                  key={`${selected?.id ?? 'new'}-ref-video`}
                  assets={referenceAssets.filter((asset) => referenceMimeKind(asset) === 'video')}
                  onAssetsChange={(assets) => changeReferenceAssets([
                    ...referenceAssets.filter((asset) => referenceMimeKind(asset) !== 'video'),
                    ...assets,
                  ])}
                  purpose='reference_video'
                  maxFiles={referencePolicy.video.max_count}
                  accept={referencePolicy.video.allowed_mime_types}
                  label={t('videoStudio.admin.referenceVideo')}
                  compact
                  adminUpload
                />
              )}
              {referencePolicy?.audio && (
                <VideoAssetUploader
                  key={`${selected?.id ?? 'new'}-ref-audio`}
                  assets={referenceAssets.filter((asset) => referenceMimeKind(asset) === 'audio')}
                  onAssetsChange={(assets) => changeReferenceAssets([
                    ...referenceAssets.filter((asset) => referenceMimeKind(asset) !== 'audio'),
                    ...assets,
                  ])}
                  purpose='reference_audio'
                  maxFiles={referencePolicy.audio.max_count}
                  accept={referencePolicy.audio.allowed_mime_types}
                  label={t('videoStudio.admin.referenceAudio')}
                  compact
                  adminUpload
                />
              )}
              {form.formState.errors.reference_assets?.message && (
                <p className='text-destructive text-xs' role='alert'>
                  {t(form.formState.errors.reference_assets.message)}
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
            !sampleVideoAssetQuery.isError &&
            (videoAssets.length > 0 || referenceAssets.length > 0) && (
              <p className='text-muted-foreground text-xs' role='status'>
                {t('videoStudio.admin.waitForAssets')}
              </p>
            )}
          {sampleVideoAssetQuery.isError && (
            <div
              className='text-destructive flex items-center justify-between gap-2 text-xs'
              role='alert'
            >
              <span>{t('videoStudio.refreshFailed')}</span>
              <Button
                type='button'
                size='xs'
                variant='outline'
                onClick={() => void sampleVideoAssetQuery.refetch()}
                disabled={sampleVideoAssetQuery.isFetching}
              >
                {sampleVideoAssetQuery.isFetching ? (
                  <LoaderCircle
                    className='animate-spin motion-reduce:animate-none'
                    aria-hidden='true'
                  />
                ) : (
                  <RefreshCw aria-hidden='true' />
                )}
                {t('videoStudio.recheck')}
              </Button>
            </div>
          )}
          {assetsReady &&
            !samplePrepared &&
            (waitingForPreparedSample ? (
              <div className='border-border bg-muted/30 flex items-start gap-2 rounded-md border px-3 py-2.5'>
                <LoaderCircle
                  className='text-muted-foreground mt-0.5 size-4 shrink-0 animate-spin motion-reduce:animate-none'
                  aria-hidden='true'
                />
                <div className='min-w-0 space-y-0.5'>
                  <p className='text-sm font-medium' role='status'>
                    {t('videoStudio.admin.preparingPreview')}
                  </p>
                  <p className='text-muted-foreground text-xs tabular-nums'>
                    {t(
                      preparationTakingLong
                        ? 'videoStudio.admin.preparingPreviewTakingLong'
                        : 'videoStudio.admin.preparingPreviewElapsed',
                      { seconds: preparationElapsedSeconds }
                    )}
                  </p>
                </div>
              </div>
            ) : (
              <p className='text-muted-foreground text-xs' role='status'>
                {t('videoStudio.admin.saveDraftForPreview')}
              </p>
            ))}
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
