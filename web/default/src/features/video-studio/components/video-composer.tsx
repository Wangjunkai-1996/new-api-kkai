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
import { Layers, LoaderCircle, Sparkles } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useForm, useWatch, type Resolver } from 'react-hook-form'
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
import { useDebounce } from '@/hooks/use-debounce'
import { formatQuota } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'
import { useVideoStudioDraftStore } from '@/stores/video-studio-draft-store'
import { useVideoStudioSubmissionStore } from '@/stores/video-studio-submission-store'

import { useVideoSubmission } from '../hooks/use-video-submission'
import type { VideoTokenGateState } from '../hooks/use-video-token-gate'
import { useVideoQuote, useVideoReferenceAssetHydration } from '../queries'
import {
  VIDEO_PROMPT_MAX_LENGTH,
  validateComposerForProfile,
  videoComposerSchema,
} from '../schemas'
import type {
  VideoComposerReference,
  VideoComposerValues,
  VideoGeneration,
  VideoModelProfile,
  VideoSample,
  VideoStudioApiError,
  VideoSubmissionReceipt,
} from '../types'
import {
  buildClearedVideoComposerValues,
  buildVideoQuoteRequest,
  getVideoComposerMode,
  getVideoQuoteRefreshDelay,
  getVideoSubmissionRequestKey,
  getVideoParametersForMode,
  restoreVideoComposerDraft,
  videoQuoteHasExpired,
  videoPromptByteLength,
} from '../video-domain'
import { validateVideoReferenceMedia } from '../video-reference-validation'
import { getVideoTokenErrorKind } from '../video-token-access'
import { VideoParameterFields } from './video-parameter-fields'
import { VideoReferenceCollection } from './video-reference-collection'

type VideoComposerProps = {
  models: VideoModelProfile[]
  sample?: VideoSample
  sampleSelectionId?: number
  reuseGeneration?: VideoGeneration
  reuseSelectionId?: number
  videoTokenGate: VideoTokenGateState
  onSubmitted?: (receipt: VideoSubmissionReceipt) => void
  onSubmissionUnknown?: (taskId: string) => void
}

export function VideoComposer(props: VideoComposerProps) {
  const { t } = useTranslation()
  const draft = useVideoStudioDraftStore((state) => state.draft)
  const saveDraft = useVideoStudioDraftStore((state) => state.saveDraft)
  const clearDraft = useVideoStudioDraftStore((state) => state.clearDraft)
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const storageError = useVideoStudioSubmissionStore(
    (state) => state.ownerId === userId && state.storageError
  )
  const hydrateSubmissions = useVideoStudioSubmissionStore(
    (state) => state.hydrate
  )
  const [uploadBusy, setUploadBusy] = useState(false)
  const [appliedSampleId, setAppliedSampleId] = useState<number>()
  const [pendingChange, setPendingChange] = useState<{
    apply: () => void
  } | null>(null)
  const [invalidQuoteKey, setInvalidQuoteKey] = useState<string | null>(null)
  const initializedRef = useRef(false)
  const appliedSelectionRef = useRef('')
  const form = useForm<VideoComposerValues>({
    resolver: zodResolver(
      videoComposerSchema
    ) as unknown as Resolver<VideoComposerValues>,
    defaultValues: {
      model_profile_id: 0,
      mode: 'text_to_video',
      prompt: '',
      references: [],
      parameters: {},
    },
  })
  const values = useWatch({ control: form.control }) as VideoComposerValues
  const selectedProfile = props.models.find(
    (profile) => profile.id === values.model_profile_id
  )
  const reuseSource = props.reuseGeneration ?? props.sample
  const reuseProfile = props.models.find(
    (profile) => profile.id === reuseSource?.model_profile_id
  )
  const reuseVersion = props.reuseGeneration
    ? props.reuseGeneration.specification_version
    : props.sample?.model_version
  const reuseUnavailable = Boolean(
    reuseSource &&
    (!reuseProfile ||
      reuseVersion !== reuseProfile.specification_version ||
      !Array.isArray(reuseSource.reference_assets) ||
      reuseSource.reference_assets.some(
        (reference) =>
          ![
            'reference_image',
            'reference_video',
            'reference_audio',
            'first_frame',
            'last_frame',
          ].includes(reference.role)
      ))
  )
  const storedReferences = values.references ?? []
  const missingIds = storedReferences
    .filter((reference) => !reference.asset)
    .map((reference) => reference.asset_id)
  const hydrationQueries = useVideoReferenceAssetHydration(missingIds)
  const references = storedReferences.map((reference) => {
    const index = missingIds.indexOf(reference.asset_id)
    return index < 0
      ? reference
      : { ...reference, asset: hydrationQueries[index]?.data ?? null }
  })
  const hydratedValues = { ...values, references }
  const mediaValidation = selectedProfile
    ? validateVideoReferenceMedia(
        references,
        selectedProfile,
        values.parameters
      )
    : null
  const referencesReady =
    !uploadBusy &&
    !mediaValidation &&
    references
      .filter((reference) => reference.included)
      .every((reference) => reference.asset?.state === 'ready')
  const debouncedValues = useDebounce(values, 300)
  const valuesSettled =
    JSON.stringify(values) === JSON.stringify(debouncedValues)
  const gate = props.videoTokenGate

  useEffect(() => {
    if (initializedRef.current) return
    const restored = restoreVideoComposerDraft(props.models, draft)
    if (!restored) return
    initializedRef.current = true
    form.reset(restored)
  }, [draft, form, props.models])
  useEffect(() => {
    hydrateSubmissions(userId)
  }, [hydrateSubmissions, userId])
  useEffect(() => {
    if (values.model_profile_id) saveDraft(values)
  }, [saveDraft, values])

  let quoteRequest = null
  if (gate.tokenId && valuesSettled && referencesReady) {
    const parsed = videoComposerSchema.safeParse(debouncedValues)
    const profile = parsed.success
      ? props.models.find((item) => item.id === parsed.data.model_profile_id)
      : undefined
    if (
      parsed.success &&
      profile &&
      !validateComposerForProfile(parsed.data, profile)
    ) {
      quoteRequest = buildVideoQuoteRequest(
        parsed.data,
        profile,
        gate.tokenId,
        appliedSampleId
      )
    }
  }
  const quoteQuery = useVideoQuote(quoteRequest, Boolean(quoteRequest))
  const quote = quoteQuery.data
  const quoteKey = quote ? `${quote.request_hash}:${quote.expires_at}` : null
  const quoteRequestKey = quoteRequest
    ? getVideoSubmissionRequestKey(quoteRequest)
    : null
  // oxlint-disable-next-line react/purity -- Quote expiry must be rechecked when a background tab resumes.
  const quoteReady = Boolean(
    quote &&
    quoteRequest &&
    quoteKey !== invalidQuoteKey &&
    valuesSettled &&
    referencesReady &&
    // oxlint-disable-next-line react/purity -- Quote expiry must be rechecked when a background tab resumes.
    !videoQuoteHasExpired(quote.expires_at, Math.floor(Date.now() / 1000))
  )
  const submission = useVideoSubmission({
    form,
    selectedProfile,
    videoTokenGate: gate,
    appliedSampleId,
    quote,
    quoteRequest,
    quoteKey,
    quoteRequestKey,
    quoteReady,
    referencesReady,
    refetchQuote: quoteQuery.refetch,
    onQuoteInvalidated: setInvalidQuoteKey,
    onSubmitted: props.onSubmitted,
    onSubmissionUnknown: props.onSubmissionUnknown,
  })
  const setSubmitError = submission.setError

  useEffect(() => {
    const source = props.reuseGeneration ?? props.sample
    if (!source) return
    const key = props.reuseGeneration
      ? `generation:${source.id}:${props.reuseSelectionId ?? 0}`
      : `sample:${source.id}:${props.sampleSelectionId ?? 0}`
    if (appliedSelectionRef.current === key) return
    const profile = props.models.find(
      (item) => item.id === source.model_profile_id
    )
    if (!profile) return
    appliedSelectionRef.current = key
    if (reuseUnavailable) return
    const apply = () => {
      const refs: VideoComposerReference[] = (
        source.reference_assets ?? []
      ).map((reference) => ({
        ...reference,
        included: true,
        client_id: `asset-${reference.asset_id}`,
      }))
      const mode = getVideoComposerMode(refs)
      form.reset({
        model_profile_id: profile.id,
        prompt: source.prompt,
        mode,
        references: refs,
        parameters: { ...source.parameters },
      })
      setAppliedSampleId(props.reuseGeneration ? undefined : props.sample?.id)
      setSubmitError(null)
    }
    const current = form.getValues()
    // oxlint-disable-next-line react/set-state-in-effect -- A new explicit sample selection asks before replacing active edits.
    if (current.prompt || (current.references ?? []).length > 0) {
      // oxlint-disable-next-line react/set-state-in-effect -- Explicit selection arrives from the gallery and asks before replacing user edits.
      setPendingChange({ apply })
    } else {
      apply()
    }
  }, [
    form,
    props.models,
    props.sample,
    props.sampleSelectionId,
    props.reuseGeneration,
    props.reuseSelectionId,
    reuseUnavailable,
    setSubmitError,
  ])

  useEffect(() => {
    if (!quote || !quoteKey) return
    const delay = getVideoQuoteRefreshDelay(quote.expires_at, Date.now())
    if (!delay) {
      // oxlint-disable-next-line react/set-state-in-effect -- Mark the expired quote so it cannot enable submit.
      setInvalidQuoteKey(quoteKey)
      return
    }
    const refetchQuote = quoteQuery.refetch
    const timer = window.setTimeout(() => {
      setInvalidQuoteKey(quoteKey)
      void refetchQuote()
    }, delay)
    return () => window.clearTimeout(timer)
  }, [quote, quoteKey, quoteQuery.refetch])
  useEffect(() => {
    if (!quoteQuery.isError) return
    const error =
      quoteQuery.error instanceof AxiosError
        ? (quoteQuery.error.response?.data as VideoStudioApiError)
        : undefined
    const blockAndRecheck = gate.blockAndRecheck
    blockAndRecheck(getVideoTokenErrorKind(error?.code))
  }, [gate, quoteQuery.error, quoteQuery.isError])

  const changeReferences = (next: VideoComposerReference[]) => {
    const mode = getVideoComposerMode(next)
    form.setValue('references', next, {
      shouldDirty: true,
      shouldValidate: true,
    })
    form.setValue('mode', mode, { shouldValidate: true })
  }
  const changeModel = (id: number) => {
    const profile = props.models.find((item) => item.id === id)
    if (!profile) return
    const current = form.getValues()
    const parameters = getVideoParametersForMode(
      profile,
      current.mode,
      current.parameters
    )
    form.reset({ ...current, model_profile_id: id, parameters })
    setAppliedSampleId(undefined)
    setSubmitError(null)
  }
  const clear = () => {
    if (!selectedProfile) return
    clearDraft()
    form.reset(buildClearedVideoComposerValues(selectedProfile))
    setAppliedSampleId(undefined)
    setSubmitError(null)
  }
  const parsed = videoComposerSchema.safeParse(hydratedValues)
  let validation: string | null | undefined
  if (!parsed.success) {
    validation = parsed.error.issues[0]?.message
  } else if (selectedProfile) {
    validation = validateComposerForProfile(parsed.data, selectedProfile)
  } else {
    validation = 'videoStudio.validation.modelRequired'
  }
  validation = mediaValidation ?? validation
  let status = quoteReady
    ? t('videoStudio.quoteReady')
    : t(validation ?? 'Describe your scene to get a quote.')
  if (uploadBusy) {
    status = t('Wait until every reference is ready before generating.')
  }
  if (quoteQuery.isFetching) status = t('videoStudio.quoting')
  if (quoteQuery.isError && quoteRequest) {
    status = t('videoStudio.quoteUnavailable')
  }
  if (quote && !quoteReady && quoteRequest && !quoteQuery.isFetching) {
    status = t('videoStudio.quoteUnavailable')
  }
  if (storageError) {
    status = t(
      'Recovery storage is unavailable. Generation is paused to prevent duplicate charges.'
    )
  }
  const busy = submission.isPreparing || uploadBusy

  return (
    <Form {...form}>
      <form
        className='video-studio-composer flex min-h-0 flex-1 flex-col'
        onSubmit={form.handleSubmit(submission.submit)}
        noValidate
      >
        <div className='video-studio-form-body min-h-0 flex-1 space-y-5'>
          <div className='video-studio-composer-heading'>
            <h2>{t('videoStudio.createVideo')}</h2>
            <Button
              type='button'
              variant='ghost'
              size='sm'
              onClick={() => setPendingChange({ apply: clear })}
              disabled={busy}
            >
              {t('videoStudio.clearDraft')}
            </Button>
          </div>
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
                      disabled={busy}
                      onChange={(event) =>
                        changeModel(Number(event.target.value))
                      }
                    >
                      {!selectedProfile && (
                        <NativeSelectOption value={field.value}>
                          {t('videoStudio.modelUnavailable')}
                        </NativeSelectOption>
                      )}
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
          {selectedProfile && (
            <VideoReferenceCollection
              references={references}
              onChange={changeReferences}
              profile={selectedProfile}
              disabled={submission.isPreparing}
              onBusyChange={setUploadBusy}
            />
          )}
          {reuseUnavailable && (
            <p className='text-destructive text-xs' role='alert'>
              {t(
                'This item cannot be reused with the current video model settings.'
              )}
            </p>
          )}
          {hydrationQueries.some((query) => query.isError) && (
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() =>
                void Promise.all(
                  hydrationQueries.map((query) => query.refetch())
                )
              }
            >
              {t('videoStudio.retry')}
            </Button>
          )}
          {mediaValidation && (
            <p className='text-destructive text-xs' role='alert'>
              {t(mediaValidation)}
            </p>
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
          {selectedProfile &&
            selectedProfile.specification.parameters.length > 0 && (
              <section className='video-studio-output-section'>
                <h2 className='mb-3 text-sm font-medium'>
                  {t('videoStudio.outputSettings')}
                </h2>
                <VideoParameterFields profile={selectedProfile} />
              </section>
            )}
        </div>
        <div className='video-studio-form-footer shrink-0'>
          <div className='video-studio-price'>
            <span>{t('Estimated cost')}</span>
            <strong>
              {quoteReady && quote
                ? quote.display_amount || formatQuota(quote.quota)
                : '—'}
            </strong>
          </div>
          {submission.error && (
            <p className='text-destructive mb-2 text-xs' role='alert'>
              {submission.error}
            </p>
          )}
          {quoteRequest &&
            (quoteQuery.isError || (quote !== undefined && !quoteReady)) && (
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
            disabled={
              busy || storageError || !gate.effectiveGroup || !quoteReady
            }
            aria-describedby='video-studio-quote-status'
          >
            {submission.isPreparing || quoteQuery.isFetching ? (
              <LoaderCircle
                className='animate-spin motion-reduce:animate-none'
                aria-hidden='true'
              />
            ) : (
              <Sparkles aria-hidden='true' />
            )}
            {t(
              submission.isPreparing ? 'Submitting...' : 'videoStudio.generate'
            )}
          </Button>
          <div
            className='text-muted-foreground mt-2 min-h-4 text-center text-xs'
            id='video-studio-quote-status'
            aria-live='polite'
          >
            {status}
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
