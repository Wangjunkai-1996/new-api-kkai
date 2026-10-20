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
import { Images, LoaderCircle, Pencil, Sparkles, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useAuthStore } from '@/stores/auth-store'
import {
  clearImageStudioSubmissionKey,
  getOrCreateImageStudioSubmissionKey,
  listPendingImageStudioSubmissions,
  markImageStudioSubmissionStarted,
  useImageStudioDraftStore,
} from '@/stores/image-studio-draft-store'

import { lookupImageSubmission } from '../api'
import { useImageComposerSchema } from '../hooks/use-image-composer-schema'
import { useImageDirectReferences } from '../hooks/use-image-direct-references'
import { useImageEditReferences } from '../hooks/use-image-edit-references'
import type { ImageTokenGateState } from '../hooks/use-image-token-gate'
import {
  buildCreateImageEditRequest,
  buildCreateImageRequest,
  buildImageComposerValues,
  imageRequestFingerprint,
  imageSubmissionFingerprint,
  resolveImageGenerationStatus,
} from '../image-domain'
import {
  buildImageEditQuoteRequest,
  isImageEditQuoteRequest,
  isImageQuoteStaleResponse,
} from '../image-edit-domain'
import {
  getImageOutputCount,
  getImageOutputParameter,
  getImageSizeParameter,
  parseImageParameters,
  validateImageParameterValue,
} from '../image-parameters'
import { parseImagePromptSize } from '../image-prompt-size'
import { IMAGE_REFERENCE_DEFAULT_MAX_BYTES } from '../image-reference-metadata'
import {
  useCreateAsyncImageGeneration,
  useAsyncImageQuote,
  useCreateImageEdit,
  useCreateImageGeneration,
  useImageEditQuote,
  useImageModels,
  useImageQuote,
} from '../queries'
import { imageComposerSchema } from '../schemas'
import type {
  ImageAsyncQuoteRequest,
  ImageComposerValues,
  ImageEditQuoteRequest,
  ImageGeneration,
  ImageGenerationInput,
  ImageQuoteRequest,
  ImageSample,
  ImageStudioComposerMode,
} from '../types'
import { ImageOutputQuantityField } from './image-output-quantity-field'
import { ImageParameterFields } from './image-parameter-fields'
import { ImageReferenceField } from './image-reference-field'
import { ImageSubmissionRecovery } from './image-submission-recovery'
import { ImageTokenSetupDialog } from './image-token-setup-dialog'

const EMPTY_VALUES: ImageComposerValues = {
  model_profile_id: 0,
  prompt: '',
  parameters: {},
}

function isImageQuoteExpiring(expiresAt: number): boolean {
  return expiresAt <= Math.floor(Date.now() / 1000) + 5
}

function useDebouncedRequest<T>(request: T | null, delay: number): T | null {
  const [debounced, setDebounced] = useState(request)
  const latestRequestRef = useRef(request)
  const fingerprint = imageRequestFingerprint(request)
  useEffect(() => {
    latestRequestRef.current = request
  }, [request])
  useEffect(() => {
    const timer = window.setTimeout(
      () => setDebounced(latestRequestRef.current),
      delay
    )
    return () => window.clearTimeout(timer)
  }, [delay, fingerprint])
  return debounced
}

export function ImageComposer(props: {
  sample?: ImageSample
  reuseGeneration?: ImageGeneration
  reuseInput?: ImageGenerationInput
  persistDraft?: boolean
  tokenGate: ImageTokenGateState
  onSubmitted: (generation: ImageGeneration) => void
}) {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const draftUserId = useImageStudioDraftStore((state) => state.userId)
  const draft = useImageStudioDraftStore((state) => state.draft)
  const hydrateDraft = useImageStudioDraftStore((state) => state.hydrate)
  const saveDraft = useImageStudioDraftStore((state) => state.save)
  const clearDraft = useImageStudioDraftStore((state) => state.clear)
  const persistDraft = props.persistDraft ?? !props.reuseGeneration
  const modelsQuery = useImageModels(props.tokenGate.tokenId)
  const generationMutation = useCreateImageGeneration()
  const asyncMutation = useCreateAsyncImageGeneration()
  const [submissionRevision, setSubmissionRevision] = useState(0)
  const [checkingSubmission, setCheckingSubmission] = useState(false)
  const editMutation = useCreateImageEdit()
  const composerSchema = useImageComposerSchema(modelsQuery.data)
  const form = useForm<ImageComposerValues>({
    resolver: zodResolver(composerSchema),
    defaultValues: EMPTY_VALUES,
    mode: 'onChange',
  })
  const values = useWatch({ control: form.control })
  const [mode, setMode] = useState<ImageStudioComposerMode>(() =>
    props.reuseInput?.mode === 'edit' ? 'edit' : 'generation'
  )
  const [reuseModeConfirmed, setReuseModeConfirmed] = useState(
    props.reuseInput?.mode !== 'unknown'
  )
  const [usingSavedReferences, setUsingSavedReferences] = useState(
    props.reuseInput?.mode === 'edit'
  )
  const [savedReferenceIds, setSavedReferenceIds] = useState(
    () => props.reuseInput?.references.map((reference) => reference.id) ?? []
  )
  const [settingsReviewed, setSettingsReviewed] = useState(false)
  const [staleQuoteToken, setStaleQuoteToken] = useState<string | null>(null)
  const initializedRef = useRef(false)
  const appliedSampleRef = useRef<number | undefined>(undefined)
  const appliedReuseRef = useRef<number | undefined>(undefined)
  const selectedProfile = modelsQuery.data?.find(
    (profile) =>
      profile.id === values.model_profile_id &&
      profile.enabled &&
      (!props.reuseGeneration ||
        settingsReviewed ||
        profile.model === props.reuseGeneration.model)
  )
  const sizeParameter = selectedProfile
    ? getImageSizeParameter(selectedProfile)
    : undefined
  const currentSize = sizeParameter
    ? values.parameters?.[sizeParameter.key]
    : undefined
  const promptSize = useMemo(
    () => parseImagePromptSize(values.prompt ?? ''),
    [values.prompt]
  )
  let promptSizeError: string | undefined
  if (promptSize.kind === 'invalid') {
    promptSizeError = t(
      'Enter the size as width x height, for example: 2048x1536.'
    )
  } else if (promptSize.kind === 'conflict') {
    promptSizeError = t('Use one output size in the prompt.')
  } else if (
    promptSize.kind === 'size' &&
    selectedProfile &&
    (!sizeParameter ||
      validateImageParameterValue(
        sizeParameter,
        promptSize.size,
        selectedProfile.effective_max_outputs,
        selectedProfile.specification.custom_size
      ))
  ) {
    promptSizeError = t(
      'This model does not support the output size in your prompt.'
    )
  }
  const currentSizeValid = Boolean(
    sizeParameter &&
    currentSize &&
    !validateImageParameterValue(
      sizeParameter,
      currentSize,
      selectedProfile?.effective_max_outputs,
      selectedProfile?.specification.custom_size
    )
  )
  const { maxImages: maxReferenceImages, references: reference } =
    useImageEditReferences(selectedProfile, props.tokenGate.capability)
  const asyncEnabled = Boolean(
    props.tokenGate.capability?.async_submission_enabled &&
    selectedProfile &&
    props.tokenGate.capability.async_models?.includes(selectedProfile.model)
  )
  const savedReferences = useMemo(
    () =>
      props.reuseInput?.references.filter((reference) =>
        savedReferenceIds.includes(reference.id)
      ) ?? [],
    [props.reuseInput?.references, savedReferenceIds]
  )
  const savedReferenceMetadata = useMemo(
    () =>
      savedReferences.map((reference) => ({
        sha256: reference.sha256,
        size_bytes: reference.size_bytes,
      })),
    [savedReferences]
  )
  const maxReferenceBytes =
    props.tokenGate.capability?.max_reference_bytes ??
    IMAGE_REFERENCE_DEFAULT_MAX_BYTES
  const maxReferenceTotalBytes =
    props.tokenGate.capability?.max_reference_total_bytes ??
    IMAGE_REFERENCE_DEFAULT_MAX_BYTES
  const savedReferencesUsable = Boolean(
    props.reuseInput?.references_available &&
    asyncEnabled &&
    savedReferences.length > 0 &&
    savedReferences.length <= maxReferenceImages &&
    savedReferences.every(
      (reference) => reference.size_bytes <= maxReferenceBytes
    ) &&
    savedReferences.reduce(
      (total, reference) => total + reference.size_bytes,
      0
    ) <= maxReferenceTotalBytes
  )
  const activeReferenceMetadata = usingSavedReferences
    ? savedReferenceMetadata
    : reference.metadata
  const directReferences = useImageDirectReferences({
    enabled: asyncEnabled && mode === 'edit' && !usingSavedReferences,
    userId,
    files: reference.files,
    metadata: reference.metadata,
  })
  const outputParameter = selectedProfile
    ? getImageOutputParameter(selectedProfile)
    : undefined
  const outputParameterKey = outputParameter?.key
  const currentOutputValue = outputParameterKey
    ? values.parameters?.[outputParameterKey]
    : undefined
  const outputCount = selectedProfile
    ? getImageOutputCount(selectedProfile, values.parameters ?? {})
    : 1
  const reuseSettingsChanged = Boolean(
    props.reuseGeneration &&
    selectedProfile &&
    !settingsReviewed &&
    selectedProfile.specification_version !==
      props.reuseGeneration.specification_version
  )
  const unsupportedParameterKeys = selectedProfile
    ? Object.keys(values.parameters ?? {}).filter(
        (key) =>
          !selectedProfile.specification.parameters.some(
            (parameter) => parameter.key === key
          )
      )
    : []
  const reuseSettingsBlocked =
    reuseSettingsChanged || unsupportedParameterKeys.length > 0

  const submittedRequest = generationMutation.variables?.request
  const submittedProfile = submittedRequest
    ? modelsQuery.data?.find(
        (profile) => profile.model === submittedRequest.model
      )
    : undefined
  const submittedOutputCount =
    submittedProfile && submittedRequest
      ? getImageOutputCount(submittedProfile, submittedRequest.parameters)
      : outputCount

  useEffect(() => {
    if (
      mode !== 'generation' ||
      !outputParameterKey ||
      currentOutputValue === outputCount
    ) {
      return
    }

    form.setValue(`parameters.${outputParameterKey}`, outputCount, {
      shouldDirty: true,
      shouldValidate: true,
    })
  }, [currentOutputValue, form, mode, outputCount, outputParameterKey])

  useEffect(() => {
    if (!persistDraft) return
    hydrateDraft(userId)
    initializedRef.current = false
  }, [hydrateDraft, persistDraft, userId])

  useEffect(() => {
    const models = modelsQuery.data
    if (
      !models ||
      models.length === 0 ||
      props.reuseGeneration ||
      initializedRef.current ||
      (persistDraft && draftUserId !== userId)
    ) {
      return
    }
    const scopedDraft = persistDraft && draftUserId === userId ? draft : null
    const draftProfile = models.find(
      (profile) => profile.id === scopedDraft?.model_profile_id
    )
    const profile = draftProfile ?? models[0]
    form.reset(
      buildImageComposerValues(profile, scopedDraft ?? undefined, mode)
    )
    initializedRef.current = true
  }, [
    draft,
    draftUserId,
    form,
    mode,
    modelsQuery.data,
    persistDraft,
    props.reuseGeneration,
    userId,
  ])

  useEffect(() => {
    const generation = props.reuseGeneration
    if (!generation || !props.reuseInput || !modelsQuery.data) return
    if (appliedReuseRef.current === generation.id) return
    const profile = modelsQuery.data.find(
      (candidate) =>
        candidate.id === generation.model_profile_id &&
        candidate.model === generation.model &&
        candidate.enabled
    )
    const parameters = { ...generation.parameters }
    const outputParameter = profile && getImageOutputParameter(profile)
    if (outputParameter) parameters[outputParameter.key] = 1
    form.reset({
      model_profile_id: generation.model_profile_id,
      prompt: generation.prompt,
      parameters,
    })
    appliedReuseRef.current = generation.id
    initializedRef.current = true
  }, [form, modelsQuery.data, props.reuseGeneration, props.reuseInput])

  useEffect(() => {
    if (!props.sample || !modelsQuery.data) return
    if (appliedSampleRef.current === props.sample.id) return
    const profile = modelsQuery.data.find(
      (candidate) => candidate.id === props.sample?.model_profile_id
    )
    if (!profile) return
    form.reset(
      buildImageComposerValues(
        profile,
        {
          prompt: props.sample.prompt,
          parameters: props.sample.parameters,
          sample_id: props.sample.id,
        },
        mode
      )
    )
    appliedSampleRef.current = props.sample.id
    initializedRef.current = true
  }, [form, mode, modelsQuery.data, props.sample])

  useEffect(() => {
    if (
      !persistDraft ||
      mode !== 'generation' ||
      !initializedRef.current ||
      userId <= 0
    ) {
      return
    }
    if (form.getValues('prompt').trim() === '') {
      clearDraft(userId)
      return
    }
    const parsed = composerSchema.safeParse(values)
    if (!parsed.success) return
    const timer = window.setTimeout(() => saveDraft(userId, parsed.data), 300)
    return () => window.clearTimeout(timer)
  }, [
    clearDraft,
    composerSchema,
    form,
    mode,
    persistDraft,
    saveDraft,
    userId,
    values,
  ])

  const quoteRequest = useMemo<
    ImageQuoteRequest | ImageEditQuoteRequest | ImageAsyncQuoteRequest | null
  >(() => {
    if (
      !props.tokenGate.tokenId ||
      !selectedProfile ||
      promptSizeError ||
      !reuseModeConfirmed ||
      reuseSettingsBlocked
    ) {
      return null
    }
    const parsed = imageComposerSchema.safeParse(values)
    if (!parsed.success) return null
    const parsedParameters = parseImageParameters(
      selectedProfile,
      parsed.data.parameters
    )
    if (!parsedParameters.success) return null
    let requestParameters = parsedParameters.parameters
    if (mode === 'edit' && outputParameterKey) {
      requestParameters = { ...requestParameters, [outputParameterKey]: 1 }
    }
    const request: ImageQuoteRequest = {
      token_id: props.tokenGate.tokenId,
      model: selectedProfile.model,
      prompt: parsed.data.prompt.trim(),
      parameters: requestParameters,
      sample_id: parsed.data.sample_id,
    }
    if (asyncEnabled) {
      if (
        mode === 'edit' &&
        (usingSavedReferences
          ? !savedReferencesUsable
          : !directReferences.ready)
      ) {
        return null
      }
      let assetIds: number[] = []
      if (mode === 'edit') {
        assetIds = usingSavedReferences
          ? savedReferenceIds
          : directReferences.assetIds
      }
      return {
        ...request,
        mode,
        asset_ids: assetIds,
      }
    }
    if (mode === 'generation') return request
    if (usingSavedReferences) return null
    return buildImageEditQuoteRequest(request, reference.metadata)
  }, [
    asyncEnabled,
    directReferences.assetIds,
    directReferences.ready,
    mode,
    outputParameterKey,
    props.tokenGate.tokenId,
    promptSizeError,
    reference.metadata,
    reuseModeConfirmed,
    reuseSettingsBlocked,
    savedReferenceIds,
    savedReferencesUsable,
    selectedProfile,
    usingSavedReferences,
    values,
  ])
  const debouncedRequest = useDebouncedRequest(quoteRequest, 400)
  const debouncedIsEdit =
    debouncedRequest !== null && isImageEditQuoteRequest(debouncedRequest)
  const generationQuoteQuery = useImageQuote(
    !asyncEnabled && mode === 'generation' && !debouncedIsEdit
      ? debouncedRequest
      : null
  )
  const editQuoteQuery = useImageEditQuote(
    !asyncEnabled && mode === 'edit' && debouncedIsEdit
      ? (debouncedRequest as ImageEditQuoteRequest)
      : null
  )
  const asyncQuoteQuery = useAsyncImageQuote(
    asyncEnabled && debouncedRequest && 'asset_ids' in debouncedRequest
      ? debouncedRequest
      : null
  )
  let quoteQuery = mode === 'edit' ? editQuoteQuery : generationQuoteQuery
  if (asyncEnabled) quoteQuery = asyncQuoteQuery
  const quoteMatchesRequest =
    debouncedRequest !== null &&
    quoteRequest !== null &&
    imageRequestFingerprint(debouncedRequest) ===
      imageRequestFingerprint(quoteRequest)
  const currentQuote =
    quoteMatchesRequest && quoteQuery.data?.quote_token !== staleQuoteToken
      ? quoteQuery.data
      : undefined
  const submitPending =
    editMutation.isPending ||
    generationMutation.isPending ||
    asyncMutation.isPending ||
    checkingSubmission ||
    form.formState.isSubmitting
  let quoteDisplay = '—'
  let quoteBusy = false
  if (quoteRequest && (!quoteMatchesRequest || quoteQuery.isFetching)) {
    quoteDisplay = t('imageStudio.pricing')
    quoteBusy = true
  } else if (quoteMatchesRequest && quoteQuery.isError) {
    quoteDisplay = t('imageStudio.quoteFailed')
  } else if (currentQuote) {
    quoteDisplay = currentQuote.display_amount
  }

  const changeModel = (profileId: number): void => {
    const profile = modelsQuery.data?.find(
      (candidate) => candidate.id === profileId
    )
    if (!profile) return
    setSettingsReviewed(true)
    const nextSizeParameter = getImageSizeParameter(profile)
    const parameters =
      nextSizeParameter && currentSize !== undefined
        ? { [nextSizeParameter.key]: currentSize }
        : undefined
    form.reset(
      buildImageComposerValues(
        profile,
        { prompt: form.getValues('prompt'), parameters },
        mode
      )
    )
  }

  const changeMode = (nextMode: ImageStudioComposerMode): void => {
    if (submitPending) return
    setReuseModeConfirmed(true)
    if (nextMode === mode) return
    setMode(nextMode)
    setUsingSavedReferences(false)
    reference.clear()
    if (selectedProfile) {
      if (props.reuseGeneration) {
        const currentValues = form.getValues()
        const parameters = { ...currentValues.parameters }
        if (nextMode === 'edit' && outputParameterKey) {
          parameters[outputParameterKey] = 1
        }
        form.reset({ ...currentValues, parameters })
        return
      }
      form.reset(
        buildImageComposerValues(selectedProfile, form.getValues(), nextMode)
      )
    }
  }

  const submit = form.handleSubmit(async () => {
    if (!quoteRequest || !currentQuote) return
    if (
      mode === 'edit' &&
      ((usingSavedReferences
        ? !savedReferencesUsable
        : reference.files.length === 0) ||
        (!asyncEnabled && !isImageEditQuoteRequest(quoteRequest)))
    ) {
      return
    }
    let quote = currentQuote
    let requestFingerprint: string
    try {
      // Asset IDs can change after upload expiry; image content identifies the user's submission.
      const identityRequest: ImageQuoteRequest = {
        token_id: quoteRequest.token_id,
        model: quoteRequest.model,
        prompt: quoteRequest.prompt,
        parameters: quoteRequest.parameters,
        sample_id: quoteRequest.sample_id,
      }
      const identity =
        mode === 'edit'
          ? buildImageEditQuoteRequest(identityRequest, activeReferenceMetadata)
          : identityRequest
      if (!identity) return
      requestFingerprint = await imageSubmissionFingerprint(identity)
    } catch {
      toast.error(
        mode === 'edit'
          ? t('imageStudio.editSubmitFailed')
          : t('imageStudio.submitFailed')
      )
      return
    }
    const idempotencyKey = getOrCreateImageStudioSubmissionKey(
      userId,
      requestFingerprint
    )
    if (!idempotencyKey) {
      toast.error(
        mode === 'edit'
          ? t('imageStudio.editSubmitFailed')
          : t('imageStudio.submitFailed')
      )
      return
    }
    let sentSubmission = false
    try {
      let generation: ImageGeneration
      const existing = listPendingImageStudioSubmissions(userId).find(
        (receipt) => receipt.idempotencyKey === idempotencyKey
      )
      if (existing) {
        setCheckingSubmission(true)
        const result = await lookupImageSubmission(idempotencyKey)
        if (result.state !== 'found' || !result.generation) {
          toast.info(
            t(
              'Your submission is still being checked. No new image request was sent.'
            )
          )
          return
        }
        generation = result.generation
      } else {
        if (isImageQuoteExpiring(quote.expires_at)) {
          const refreshed = await quoteQuery.refetch()
          if (!refreshed.data) return
          if (
            refreshed.data.quota !== quote.quota ||
            refreshed.data.display_amount !== quote.display_amount
          ) {
            toast.info(t('imageStudio.quoteChanged'))
            return
          }
          quote = refreshed.data
        }
        if (!markImageStudioSubmissionStarted(userId, requestFingerprint)) {
          toast.error(
            t(
              'Unable to save the submission receipt. Allow browser storage and try again.'
            )
          )
          return
        }
        sentSubmission = true
        if (asyncEnabled && 'asset_ids' in quoteRequest) {
          generation = await asyncMutation.mutateAsync({
            request: { ...quoteRequest, quote_token: quote.quote_token },
            idempotencyKey,
          })
        } else if (isImageEditQuoteRequest(quoteRequest)) {
          generation = await editMutation.mutateAsync({
            request: buildCreateImageEditRequest(quoteRequest, quote),
            images: reference.files,
            idempotencyKey,
          })
        } else {
          generation = await generationMutation.mutateAsync({
            request: buildCreateImageRequest(quoteRequest, quote),
            idempotencyKey,
          })
        }
      }
      if (generation.status !== 'unknown') {
        clearImageStudioSubmissionKey(userId, requestFingerprint)
      }
      const resolution = resolveImageGenerationStatus(generation.status)
      if (resolution.outcome === 'failure') {
        toast.error(
          mode === 'edit'
            ? t('imageStudio.editFailed')
            : t('imageStudio.generationFailed')
        )
      } else if (resolution.outcome === 'pending') {
        toast.info(t('imageStudio.submissionProcessing'))
      } else {
        if (persistDraft && mode === 'generation' && resolution.clearDraft) {
          clearDraft(userId)
        }
        toast.success(
          mode === 'edit'
            ? t('imageStudio.editSubmitted')
            : t('imageStudio.submitted')
        )
      }
      props.onSubmitted(generation)
    } catch (error) {
      const status = isAxiosError(error) ? error.response?.status : undefined
      if (
        sentSubmission &&
        status &&
        status >= 400 &&
        status < 500 &&
        status !== 408 &&
        status !== 409
      ) {
        clearImageStudioSubmissionKey(userId, requestFingerprint)
      }
      if (sentSubmission && isImageQuoteStaleResponse(error)) {
        clearImageStudioSubmissionKey(userId, requestFingerprint)
        setStaleQuoteToken(quote.quote_token)
        toast.error(t('imageStudio.quoteChanged'))
        await quoteQuery.refetch()
        return
      }
      let message = t('imageStudio.submitFailed')
      if (mode === 'edit') message = t('imageStudio.editSubmitFailed')
      if (error instanceof Error) message = error.message
      toast.error(message)
    } finally {
      setCheckingSubmission(false)
      setSubmissionRevision((value) => value + 1)
    }
  })

  let gateMessage: string | null = null
  if (props.tokenGate.checkFailed) {
    gateMessage = t('imageStudio.token.checkFailed')
  } else if (props.tokenGate.capability?.status === 'group_unavailable') {
    gateMessage = t('imageStudio.token.groupUnavailable')
  } else if (props.tokenGate.capability?.status === 'limit_reached') {
    gateMessage = t('imageStudio.token.limitReached')
  } else if (props.tokenGate.capability?.status === 'models_unavailable') {
    gateMessage = t('imageStudio.token.modelsUnavailable')
  }

  let submitLabel = t('imageStudio.generateCount', { count: outputCount })
  let submitIcon = <Sparkles aria-hidden='true' />
  if (mode === 'edit') {
    submitLabel = t('imageStudio.edit')
    submitIcon = <Pencil aria-hidden='true' />
  }
  if (submitPending) {
    if (mode === 'edit') {
      submitLabel = t('imageStudio.editing')
    } else {
      submitLabel = t('imageStudio.generatingCount', {
        count: submittedOutputCount,
      })
    }
    submitIcon = (
      <LoaderCircle
        className='animate-spin motion-reduce:animate-none'
        aria-hidden='true'
      />
    )
  }
  if (asyncEnabled && directReferences.processing) {
    submitLabel =
      directReferences.progress.phase === 'validating'
        ? t('Validating reference images...')
        : t('Uploading reference images: {{progress}}%', {
            progress: directReferences.progress.progress,
          })
  } else if (asyncEnabled && submitPending) {
    submitLabel = t('Submitting image task...')
  }
  let displayedQuote = quoteDisplay
  if (mode === 'generation') {
    displayedQuote = t('imageStudio.estimatedPriceForCount', {
      count: outputCount,
      amount: quoteDisplay,
    })
  }

  return (
    <Form {...form}>
      <form
        className='flex min-h-0 flex-1 flex-col'
        onSubmit={submit}
        noValidate
      >
        <div className='min-h-0 flex-1 space-y-5 overflow-y-auto p-4 sm:p-5'>
          <ImageSubmissionRecovery
            key={`${userId}:${submissionRevision}`}
            userId={userId}
            onRecovered={props.onSubmitted}
          />
          {gateMessage && (
            <Alert variant='destructive'>
              <AlertTitle>{t('imageStudio.token.unavailable')}</AlertTitle>
              <AlertDescription>{gateMessage}</AlertDescription>
            </Alert>
          )}
          {!reuseModeConfirmed && (
            <Alert>
              <AlertDescription>
                {t(
                  'Choose a generation mode to continue. The original mode is unavailable for this older task.'
                )}
              </AlertDescription>
            </Alert>
          )}
          {props.reuseGeneration &&
            !modelsQuery.isLoading &&
            !selectedProfile && (
              <Alert variant='destructive'>
                <AlertDescription>
                  {t(
                    'The original model is unavailable. Choose a model to continue.'
                  )}
                </AlertDescription>
              </Alert>
            )}
          {reuseSettingsBlocked && selectedProfile && (
            <Alert variant='destructive'>
              <AlertDescription>
                <p>
                  {t(
                    'The model settings have changed or contain unsupported parameters. Reset to the current defaults before generating.'
                  )}
                </p>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={submitPending}
                  onClick={() => {
                    const defaults = buildImageComposerValues(
                      selectedProfile,
                      {
                        prompt: form.getValues('prompt'),
                      },
                      mode
                    )
                    if (outputParameterKey) {
                      defaults.parameters[outputParameterKey] = 1
                    }
                    form.reset(defaults)
                    setSettingsReviewed(true)
                  }}
                >
                  {t('Reset to current model defaults')}
                </Button>
              </AlertDescription>
            </Alert>
          )}
          <div className='space-y-2'>
            <span className='text-sm font-medium'>{t('imageStudio.mode')}</span>
            <ToggleGroup
              value={reuseModeConfirmed ? [mode] : []}
              onValueChange={(next) => {
                const nextMode = next.at(0) as
                  | ImageStudioComposerMode
                  | undefined
                if (nextMode) changeMode(nextMode)
              }}
              variant='outline'
              className='grid w-full grid-cols-2'
              aria-label={t('imageStudio.mode')}
            >
              <ToggleGroupItem
                value='generation'
                disabled={submitPending}
                className='min-w-0 px-2'
              >
                <Sparkles aria-hidden='true' />
                <span className='truncate'>
                  {t('imageStudio.mode.generation')}
                </span>
              </ToggleGroupItem>
              <ToggleGroupItem
                value='edit'
                disabled={submitPending || modelsQuery.isLoading}
                className='min-w-0 px-2'
              >
                <Images aria-hidden='true' />
                <span className='truncate'>{t('imageStudio.mode.edit')}</span>
              </ToggleGroupItem>
            </ToggleGroup>
          </div>
          {mode === 'edit' && !modelsQuery.isLoading && !selectedProfile && (
            <Alert variant='destructive'>
              <AlertTitle>{t('imageStudio.editUnavailable')}</AlertTitle>
              <AlertDescription>
                {t('imageStudio.token.modelsUnavailable')}
              </AlertDescription>
            </Alert>
          )}
          <FormField
            control={form.control}
            name='model_profile_id'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('imageStudio.model')}</FormLabel>
                <FormControl>
                  <NativeSelect
                    className='w-full'
                    value={field.value > 0 ? String(field.value) : ''}
                    disabled={
                      !props.tokenGate.tokenId ||
                      modelsQuery.isLoading ||
                      submitPending
                    }
                    onChange={(event) =>
                      changeModel(Number(event.target.value))
                    }
                  >
                    <NativeSelectOption value=''>
                      {t('imageStudio.selectModel')}
                    </NativeSelectOption>
                    {props.reuseGeneration &&
                      !selectedProfile &&
                      field.value ===
                        props.reuseGeneration.model_profile_id && (
                        <NativeSelectOption
                          value={String(field.value)}
                          disabled
                        >
                          {props.reuseGeneration.model}
                        </NativeSelectOption>
                      )}
                    {modelsQuery.data
                      ?.filter((profile) => profile.enabled)
                      .map((profile) => (
                        <NativeSelectOption
                          key={profile.id}
                          value={String(profile.id)}
                        >
                          {profile.display_name}
                        </NativeSelectOption>
                      ))}
                  </NativeSelect>
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          {mode === 'edit' && usingSavedReferences && (
            <div className='space-y-3'>
              <p className='text-sm font-medium'>
                {t('Original reference images')}
              </p>
              <ul className='space-y-2'>
                {savedReferences.map((savedReference, index) => (
                  <li
                    key={savedReference.id}
                    className='flex items-center gap-3 rounded-md border p-3'
                  >
                    <Images
                      className='text-muted-foreground size-5 shrink-0'
                      aria-hidden='true'
                    />
                    <span className='min-w-0 flex-1 text-xs'>
                      {t(
                        'Reference {{position}} · {{width}} × {{height}} · {{size}} KB',
                        {
                          position: index + 1,
                          width: savedReference.width,
                          height: savedReference.height,
                          size: Math.ceil(savedReference.size_bytes / 1024),
                        }
                      )}
                    </span>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon-sm'
                      disabled={submitPending}
                      aria-label={t('imageStudio.removeReferenceImage', {
                        position: index + 1,
                      })}
                      onClick={() =>
                        setSavedReferenceIds((ids) =>
                          ids.filter((id) => id !== savedReference.id)
                        )
                      }
                    >
                      <Trash2 aria-hidden='true' />
                    </Button>
                  </li>
                ))}
              </ul>
              <p
                className={
                  savedReferencesUsable
                    ? 'text-muted-foreground text-xs'
                    : 'text-destructive text-xs'
                }
                role={savedReferencesUsable ? undefined : 'alert'}
              >
                {savedReferencesUsable
                  ? t(
                      'Original reference images are reused in their original order.'
                    )
                  : t(
                      'Original references are unavailable or incompatible with the current model. Upload the complete reference set to continue.'
                    )}
              </p>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={submitPending}
                onClick={() => {
                  setUsingSavedReferences(false)
                  setSavedReferenceIds([])
                  reference.clear()
                }}
              >
                {t('Use new reference images')}
              </Button>
            </div>
          )}
          {mode === 'edit' && selectedProfile && !usingSavedReferences && (
            <ImageReferenceField
              controller={reference}
              maxReferenceImages={maxReferenceImages}
              disabled={submitPending || reference.processing}
            />
          )}
          {asyncEnabled &&
            mode === 'edit' &&
            !usingSavedReferences &&
            directReferences.error && (
              <Alert variant='destructive'>
                <AlertDescription>
                  <p>{directReferences.error}</p>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={directReferences.retry}
                  >
                    {t('Retry upload')}
                  </Button>
                </AlertDescription>
              </Alert>
            )}
          <FormField
            control={form.control}
            name='prompt'
            render={({ field, fieldState }) => (
              <FormItem>
                <FormLabel>{t('imageStudio.prompt')}</FormLabel>
                <FormControl
                  aria-invalid={Boolean(promptSizeError || fieldState.error)}
                >
                  <Textarea
                    {...field}
                    rows={7}
                    maxLength={8000}
                    placeholder={t('imageStudio.promptPlaceholder')}
                    onChange={(event) => {
                      const nextSize = parseImagePromptSize(event.target.value)
                      const previousSize = parseImagePromptSize(field.value)
                      field.onChange(event)
                      if (
                        nextSize.kind !== 'size' ||
                        !sizeParameter ||
                        (previousSize.kind === 'size' &&
                          previousSize.size === nextSize.size)
                      ) {
                        return
                      }
                      form.setValue(
                        `parameters.${sizeParameter.key}`,
                        nextSize.size,
                        {
                          shouldDirty: true,
                          shouldValidate: true,
                        }
                      )
                    }}
                  />
                </FormControl>
                <FormDescription aria-live='polite'>
                  {promptSizeError ? (
                    <span className='text-destructive'>{promptSizeError}</span>
                  ) : (
                    t(
                      'Include a size such as 2048x1536 in your prompt to apply it automatically.'
                    )
                  )}
                  {!promptSizeError &&
                    promptSize.kind === 'size' &&
                    currentSizeValid &&
                    currentSize === promptSize.size && (
                      <span className='mt-1 block'>
                        {t('Output size from prompt: {{size}}', {
                          size: promptSize.size.replace('x', ' × '),
                        })}
                      </span>
                    )}
                  {promptSizeError &&
                    selectedProfile?.specification.custom_size && (
                      <span className='mt-1 block'>
                        {t(
                          'Size limits: multiples of {{step}} px; each edge up to {{maxEdge}} px; {{minPixels}}–{{maxPixels}} total pixels; aspect ratio up to {{maxRatio}}:1.',
                          {
                            step: selectedProfile.specification.custom_size
                              .step,
                            maxEdge:
                              selectedProfile.specification.custom_size
                                .max_edge,
                            minPixels:
                              selectedProfile.specification.custom_size
                                .min_pixels,
                            maxPixels:
                              selectedProfile.specification.custom_size
                                .max_pixels,
                            maxRatio:
                              selectedProfile.specification.custom_size
                                .max_aspect_ratio,
                          }
                        )}
                      </span>
                    )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          {mode === 'generation' && selectedProfile && (
            <ImageOutputQuantityField
              control={form.control}
              profile={selectedProfile}
              disabled={submitPending}
            />
          )}
          {selectedProfile && (
            <ImageParameterFields
              control={form.control}
              profile={selectedProfile}
              hideOutputCount
            />
          )}
        </div>
        <div className='bg-background shrink-0 space-y-3 border-t p-4'>
          {sizeParameter && (
            <p className='text-muted-foreground text-sm' aria-live='polite'>
              {currentSizeValid
                ? t('Current output size: {{size}}', {
                    size: String(currentSize).replace('x', ' × '),
                  })
                : t('Choose a valid output size.')}
            </p>
          )}
          <div className='flex items-center justify-between gap-3 text-sm'>
            <span className='text-muted-foreground'>
              {t('imageStudio.estimatedPrice')}
            </span>
            <span
              className='font-medium'
              aria-busy={quoteBusy}
              aria-live='polite'
            >
              {displayedQuote}
            </span>
          </div>
          <Button
            type='submit'
            className='w-full'
            disabled={
              !props.tokenGate.tokenId ||
              !quoteRequest ||
              !currentQuote ||
              quoteQuery.isFetching ||
              reference.processing ||
              directReferences.processing ||
              submitPending
            }
          >
            {submitIcon}
            {submitLabel}
          </Button>
          {!props.tokenGate.tokenId &&
            props.tokenGate.capability?.status === 'missing' && (
              <Button
                type='button'
                className='w-full'
                variant='outline'
                onClick={() => props.tokenGate.setDialogOpen(true)}
              >
                {t('imageStudio.token.createAndContinue')}
              </Button>
            )}
        </div>
      </form>
      <ImageTokenSetupDialog gate={props.tokenGate} />
    </Form>
  )
}
