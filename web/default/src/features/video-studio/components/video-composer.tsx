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
import { Link } from '@tanstack/react-router'
import { AxiosError } from 'axios'
import { ChevronDown, KeyRound, LoaderCircle, Sparkles } from 'lucide-react'
import { nanoid } from 'nanoid'
import { type ReactNode, useEffect, useMemo, useRef, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useDebounce } from '@/hooks/use-debounce'
import { formatQuota } from '@/lib/format'
import { useVideoStudioDraftStore } from '@/stores/video-studio-draft-store'

import { useVideoTokenGate } from '../hooks/use-video-token-gate'
import {
  useCreateVideoGeneration,
  useVideoModels,
  useVideoQuote,
  useVideoReferenceAssetHydration,
} from '../queries'
import {
  VIDEO_GENERATION_MODES,
  VIDEO_PROMPT_MAX_LENGTH,
  validateComposerForProfile,
  videoComposerSchema,
} from '../schemas'
import type {
  VideoAsset,
  VideoComposerValues,
  VideoGenerationMode,
  VideoSample,
  VideoStudioApiError,
  VideoSubmissionReceipt,
} from '../types'
import {
  buildClearedVideoComposerValues,
  buildCreateVideoRequest,
  buildVideoComposerValues,
  buildVideoQuoteRequest,
  getSampleReferenceAssets,
  getVideoQuoteRefreshDelay,
  getVideoSubmissionRequestKey,
  getVideoParametersForMode,
  getVideoSubmissionLock,
  getVideoReferenceRoles,
  isVideoQuoteStaleError,
  restoreVideoComposerDraft,
  type VideoSubmissionLock,
  VIDEO_MODE_LABEL_KEYS,
  VIDEO_REFERENCE_ROLE_LABEL_KEYS,
  videoQuoteHasExpired,
} from '../video-domain'
import {
  getHydratedVideoReferences,
  getVideoReferenceHydrationRecovery,
} from '../video-reference-hydration'
import { getVideoTokenErrorKind } from '../video-token-access'
import { VideoAssetUploader } from './video-asset-uploader'
import { VideoParameterFields } from './video-parameter-fields'
import { VideoTokenSetupDialog } from './video-token-setup-dialog'

type VideoComposerProps = {
  sample?: VideoSample
  onSubmitted?: (receipt: VideoSubmissionReceipt) => void
}

const getVideoStudioResponseError = (
  error: unknown
): VideoStudioApiError | undefined =>
  error instanceof AxiosError
    ? (error.response?.data as VideoStudioApiError | undefined)
    : undefined

export function VideoComposer(props: VideoComposerProps) {
  const { t } = useTranslation()
  const modelsQuery = useVideoModels()
  const draft = useVideoStudioDraftStore((state) => state.draft)
  const saveDraft = useVideoStudioDraftStore((state) => state.saveDraft)
  const clearDraft = useVideoStudioDraftStore((state) => state.clearDraft)
  const createMutation = useCreateVideoGeneration()
  const [referenceAssets, setReferenceAssets] = useState<VideoAsset[]>([])
  const [referenceHydrationIds, setReferenceHydrationIds] = useState<number[]>(
    []
  )
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [appliedSampleId, setAppliedSampleId] = useState<number | undefined>()
  const [submissionLock, setSubmissionLock] =
    useState<VideoSubmissionLock | null>(null)
  const [invalidQuoteKey, setInvalidQuoteKey] = useState<string | null>(null)
  const idempotencyKeyRef = useRef<string | null>(null)
  const idempotencyRequestRef = useRef<string | null>(null)
  const referenceUploadInputRef = useRef<HTMLInputElement>(null)
  const initializedRef = useRef(false)
  const referenceSyncBlockedRef = useRef(false)
  const referenceHydrationQueries = useVideoReferenceAssetHydration(
    referenceHydrationIds
  )

  const firstProfile = modelsQuery.data?.[0]
  const form = useForm<VideoComposerValues>({
    resolver: zodResolver(videoComposerSchema),
    defaultValues: {
      model_profile_id: 0,
      mode: 'text_to_video',
      prompt: '',
      reference_asset_ids: [],
      parameters: {},
    },
  })

  const values = useWatch({ control: form.control }) as VideoComposerValues
  const debouncedValues = useDebounce(values, 300)
  const selectedProfile = modelsQuery.data?.find(
    (profile) => profile.id === values.model_profile_id
  )
  const videoTokenGate = useVideoTokenGate(selectedProfile?.model)
  const videoTokenId = videoTokenGate.tokenId
  const blockAndRecheckVideoToken = videoTokenGate.blockAndRecheck
  const referenceHydrationFailed =
    referenceHydrationIds.length > 0 &&
    referenceHydrationQueries.some((query) => query.isError)
  const referenceHydrationPending = referenceHydrationQueries.some(
    (query) => !query.isFetchedAfterMount || query.isFetching
  )
  const referenceHydrationRecovery = getVideoReferenceHydrationRecovery(
    referenceHydrationIds,
    referenceHydrationQueries
  )

  useEffect(() => {
    if (!modelsQuery.data || initializedRef.current) return
    const restored = restoreVideoComposerDraft(modelsQuery.data, draft)
    if (!restored) return
    initializedRef.current = true
    referenceSyncBlockedRef.current = restored.reference_asset_ids.length > 0
    setReferenceHydrationIds(restored.reference_asset_ids)
    form.reset(restored)
  }, [draft, form, modelsQuery.data])

  useEffect(() => {
    if (!props.sample || !modelsQuery.data) return
    const profile = modelsQuery.data.find(
      (item) => item.id === props.sample?.model_profile_id
    )
    if (!profile) return
    referenceSyncBlockedRef.current = false
    setReferenceHydrationIds([])
    form.reset(buildVideoComposerValues(profile, props.sample))
    setReferenceAssets(getSampleReferenceAssets(props.sample))
    setAppliedSampleId(props.sample.id)
    setSubmissionLock(null)
  }, [form, modelsQuery.data, props.sample])

  useEffect(() => {
    if (!values.model_profile_id) return
    saveDraft(values)
  }, [saveDraft, values])

  useEffect(() => {
    if (referenceSyncBlockedRef.current) return
    form.setValue(
      'reference_asset_ids',
      referenceAssets
        .filter((asset) => asset.state === 'ready')
        .map((asset) => asset.id),
      { shouldValidate: true }
    )
  }, [form, referenceAssets])

  useEffect(() => {
    if (referenceHydrationIds.length === 0) return
    const hydratedReferences = getHydratedVideoReferences(
      referenceHydrationIds,
      referenceHydrationQueries
    )
    if (!hydratedReferences) return
    setReferenceAssets(hydratedReferences)
    referenceSyncBlockedRef.current = false
    setReferenceHydrationIds([])
  }, [referenceHydrationIds, referenceHydrationQueries])

  const quoteRequest = useMemo(() => {
    if (!videoTokenId) return null
    const parsed = videoComposerSchema.safeParse(debouncedValues)
    if (!parsed.success) return null
    const profile = modelsQuery.data?.find(
      (item) => item.id === parsed.data.model_profile_id
    )
    if (!profile || validateComposerForProfile(parsed.data, profile)) {
      return null
    }
    return buildVideoQuoteRequest(
      parsed.data,
      profile,
      videoTokenId,
      appliedSampleId
    )
  }, [appliedSampleId, debouncedValues, modelsQuery.data, videoTokenId])

  const quoteQuery = useVideoQuote(quoteRequest, quoteRequest !== null)
  const refetchQuote = quoteQuery.refetch
  const quote = quoteQuery.data
  const quoteKey = quote
    ? `${quote.request_hash}:${String(quote.expires_at)}`
    : null
  const quoteRequestKey = quoteRequest
    ? getVideoSubmissionRequestKey(quoteRequest)
    : null
  const composerValuesAreDebounced =
    JSON.stringify(values) === JSON.stringify(debouncedValues)
  const quoteReady =
    quote !== undefined &&
    quoteKey !== invalidQuoteKey &&
    composerValuesAreDebounced &&
    !videoQuoteHasExpired(quote.expires_at, Math.floor(Date.now() / 1000))
  const quoteDisplay =
    quote?.display_amount || (quote ? formatQuota(quote.quota) : '')

  useEffect(() => {
    if (!quote || !quoteKey) return

    setInvalidQuoteKey((current) =>
      current !== null && current !== quoteKey ? null : current
    )
    const expireQuote = () => {
      setInvalidQuoteKey(quoteKey)
      void refetchQuote()
    }
    const delay = getVideoQuoteRefreshDelay(quote.expires_at, Date.now())
    if (delay === 0) {
      expireQuote()
      return
    }
    const timeout = window.setTimeout(expireQuote, delay)
    return () => window.clearTimeout(timeout)
  }, [quote, quoteKey, refetchQuote])

  useEffect(() => {
    if (submissionLock === null) return
    if (idempotencyRequestRef.current !== quoteRequestKey) {
      setSubmissionLock(null)
    }
  }, [quoteRequestKey, submissionLock])

  useEffect(() => {
    if (!quoteQuery.isError) return
    const responseError = getVideoStudioResponseError(quoteQuery.error)
    const errorKind = getVideoTokenErrorKind(responseError?.code)
    blockAndRecheckVideoToken(errorKind)
  }, [blockAndRecheckVideoToken, quoteQuery.error, quoteQuery.isError])

  const handleModelChange = (profileId: number) => {
    const profile = modelsQuery.data?.find((item) => item.id === profileId)
    if (!profile) return
    const nextValues = buildVideoComposerValues(profile)
    nextValues.prompt = form.getValues('prompt')
    referenceSyncBlockedRef.current = false
    setReferenceHydrationIds([])
    form.reset(nextValues)
    setReferenceAssets([])
    setAppliedSampleId(undefined)
    setSubmissionLock(null)
    idempotencyKeyRef.current = null
    idempotencyRequestRef.current = null
    setSubmitError(null)
  }

  const handleModeChange = (mode: VideoGenerationMode) => {
    form.setValue('mode', mode, { shouldValidate: true })
    if (!selectedProfile) return
    form.setValue(
      'parameters',
      getVideoParametersForMode(
        selectedProfile,
        mode,
        form.getValues('parameters')
      ),
      { shouldValidate: true }
    )
    const referenceLimit = getVideoReferenceRoles(selectedProfile, mode).length
    const referenceAssetIds = form
      .getValues('reference_asset_ids')
      .slice(0, referenceLimit)
    form.setValue('reference_asset_ids', referenceAssetIds, {
      shouldValidate: true,
    })
    if (referenceSyncBlockedRef.current) {
      referenceSyncBlockedRef.current = referenceAssetIds.length > 0
      setReferenceHydrationIds(referenceAssetIds)
    }
    setReferenceAssets((assets) => assets.slice(0, referenceLimit))
  }

  const retryReferenceHydration = () => {
    void Promise.all(referenceHydrationQueries.map((query) => query.refetch()))
  }

  const focusReferenceUploader = () => {
    window.requestAnimationFrame(() => referenceUploadInputRef.current?.focus())
  }

  const removeUnavailableReferences = () => {
    if (!referenceHydrationRecovery || referenceHydrationPending) return
    referenceSyncBlockedRef.current = false
    setReferenceHydrationIds([])
    setReferenceAssets(referenceHydrationRecovery.retainedAssets)
    form.setValue(
      'reference_asset_ids',
      referenceHydrationRecovery.retainedAssetIds,
      { shouldDirty: true, shouldValidate: true }
    )
    focusReferenceUploader()
  }

  const clearRestoredDraft = () => {
    if (!selectedProfile) return
    const clearedValues = buildClearedVideoComposerValues(
      selectedProfile,
      values.mode
    )
    referenceSyncBlockedRef.current = false
    setReferenceHydrationIds([])
    setReferenceAssets([])
    setAppliedSampleId(undefined)
    setSubmissionLock(null)
    setInvalidQuoteKey(null)
    idempotencyKeyRef.current = null
    idempotencyRequestRef.current = null
    setSubmitError(null)
    clearDraft()
    form.reset(clearedValues)
    focusReferenceUploader()
  }

  const handleVideoTokenPrimaryAction = () => {
    setSubmitError(null)
    videoTokenGate.openOrRetry()
  }

  const handleSubmit = form.handleSubmit(async (submittedValues) => {
    if (!selectedProfile || !videoTokenId) return
    const validationError = validateComposerForProfile(
      submittedValues,
      selectedProfile
    )
    if (validationError) {
      setSubmitError(t(validationError))
      return
    }
    if (!quoteReady || !quote) {
      setSubmitError(t('videoStudio.quoteUnavailable'))
      await quoteQuery.refetch()
      return
    }

    const submissionRequest = buildVideoQuoteRequest(
      submittedValues,
      selectedProfile,
      videoTokenId,
      appliedSampleId
    )
    const submissionRequestKey = getVideoSubmissionRequestKey(submissionRequest)
    if (idempotencyRequestRef.current !== submissionRequestKey) {
      idempotencyKeyRef.current = nanoid()
      idempotencyRequestRef.current = submissionRequestKey
    }

    setSubmitError(null)
    try {
      const receipt = await createMutation.mutateAsync({
        request: buildCreateVideoRequest(submissionRequest, quote),
        idempotencyKey: idempotencyKeyRef.current ?? nanoid(),
      })
      idempotencyKeyRef.current = null
      idempotencyRequestRef.current = null
      toast.success(t('videoStudio.generationQueued'))
      props.onSubmitted?.(receipt)
    } catch (error) {
      const responseError = getVideoStudioResponseError(error)
      const responseStatus =
        error instanceof AxiosError ? error.response?.status : undefined
      if (isVideoQuoteStaleError(responseStatus, responseError)) {
        if (quoteKey) setInvalidQuoteKey(quoteKey)
        setSubmitError(t('videoStudio.quoteChanged'))
        await quoteQuery.refetch()
        return
      }
      const videoTokenErrorKind = getVideoTokenErrorKind(responseError?.code)
      if (videoTokenGate.blockAndRecheck(videoTokenErrorKind)) {
        let message = t('videoStudio.videoKey.invalidated')
        if (videoTokenErrorKind === 'group-unavailable') {
          message = t('videoStudio.videoKey.groupUnavailable', {
            group: videoTokenGate.requiredGroup,
          })
        }
        if (videoTokenErrorKind === 'limit-reached') {
          message = t('videoStudio.videoKey.limitReached')
        }
        if (videoTokenErrorKind === 'models-unavailable') {
          message = t('videoStudio.videoKey.modelsUnavailable')
        }
        setSubmitError(message)
        return
      }
      const nextSubmissionLock = getVideoSubmissionLock(responseError)
      if (nextSubmissionLock) {
        setSubmissionLock(nextSubmissionLock)
        setSubmitError(
          nextSubmissionLock.taskId
            ? t('videoStudio.submissionUnknownWithTask', {
                taskId: nextSubmissionLock.taskId,
              })
            : t('videoStudio.submissionUnknown')
        )
        return
      }
      const message =
        responseError?.message ||
        (error instanceof Error
          ? error.message
          : t('videoStudio.generationFailed'))
      setSubmitError(message)
    }
  })

  if (modelsQuery.isLoading && !modelsQuery.data) {
    return (
      <div className='flex min-h-48 items-center justify-center' role='status'>
        <LoaderCircle
          className='text-muted-foreground size-5 animate-spin motion-reduce:animate-none'
          aria-hidden='true'
        />
        <span className='sr-only'>{t('videoStudio.loadingModels')}</span>
      </div>
    )
  }

  if (modelsQuery.isError || !firstProfile) {
    return (
      <div className='flex min-h-48 flex-col items-center justify-center gap-3 px-5 text-center'>
        <p className='text-sm font-medium'>{t('videoStudio.noModels')}</p>
        <Button
          variant='outline'
          size='sm'
          onClick={() => modelsQuery.refetch()}
        >
          {t('videoStudio.retry')}
        </Button>
      </div>
    )
  }

  const referenceLimit = selectedProfile
    ? getVideoReferenceRoles(selectedProfile, values.mode).length
    : 0
  const referenceLabels = selectedProfile
    ? getVideoReferenceRoles(selectedProfile, values.mode).map((role) =>
        t(VIDEO_REFERENCE_ROLE_LABEL_KEYS[role])
      )
    : []
  const videoTokenReady = videoTokenGate.access?.kind === 'ready'
  const videoTokenMissing = videoTokenGate.access?.kind === 'missing'
  const videoTokenGroupUnavailable =
    videoTokenGate.access?.kind === 'group-unavailable'
  const videoTokenLimitReached = videoTokenGate.access?.kind === 'limit-reached'
  const videoTokenModelsUnavailable =
    videoTokenGate.access?.kind === 'models-unavailable'
  const videoTokenInvalid = videoTokenGate.access?.kind === 'invalid'
  let generateLabel = t('videoStudio.generate')
  if (!videoTokenReady) {
    if (
      videoTokenGate.checking ||
      (videoTokenGate.gateAction === 'recheck' && videoTokenGate.queryFetching)
    ) {
      generateLabel = t('videoStudio.videoKey.checking')
    } else if (videoTokenMissing) {
      generateLabel = t('videoStudio.videoKey.createAndContinue')
    } else if (videoTokenGate.gateAction === 'recheck') {
      generateLabel = t('videoStudio.videoKey.retryCheck')
    }
  } else {
    if (quoteReady) {
      generateLabel = t('videoStudio.generateWithPrice', {
        price: quoteDisplay,
      })
    }
    if (quoteQuery.isFetching) generateLabel = t('videoStudio.quoting')
  }
  const quoteRetryAvailable =
    videoTokenReady &&
    quoteRequest !== null &&
    quoteQuery.isError &&
    !quoteQuery.isFetching
  if (quoteRetryAvailable) generateLabel = t('videoStudio.retry')
  let quoteStatusMessage: ReactNode = null
  if (videoTokenGate.checking) {
    quoteStatusMessage = t('videoStudio.videoKey.checking')
  } else if (videoTokenGate.checkFailed) {
    quoteStatusMessage = t('videoStudio.videoKey.checkFailed')
  } else if (videoTokenMissing) {
    quoteStatusMessage = t('videoStudio.videoKey.requiredBeforePricing', {
      group: videoTokenGate.requiredGroup,
    })
  } else if (videoTokenGroupUnavailable) {
    quoteStatusMessage = t('videoStudio.videoKey.groupUnavailable', {
      group: videoTokenGate.requiredGroup,
    })
  } else if (videoTokenLimitReached) {
    quoteStatusMessage = (
      <span>
        {t('videoStudio.videoKey.limitReached')}{' '}
        <Link
          to='/keys'
          className='text-foreground underline underline-offset-2'
        >
          {t('videoStudio.videoKey.manageKeys')}
        </Link>
      </span>
    )
  } else if (videoTokenModelsUnavailable) {
    quoteStatusMessage = t('videoStudio.videoKey.modelsUnavailable')
  } else if (videoTokenInvalid) {
    quoteStatusMessage = t('videoStudio.videoKey.invalidResponse')
  } else if (quoteReady) {
    quoteStatusMessage = t('videoStudio.quoteReady')
  }
  if (submissionLock !== null) {
    quoteStatusMessage = submissionLock.taskId
      ? t('videoStudio.submissionLocked', { taskId: submissionLock.taskId })
      : t('videoStudio.submissionUnknown')
  }
  let primaryActionDisabled =
    createMutation.isPending ||
    videoTokenGate.creating ||
    submissionLock !== null
  if (videoTokenReady) {
    primaryActionDisabled ||= !quoteReady && !quoteRetryAvailable
  } else {
    primaryActionDisabled ||=
      !videoTokenGate.actionAvailable || videoTokenGate.queryFetching
  }
  const primaryActionBusy =
    createMutation.isPending ||
    videoTokenGate.checking ||
    (videoTokenGate.gateAction === 'recheck' && videoTokenGate.queryFetching) ||
    (videoTokenReady && quoteQuery.isFetching)
  let primaryActionIcon = <Sparkles aria-hidden='true' />
  if (!videoTokenReady) primaryActionIcon = <KeyRound aria-hidden='true' />
  if (primaryActionBusy) {
    primaryActionIcon = (
      <LoaderCircle
        className='animate-spin motion-reduce:animate-none'
        aria-hidden='true'
      />
    )
  }

  return (
    <Form {...form}>
      <form
        className='flex min-h-0 flex-1 flex-col'
        onSubmit={handleSubmit}
        noValidate
      >
        <div className='min-h-0 flex-1 space-y-5 overflow-y-auto px-4 py-4 sm:px-5'>
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
                      handleModelChange(Number(event.target.value))
                    }
                  >
                    {modelsQuery.data?.map((profile) => (
                      <NativeSelectOption key={profile.id} value={profile.id}>
                        {profile.display_name}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          {selectedProfile &&
            selectedProfile.specification.modes.length > 1 && (
              <FormField
                control={form.control}
                name='mode'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('videoStudio.mode')}</FormLabel>
                    <FormControl>
                      <ToggleGroup
                        value={[field.value]}
                        onValueChange={(next) => {
                          const mode = next.at(0) as
                            | VideoGenerationMode
                            | undefined
                          if (mode) handleModeChange(mode)
                        }}
                        variant='outline'
                        className='grid w-full auto-cols-fr grid-flow-col'
                      >
                        {VIDEO_GENERATION_MODES.filter((mode) =>
                          selectedProfile.specification.modes.includes(mode)
                        ).map((mode) => (
                          <ToggleGroupItem
                            key={mode}
                            value={mode}
                            className='min-w-0 px-2'
                          >
                            <span className='truncate'>
                              {t(VIDEO_MODE_LABEL_KEYS[mode])}
                            </span>
                          </ToggleGroupItem>
                        ))}
                      </ToggleGroup>
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

          {referenceLimit > 0 && (
            <div className='space-y-2'>
              <span className='text-sm font-medium'>
                {values.mode === 'first_last_frame'
                  ? t('videoStudio.firstLastFrames')
                  : t('videoStudio.referenceImage')}
              </span>
              {referenceHydrationIds.length > 0 ? (
                <div
                  className='border-border bg-muted/20 flex min-h-20 items-center justify-center gap-2 rounded-lg border px-3 py-4 text-xs'
                  role={referenceHydrationFailed ? 'alert' : 'status'}
                >
                  {referenceHydrationFailed ? (
                    <div className='flex w-full flex-col gap-3 sm:flex-row sm:items-center'>
                      <span className='text-destructive min-w-0 flex-1'>
                        {t('videoStudio.referenceRestoreFailed')}
                      </span>
                      <span className='flex flex-wrap items-center gap-2'>
                        <Button
                          type='button'
                          size='sm'
                          variant='outline'
                          disabled={referenceHydrationPending}
                          onClick={retryReferenceHydration}
                        >
                          {referenceHydrationPending && (
                            <LoaderCircle
                              className='animate-spin motion-reduce:animate-none'
                              aria-hidden='true'
                            />
                          )}
                          {t('videoStudio.retry')}
                        </Button>
                        {referenceHydrationRecovery && (
                          <Button
                            type='button'
                            size='sm'
                            variant='outline'
                            disabled={referenceHydrationPending}
                            onClick={removeUnavailableReferences}
                          >
                            {t('videoStudio.removeUnavailableReferences')}
                          </Button>
                        )}
                        <Button
                          type='button'
                          size='sm'
                          variant='destructive'
                          onClick={clearRestoredDraft}
                        >
                          {t('videoStudio.clearDraft')}
                        </Button>
                      </span>
                    </div>
                  ) : (
                    <>
                      <LoaderCircle
                        className='text-muted-foreground size-4 animate-spin motion-reduce:animate-none'
                        aria-hidden='true'
                      />
                      <span className='text-muted-foreground'>
                        {t('videoStudio.restoringReferences')}
                      </span>
                    </>
                  )}
                </div>
              ) : (
                <VideoAssetUploader
                  key={`${values.mode}-${referenceLimit}`}
                  assets={referenceAssets}
                  onAssetsChange={setReferenceAssets}
                  purpose='reference'
                  maxFiles={referenceLimit}
                  accept={['image/jpeg', 'image/png', 'image/webp']}
                  label={t('videoStudio.addImage')}
                  assetLabels={referenceLabels}
                  inputRef={referenceUploadInputRef}
                />
              )}
            </div>
          )}

          <FormField
            control={form.control}
            name='prompt'
            render={({ field }) => (
              <FormItem>
                <div className='flex items-center justify-between gap-2'>
                  <FormLabel>{t('videoStudio.prompt')}</FormLabel>
                  <span className='text-muted-foreground text-xs tabular-nums'>
                    {field.value.length}/{VIDEO_PROMPT_MAX_LENGTH}
                  </span>
                </div>
                <FormControl>
                  <Textarea
                    {...field}
                    rows={7}
                    maxLength={VIDEO_PROMPT_MAX_LENGTH}
                    className='min-h-36 resize-y'
                    placeholder={t('videoStudio.promptPlaceholder')}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          {selectedProfile &&
            selectedProfile.specification.parameters.length > 0 && (
              <Collapsible defaultOpen>
                <CollapsibleTrigger className='group flex w-full items-center justify-between py-1 text-sm font-medium'>
                  {t('videoStudio.parameters')}
                  <ChevronDown
                    className='size-4 transition-transform group-data-[panel-open]:rotate-180 motion-reduce:transition-none'
                    aria-hidden='true'
                  />
                </CollapsibleTrigger>
                <CollapsibleContent className='pt-4'>
                  <VideoParameterFields profile={selectedProfile} />
                </CollapsibleContent>
              </Collapsible>
            )}
        </div>

        <div className='bg-background shrink-0 border-t p-4 sm:px-5'>
          {submitError && (
            <p className='text-destructive mb-2 text-xs' role='alert'>
              {submitError}
            </p>
          )}
          <Button
            type={videoTokenReady ? 'submit' : 'button'}
            size='lg'
            className='w-full'
            disabled={primaryActionDisabled}
            onClick={
              videoTokenReady ? undefined : handleVideoTokenPrimaryAction
            }
          >
            {primaryActionIcon}
            {generateLabel}
          </Button>
          <div
            className='text-muted-foreground mt-2 min-h-4 text-center text-xs'
            aria-live='polite'
          >
            {quoteStatusMessage}
          </div>
        </div>
      </form>

      <VideoTokenSetupDialog
        open={videoTokenGate.dialogOpen}
        requiredGroup={videoTokenGate.requiredGroup}
        creating={videoTokenGate.creating}
        errorMessage={videoTokenGate.createError}
        onOpenChange={videoTokenGate.handleDialogOpenChange}
        onConfirm={() => void videoTokenGate.createAndContinue()}
      />
    </Form>
  )
}
