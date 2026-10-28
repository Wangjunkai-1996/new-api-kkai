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
import { AxiosError } from 'axios'
import { Film, Image, Layers, LoaderCircle, Sparkles } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
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
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useDebounce } from '@/hooks/use-debounce'
import { formatQuota } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'
import { useVideoStudioDraftStore } from '@/stores/video-studio-draft-store'
import { useVideoStudioSubmissionStore } from '@/stores/video-studio-submission-store'

import { useVideoSubmission } from '../hooks/use-video-submission'
import type { VideoTokenGateState } from '../hooks/use-video-token-gate'
import { useVideoQuote, useVideoReferenceAssetHydration } from '../queries'
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
  VideoModelProfile,
  VideoSample,
  VideoStudioApiError,
  VideoSubmissionReceipt,
} from '../types'
import {
  buildClearedVideoComposerValues,
  buildVideoComposerValues,
  buildVideoQuoteRequest,
  getSampleReferenceAssets,
  getVideoQuoteRefreshDelay,
  getVideoSubmissionRequestKey,
  getVideoParametersForMode,
  getVideoReferenceRoles,
  restoreVideoComposerDraft,
  VIDEO_MODE_LABEL_KEYS,
  VIDEO_REFERENCE_ROLE_LABEL_KEYS,
  videoQuoteHasExpired,
  videoPromptByteLength,
} from '../video-domain'
import {
  getHydratedVideoReferences,
  getVideoReferenceHydrationRecovery,
} from '../video-reference-hydration'
import { getVideoTokenErrorKind } from '../video-token-access'
import { VideoAssetUploader } from './video-asset-uploader'
import { VideoParameterFields } from './video-parameter-fields'

type VideoComposerProps = {
  models: VideoModelProfile[]
  sample?: VideoSample
  sampleSelectionId?: number
  videoTokenGate: VideoTokenGateState
  onSubmitted?: (receipt: VideoSubmissionReceipt) => void
  onSubmissionUnknown?: (taskId: string) => void
}

const getVideoStudioResponseError = (
  error: unknown
): VideoStudioApiError | undefined =>
  error instanceof AxiosError
    ? (error.response?.data as VideoStudioApiError | undefined)
    : undefined

export function VideoComposer(props: VideoComposerProps) {
  const { t } = useTranslation()
  const draft = useVideoStudioDraftStore((state) => state.draft)
  const saveDraft = useVideoStudioDraftStore((state) => state.saveDraft)
  const clearDraft = useVideoStudioDraftStore((state) => state.clearDraft)
  const [referenceAssets, setReferenceAssets] = useState<VideoAsset[]>([])
  const [referenceHydrationIds, setReferenceHydrationIds] = useState<number[]>(
    []
  )
  const [appliedSampleId, setAppliedSampleId] = useState<number | undefined>()
  const [pendingChange, setPendingChange] = useState<{
    apply: () => void
  } | null>(null)

  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const storageError = useVideoStudioSubmissionStore(
    (state) => state.ownerId === userId && state.storageError
  )
  const hydrateSubmissions = useVideoStudioSubmissionStore(
    (state) => state.hydrate
  )
  const appliedSampleRef = useRef<string | undefined>(undefined)
  const [invalidQuoteKey, setInvalidQuoteKey] = useState<string | null>(null)
  const referenceUploadInputRef = useRef<HTMLInputElement>(null)
  const initializedRef = useRef(false)
  const referenceSyncBlockedRef = useRef(false)
  const referenceHydrationQueries = useVideoReferenceAssetHydration(
    referenceHydrationIds
  )

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
  const selectedProfile = props.models.find(
    (profile) => profile.id === values.model_profile_id
  )
  const videoTokenGate = props.videoTokenGate
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
    if (initializedRef.current) return
    const restored = restoreVideoComposerDraft(props.models, draft)
    if (!restored) return
    initializedRef.current = true
    referenceSyncBlockedRef.current = restored.reference_asset_ids.length > 0
    // oxlint-disable-next-line react/set-state-in-effect -- Restore reference hydration alongside the persisted form draft.
    setReferenceHydrationIds(restored.reference_asset_ids)
    form.reset(restored)
  }, [draft, form, props.models])

  useEffect(() => {
    hydrateSubmissions(userId)
  }, [hydrateSubmissions, userId])

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
    // oxlint-disable-next-line react/set-state-in-effect -- Apply reference assets returned by the hydration queries.
    setReferenceAssets(hydratedReferences)
    referenceSyncBlockedRef.current = false
    setReferenceHydrationIds([])
  }, [referenceHydrationIds, referenceHydrationQueries])

  const composerValuesAreDebounced =
    JSON.stringify(values) === JSON.stringify(debouncedValues)
  const quoteRequest = useMemo(() => {
    if (!videoTokenId || !composerValuesAreDebounced) return null
    const parsed = videoComposerSchema.safeParse(debouncedValues)
    if (!parsed.success) return null
    const profile = props.models.find(
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
  }, [
    appliedSampleId,
    composerValuesAreDebounced,
    debouncedValues,
    props.models,
    videoTokenId,
  ])

  const quoteQuery = useVideoQuote(quoteRequest, quoteRequest !== null)
  const refetchQuote = quoteQuery.refetch
  const quote = quoteQuery.data
  const quoteKey = quote
    ? `${quote.request_hash}:${String(quote.expires_at)}`
    : null
  const quoteRequestKey = quoteRequest
    ? getVideoSubmissionRequestKey(quoteRequest)
    : null
  const quoteReady =
    quote !== undefined &&
    quoteKey !== invalidQuoteKey &&
    composerValuesAreDebounced &&
    // oxlint-disable-next-line react/purity -- Recheck expiry on render in case the refresh timer was suspended in a background tab.
    !videoQuoteHasExpired(quote.expires_at, Math.floor(Date.now() / 1000))
  const quoteDisplay =
    quote?.display_amount || (quote ? formatQuota(quote.quota) : '')

  const submission = useVideoSubmission({
    form,
    selectedProfile,
    videoTokenGate,
    appliedSampleId,
    quote,
    quoteRequest,
    quoteKey,
    quoteRequestKey,
    quoteReady,
    referencesReady:
      referenceHydrationIds.length === 0 &&
      referenceAssets.every((asset) => asset.state === 'ready'),
    refetchQuote,
    onQuoteInvalidated: setInvalidQuoteKey,
    onSubmitted: props.onSubmitted,
    onSubmissionUnknown: props.onSubmissionUnknown,
  })
  const submitError = submission.error
  const setSubmitError = submission.setError
  const preparingSubmission = submission.isPreparing

  useEffect(() => {
    const sample = props.sample
    if (!sample) return
    const selectionId = `${sample.id}:${props.sampleSelectionId ?? 0}`
    if (appliedSampleRef.current === selectionId) return
    const profile = props.models.find(
      (item) => item.id === sample.model_profile_id
    )
    if (!profile) return
    appliedSampleRef.current = selectionId
    const apply = () => {
      referenceSyncBlockedRef.current = false
      setReferenceHydrationIds([])
      form.reset(buildVideoComposerValues(profile, sample))
      setReferenceAssets(getSampleReferenceAssets(sample, profile))
      setAppliedSampleId(sample.id)
      setSubmitError(null)
    }
    const current = form.getValues()
    // A catalog refetch must never apply the same sample over user edits.
    // oxlint-disable-next-line react/set-state-in-effect -- A new explicit sample selection opens a replacement confirmation.
    if (current.prompt || current.reference_asset_ids.length > 0) {
      setPendingChange({ apply })
    } else apply()
  }, [
    form,
    props.models,
    props.sample,
    props.sampleSelectionId,
    setSubmitError,
  ])

  useEffect(() => {
    if (!quote || !quoteKey) return

    // oxlint-disable-next-line react/set-state-in-effect -- Clear the expired quote marker when the query returns a replacement.
    setInvalidQuoteKey((current) =>
      current !== null && current !== quoteKey ? null : current
    )
    const expireQuote = () => {
      setInvalidQuoteKey(quoteKey)
      void refetchQuote()
    }
    const delay = getVideoQuoteRefreshDelay(quote.expires_at, Date.now())
    if (delay === 0) {
      // An expired response needs an explicit refresh; never loop on stale replies.
      setInvalidQuoteKey(quoteKey)
      return
    }
    const timeout = window.setTimeout(expireQuote, delay)
    return () => window.clearTimeout(timeout)
  }, [quote, quoteKey, refetchQuote])

  useEffect(() => {
    if (!quoteQuery.isError) return
    const responseError = getVideoStudioResponseError(quoteQuery.error)
    const errorKind = getVideoTokenErrorKind(responseError?.code)
    blockAndRecheckVideoToken(errorKind)
  }, [blockAndRecheckVideoToken, quoteQuery.error, quoteQuery.isError])

  const applyModelChange = (profileId: number) => {
    const profile = props.models.find((item) => item.id === profileId)
    if (!profile) return
    const nextValues = buildVideoComposerValues(profile)
    nextValues.prompt = form.getValues('prompt')
    referenceSyncBlockedRef.current = false
    setReferenceHydrationIds([])
    form.reset(nextValues)
    setReferenceAssets([])
    setAppliedSampleId(undefined)
    setSubmitError(null)
  }

  const applyModeChange = (mode: VideoGenerationMode) => {
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

  const handleModelChange = (profileId: number) => {
    if (profileId === form.getValues('model_profile_id')) return
    if (
      form.getValues('reference_asset_ids').length > 0 ||
      referenceAssets.length > 0 ||
      Object.keys(form.getValues('parameters')).length > 0
    ) {
      setPendingChange({ apply: () => applyModelChange(profileId) })
    } else applyModelChange(profileId)
  }

  const handleModeChange = (mode: VideoGenerationMode) => {
    if (mode === form.getValues('mode')) return
    const currentParameters = form.getValues('parameters')
    const nextParameters = selectedProfile
      ? getVideoParametersForMode(selectedProfile, mode, currentParameters)
      : currentParameters
    if (
      form.getValues('reference_asset_ids').length > 0 ||
      referenceAssets.length > 0 ||
      Object.keys(currentParameters).some(
        (key) => nextParameters[key] !== currentParameters[key]
      )
    ) {
      setPendingChange({ apply: () => applyModeChange(mode) })
    } else applyModeChange(mode)
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
    setInvalidQuoteKey(null)
    setSubmitError(null)
    clearDraft()
    form.reset(clearedValues)
    focusReferenceUploader()
  }

  const handleSubmit = form.handleSubmit(submission.submit)

  const referenceRoles = selectedProfile
    ? getVideoReferenceRoles(selectedProfile, values.mode)
    : []
  const referenceLimit = referenceRoles.length
  const referenceLabels = referenceRoles.map((role) =>
    t(VIDEO_REFERENCE_ROLE_LABEL_KEYS[role])
  )
  const usesVideoReference = referenceRoles.includes('reference_video')
  let generateLabel = t('videoStudio.generate')
  if (quoteQuery.isFetching) generateLabel = t('videoStudio.quoting')
  if (preparingSubmission) {
    generateLabel = t('Submitting...')
  }
  let quoteStatusMessage = quoteReady
    ? t('videoStudio.quoteReady')
    : t('Describe your scene to get a quote.')
  const validation = videoComposerSchema.safeParse(values)
  if (!validation.success) {
    const message = validation.error.issues[0]?.message
    if (message) quoteStatusMessage = t(message)
  } else if (selectedProfile) {
    const message = validateComposerForProfile(validation.data, selectedProfile)
    if (message) quoteStatusMessage = t(message)
  }
  if (
    referenceHydrationIds.length > 0 ||
    referenceAssets.some((asset) => asset.state !== 'ready')
  ) {
    quoteStatusMessage = t(
      'Wait until every reference is ready before generating.'
    )
  }
  if (quoteRequest && !quoteReady) {
    quoteStatusMessage = quoteQuery.isFetching
      ? t('videoStudio.quoting')
      : t('videoStudio.quoteUnavailable')
  }
  const quoteNeedsRefresh = Boolean(
    quoteRequest && (quoteQuery.isError || (quote !== undefined && !quoteReady))
  )
  if (!videoTokenGate.effectiveGroup) {
    quoteStatusMessage = t(
      'Your video access is being verified. Please try again shortly.'
    )
  }
  if (storageError) {
    quoteStatusMessage = t(
      'Recovery storage is unavailable. Generation is paused to prevent duplicate charges.'
    )
  }
  const primaryActionDisabled =
    preparingSubmission ||
    storageError ||
    !videoTokenGate.effectiveGroup ||
    !quoteReady ||
    referenceHydrationIds.length > 0 ||
    referenceAssets.some((asset) => asset.state !== 'ready')
  const primaryActionBusy = preparingSubmission || quoteQuery.isFetching
  let primaryActionIcon = <Sparkles aria-hidden='true' />
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
        className='video-studio-composer flex min-h-0 flex-1 flex-col'
        onSubmit={handleSubmit}
        noValidate
      >
        <div className='video-studio-form-body min-h-0 flex-1 space-y-5'>
          <FormField
            control={form.control}
            name='model_profile_id'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('videoStudio.model')}</FormLabel>
                <div className='video-studio-model'>
                  <Layers
                    className='video-studio-model-icon'
                    aria-hidden='true'
                  />
                  <FormControl>
                    <NativeSelect
                      className='w-full'
                      value={field.value}
                      onChange={(event) =>
                        handleModelChange(Number(event.target.value))
                      }
                    >
                      {props.models.map((profile) => (
                        <NativeSelectOption key={profile.id} value={profile.id}>
                          {profile.display_name}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </FormControl>
                  {selectedProfile?.provider_label && (
                    <span className='video-studio-model-provider'>
                      {selectedProfile.provider_label}
                    </span>
                  )}
                </div>
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
                        className='video-studio-modes grid w-full auto-cols-fr grid-flow-col'
                      >
                        {VIDEO_GENERATION_MODES.filter((mode) =>
                          selectedProfile.specification.modes.includes(mode)
                        ).map((mode) => (
                          <ToggleGroupItem
                            key={mode}
                            value={mode}
                            className='min-w-0 px-2'
                          >
                            {mode === 'text_to_video' && (
                              <Film aria-hidden='true' />
                            )}
                            {mode === 'image_to_video' && (
                              <Image aria-hidden='true' />
                            )}
                            {mode === 'first_last_frame' && (
                              <Layers aria-hidden='true' />
                            )}
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

          {selectedProfile?.specification.modes.length === 1 && (
            <div className='video-studio-mode-single'>
              <span>{t('videoStudio.mode')}</span>
              <span>
                <Film aria-hidden='true' />
                {t(
                  VIDEO_MODE_LABEL_KEYS[
                    selectedProfile.specification.modes[0] ?? values.mode
                  ]
                )}
              </span>
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
                    {t('{{count}} / {{limit}} bytes', {
                      count: videoPromptByteLength(field.value),
                      limit: VIDEO_PROMPT_MAX_LENGTH,
                    })}
                  </span>
                </div>
                <FormControl>
                  <Textarea
                    {...field}
                    rows={4}
                    className='video-studio-prompt resize-y'
                    placeholder={t('videoStudio.promptPlaceholder')}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          {referenceLimit > 0 && (
            <div className='space-y-2'>
              <span className='text-sm font-medium'>
                {values.mode === 'first_last_frame'
                  ? t('videoStudio.firstLastFrames')
                  : t(
                      usesVideoReference
                        ? 'videoStudio.referenceVideo'
                        : 'videoStudio.referenceImage'
                    )}
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
                  key={`${values.mode}-${referenceLimit}-${usesVideoReference ? 'video' : 'image'}`}
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
                  inputRef={referenceUploadInputRef}
                />
              )}
            </div>
          )}

          {selectedProfile &&
            selectedProfile.specification.parameters.length > 0 && (
              <VideoParameterFields profile={selectedProfile} />
            )}
        </div>

        <div className='video-studio-form-footer shrink-0'>
          <div className='video-studio-price'>
            <span>{t('Estimated cost')}</span>
            <strong>{quoteReady ? quoteDisplay : '—'}</strong>
          </div>
          {submitError && (
            <p className='text-destructive mb-2 text-xs' role='alert'>
              {submitError}
            </p>
          )}
          {quoteNeedsRefresh && (
            <Button
              type='button'
              variant='outline'
              className='mb-2 w-full'
              disabled={quoteQuery.isFetching}
              onClick={() => void quoteQuery.refetch()}
            >
              {t('Refresh quote')}
            </Button>
          )}
          <Button
            type='submit'
            size='lg'
            className='w-full'
            disabled={primaryActionDisabled}
            aria-describedby='video-studio-quote-status'
          >
            {primaryActionIcon}
            {generateLabel}
          </Button>
          <div
            className='text-muted-foreground mt-2 min-h-4 text-center text-xs'
            id='video-studio-quote-status'
            aria-live='polite'
          >
            {quoteStatusMessage}
          </div>
        </div>
      </form>
      <AlertDialog
        open={pendingChange !== null}
        onOpenChange={(open) => {
          if (!open) setPendingChange(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Replace current settings?')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This change may replace your prompt, parameters, or reference media. Your pending submissions remain protected.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel autoFocus>{t('Keep editing')}</AlertDialogCancel>
            <Button
              type='button'
              onClick={() => {
                pendingChange?.apply()
                setPendingChange(null)
              }}
            >
              {t('Replace settings')}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Form>
  )
}
