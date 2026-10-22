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
import {
  ArrowUpRight,
  ChevronDown,
  Images,
  LoaderCircle,
  Pencil,
  SlidersHorizontal,
  Sparkles,
  Trash2,
  Undo2,
} from 'lucide-react'
import { useEffect, useEffectEvent, useMemo, useRef, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
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
  isImageSubmissionReceiptExpired,
  resolveImageGenerationStatus,
} from '../image-domain'
import {
  buildImageEditQuoteRequest,
  isImageEditQuoteRequest,
  isImageQuoteStaleResponse,
  getImageProfileMaxReferenceImages,
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
import { ImageResolutionField } from './image-resolution-field'
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

export type ImageComposerLoadRequest =
  | { revision: number; kind: 'sample'; sample: ImageSample }
  | {
      revision: number
      kind: 'regenerate'
      generation: ImageGeneration
      input: ImageGenerationInput
    }
  | {
      revision: number
      kind: 'edit-output'
      file: File
      generation?: ImageGeneration
      assetId?: number
      expectedEditorRevision?: number
    }

export type ImageComposerActivity = {
  phase: 'idle' | 'preparing' | 'uploading' | 'validating' | 'submitting'
  progress?: number
}

export type ImageComposerSubmissionSnapshot = {
  mode: ImageStudioComposerMode
  values: ImageComposerValues
  referenceFiles: File[]
  referenceIds: number[]
}

export function ImageComposer(props: {
  presentation?: 'workbench' | 'sheet'
  loadRequest?: ImageComposerLoadRequest
  onLoadRequestSettled?: (revision: number, accepted: boolean) => void
  onEditorRevisionChange?: (revision: number) => void
  onActivityChange?: (activity: ImageComposerActivity) => void
  sample?: ImageSample
  reuseGeneration?: ImageGeneration
  reuseInput?: ImageGenerationInput
  persistDraft?: boolean
  tokenGate: ImageTokenGateState
  onSubmitted: (
    generation: ImageGeneration,
    snapshot?: ImageComposerSubmissionSnapshot
  ) => void
}) {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const draftUserId = useImageStudioDraftStore((state) => state.userId)
  const draft = useImageStudioDraftStore((state) => state.draft)
  const hydrateDraft = useImageStudioDraftStore((state) => state.hydrate)
  const saveDraft = useImageStudioDraftStore((state) => state.save)
  const clearDraft = useImageStudioDraftStore((state) => state.clear)
  const persistDraft = props.persistDraft ?? !props.reuseGeneration
  const workbench = props.presentation === 'workbench'
  const [reuseContext, setReuseContext] = useState(() =>
    props.reuseGeneration && props.reuseInput
      ? { generation: props.reuseGeneration, input: props.reuseInput }
      : null
  )
  const reuseGeneration = reuseContext?.generation
  const reuseInput = reuseContext?.input
  const [pendingLoad, setPendingLoad] =
    useState<ImageComposerLoadRequest | null>(null)
  const [loadingRequest, setLoadingRequest] = useState(false)
  const [canRestoreDraft, setCanRestoreDraft] = useState(false)
  const ordinaryDraftRef = useRef<ImageComposerSubmissionSnapshot | null>(null)
  const pristineValuesRef = useRef<ImageComposerValues | null>(null)
  const appliedLoadRevisionRef = useRef<number | null>(null)
  const editorRevisionRef = useRef(0)
  const editorRevisionCallbackRef = useRef(props.onEditorRevisionChange)
  editorRevisionCallbackRef.current = props.onEditorRevisionChange
  const latestLoadRequestRef = useRef(props.loadRequest)
  latestLoadRequestRef.current = props.loadRequest
  const mountedRef = useRef(true)
  const activeUserRef = useRef(userId)
  activeUserRef.current = userId
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
    reuseInput?.mode === 'edit' ? 'edit' : 'generation'
  )
  const [reuseModeConfirmed, setReuseModeConfirmed] = useState(
    reuseInput?.mode !== 'unknown'
  )
  const [usingSavedReferences, setUsingSavedReferences] = useState(
    reuseInput?.mode === 'edit'
  )
  const [savedReferenceIds, setSavedReferenceIds] = useState(
    () => reuseInput?.references.map((reference) => reference.id) ?? []
  )
  const [settingsReviewed, setSettingsReviewed] = useState(false)
  const [advancedDisclosure, setAdvancedDisclosure] = useState<{
    scope: string
    open: boolean
  } | null>(null)
  const [staleQuoteToken, setStaleQuoteToken] = useState<string | null>(null)
  const initializedRef = useRef(false)
  const appliedSampleRef = useRef<number | undefined>(undefined)
  const appliedReuseRef = useRef<number | undefined>(undefined)
  const selectedProfile = modelsQuery.data?.find(
    (profile) =>
      profile.id === values.model_profile_id &&
      profile.enabled &&
      (!reuseGeneration ||
        settingsReviewed ||
        profile.model === reuseGeneration.model)
  )
  const advancedParameters =
    selectedProfile?.specification.parameters.filter(
      (item) => !['size', 'n', 'quality'].includes(item.request_key)
    ) ?? []
  const advancedScope = `${selectedProfile?.id ?? 0}:${selectedProfile?.specification_version ?? 0}`
  const advancedHasErrors = Object.keys(
    form.formState.errors.parameters ?? {}
  ).some((key) => advancedParameters.some((item) => item.key === key))
  const advancedModified = advancedParameters.some(
    (item) =>
      values.parameters?.[item.key] !==
      selectedProfile?.default_parameters[item.key]
  )
  const advancedOpen =
    advancedHasErrors ||
    (advancedDisclosure?.scope === advancedScope
      ? advancedDisclosure.open
      : advancedParameters.length <= 2)
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
  const promptSizeApplied =
    !promptSizeError &&
    promptSize.kind === 'size' &&
    currentSizeValid &&
    currentSize === promptSize.size
  const { maxImages: maxReferenceImages, references: reference } =
    useImageEditReferences(selectedProfile, props.tokenGate.capability)
  const asyncEnabled = Boolean(
    props.tokenGate.capability?.async_submission_enabled &&
    selectedProfile &&
    props.tokenGate.capability.async_models?.includes(selectedProfile.model)
  )
  const savedReferences = useMemo(
    () =>
      reuseInput?.references.filter((reference) =>
        savedReferenceIds.includes(reference.id)
      ) ?? [],
    [reuseInput?.references, savedReferenceIds]
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
    reuseInput?.references_available &&
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
    reuseGeneration &&
    selectedProfile &&
    !settingsReviewed &&
    selectedProfile.specification_version !==
      reuseGeneration.specification_version
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
    mountedRef.current = true
    // oxlint-disable-next-line react/incompatible-library -- Subscribe to edits without replacing the existing React Hook Form state.
    const subscription = form.watch(() => {
      editorRevisionRef.current += 1
      editorRevisionCallbackRef.current?.(editorRevisionRef.current)
    })
    return () => {
      mountedRef.current = false
      subscription.unsubscribe()
    }
  }, [form])

  const referenceFingerprint = imageRequestFingerprint({
    mode,
    usingSavedReferences,
    savedReferenceIds,
    metadata: reference.metadata,
    referenceScope: [
      selectedProfile?.id,
      selectedProfile?.specification_version,
      maxReferenceImages,
      maxReferenceBytes,
      maxReferenceTotalBytes,
    ],
  })
  useEffect(() => {
    editorRevisionRef.current += 1
    editorRevisionCallbackRef.current?.(editorRevisionRef.current)
  }, [referenceFingerprint])

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
      reuseGeneration ||
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
    const initialValues = buildImageComposerValues(
      profile,
      scopedDraft ?? undefined,
      mode
    )
    pristineValuesRef.current = initialValues
    form.reset(initialValues)
    initializedRef.current = true
  }, [
    draft,
    draftUserId,
    form,
    mode,
    modelsQuery.data,
    persistDraft,
    reuseGeneration,
    userId,
  ])

  useEffect(() => {
    const generation = reuseGeneration
    if (!generation || !reuseInput || !modelsQuery.data) return
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
    setMode(reuseInput.mode === 'edit' ? 'edit' : 'generation')
    setReuseModeConfirmed(reuseInput.mode !== 'unknown')
    setUsingSavedReferences(reuseInput.mode === 'edit')
    setSavedReferenceIds(reuseInput.references.map((item) => item.id))
    setSettingsReviewed(false)
    setStaleQuoteToken(null)
    reference.clear()
    form.reset({
      model_profile_id: generation.model_profile_id,
      prompt: generation.prompt,
      parameters,
    })
    appliedReuseRef.current = generation.id
    initializedRef.current = true
  }, [form, modelsQuery.data, reuseGeneration, reuseInput, reference])

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
      reuseGeneration ||
      canRestoreDraft ||
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
    canRestoreDraft,
    reuseGeneration,
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
    loadingRequest ||
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

  const hasDraftInputs = (): boolean => {
    const current = form.getValues()
    return Boolean(
      current.prompt.trim() ||
      reference.files.length > 0 ||
      usingSavedReferences ||
      mode !== 'generation' ||
      form.formState.isDirty ||
      (pristineValuesRef.current &&
        imageRequestFingerprint(current) !==
          imageRequestFingerprint(pristineValuesRef.current))
    )
  }

  const applyLoadRequest = async (
    request: ImageComposerLoadRequest
  ): Promise<void> => {
    const expectedRevision = editorRevisionRef.current
    if (
      submitPending ||
      latestLoadRequestRef.current?.revision !== request.revision ||
      (request.kind === 'edit-output' &&
        request.expectedEditorRevision !== undefined &&
        request.expectedEditorRevision !== expectedRevision)
    ) {
      props.onLoadRequestSettled?.(request.revision, false)
      toast.info(t('Your draft changed. Try loading the image again.'))
      return
    }
    let nextMode: ImageStudioComposerMode = 'generation'
    let nextValues: ImageComposerValues
    let nextContext: typeof reuseContext = null
    let nextUsingSavedReferences = false
    let nextSavedReferenceIds: number[] = []
    let nextModeConfirmed = true
    let nextFiles: File[] = []
    let nextProfile = selectedProfile
    if (request.kind === 'sample') {
      nextProfile = modelsQuery.data?.find(
        (profile) =>
          profile.id === request.sample.model_profile_id && profile.enabled
      )
      if (!nextProfile) {
        props.onLoadRequestSettled?.(request.revision, false)
        toast.error(
          t('The original model is unavailable. Choose a model to continue.')
        )
        return
      }
      nextMode = mode
      nextFiles = mode === 'edit' ? [...reference.files] : []
      nextContext = mode === 'edit' ? reuseContext : null
      nextUsingSavedReferences = mode === 'edit' && usingSavedReferences
      nextSavedReferenceIds = nextUsingSavedReferences
        ? [...savedReferenceIds]
        : []
      nextModeConfirmed = reuseModeConfirmed
      nextValues = buildImageComposerValues(
        nextProfile,
        {
          prompt: request.sample.prompt,
          parameters: request.sample.parameters,
          sample_id: request.sample.id,
        },
        mode
      )
    } else if (request.kind === 'regenerate') {
      nextProfile = modelsQuery.data?.find(
        (profile) =>
          profile.id === request.generation.model_profile_id &&
          profile.model === request.generation.model &&
          profile.enabled
      )
      nextMode = request.input.mode === 'edit' ? 'edit' : 'generation'
      nextContext = { generation: request.generation, input: request.input }
      nextUsingSavedReferences = request.input.mode === 'edit'
      nextSavedReferenceIds = request.input.references.map((item) => item.id)
      nextModeConfirmed = request.input.mode !== 'unknown'
      const parameters = { ...request.generation.parameters }
      const output = nextProfile && getImageOutputParameter(nextProfile)
      if (output) parameters[output.key] = 1
      nextValues = {
        model_profile_id: request.generation.model_profile_id,
        prompt: request.generation.prompt,
        parameters,
      }
    } else {
      if (!nextProfile || getImageProfileMaxReferenceImages(nextProfile) < 1) {
        props.onLoadRequestSettled?.(request.revision, false)
        toast.error(t('Choose a model that supports image editing first.'))
        return
      }
      nextMode = 'edit'
      nextValues = buildImageComposerValues(
        nextProfile,
        form.getValues(),
        'edit'
      )
      nextFiles = [request.file]
    }
    const preservePreviousDraft = hasDraftInputs()
    const previousDraft: ImageComposerSubmissionSnapshot = {
      mode,
      values: {
        ...form.getValues(),
        parameters: { ...form.getValues('parameters') },
      },
      referenceFiles: [...reference.files],
      referenceIds: usingSavedReferences ? [...savedReferenceIds] : [],
    }
    setLoadingRequest(true)
    setPendingLoad(null)
    try {
      const targetMaxImages = nextProfile
        ? getImageProfileMaxReferenceImages(nextProfile)
        : 0
      const accepted = await reference.replace(nextFiles, {
        limits: nextProfile
          ? {
              scopeKey: [
                nextProfile.id,
                nextProfile.specification_version,
                targetMaxImages,
                maxReferenceBytes,
                maxReferenceTotalBytes,
              ].join(':'),
              maxReferenceImages: targetMaxImages,
              maxReferenceBytes,
              maxReferenceTotalBytes,
            }
          : undefined,
        canCommit: () =>
          mountedRef.current &&
          activeUserRef.current === userId &&
          editorRevisionRef.current === expectedRevision &&
          latestLoadRequestRef.current?.revision === request.revision,
        onCommit: () => {
          if (
            !ordinaryDraftRef.current &&
            persistDraft &&
            !reuseGeneration &&
            preservePreviousDraft
          ) {
            ordinaryDraftRef.current = previousDraft
            setCanRestoreDraft(true)
          }
          appliedReuseRef.current = nextContext?.generation.id
          initializedRef.current = true
          setReuseContext(nextContext)
          setMode(nextMode)
          setReuseModeConfirmed(nextModeConfirmed)
          setUsingSavedReferences(nextUsingSavedReferences)
          setSavedReferenceIds(nextSavedReferenceIds)
          setSettingsReviewed(request.kind === 'sample')
          setStaleQuoteToken(null)
          form.reset(nextValues)
        },
      })
      if (!mountedRef.current || activeUserRef.current !== userId) return
      props.onLoadRequestSettled?.(request.revision, accepted)
      if (accepted) {
        form.setFocus('prompt')
      } else {
        toast.error(t('Unable to load this image. Your current draft is kept.'))
      }
    } finally {
      if (mountedRef.current) setLoadingRequest(false)
    }
  }

  const acceptExternalRequest = useEffectEvent(() => {
    const request = props.loadRequest
    if (
      !request ||
      !modelsQuery.data ||
      appliedLoadRevisionRef.current === request.revision
    ) {
      return
    }
    appliedLoadRevisionRef.current = request.revision
    if (
      submitPending ||
      (request.kind === 'edit-output' &&
        request.expectedEditorRevision !== undefined &&
        request.expectedEditorRevision !== editorRevisionRef.current)
    ) {
      props.onLoadRequestSettled?.(request.revision, false)
      toast.info(t('Your draft changed. Try loading the image again.'))
      return
    }
    const needsConfirmation =
      request.kind === 'edit-output'
        ? reference.files.length > 0 ||
          (usingSavedReferences && savedReferenceIds.length > 0)
        : hasDraftInputs()
    if (needsConfirmation) {
      setPendingLoad(request)
      return
    }
    void applyLoadRequest(request)
  })
  useEffect(() => {
    acceptExternalRequest()
  }, [props.loadRequest, modelsQuery.data])

  const restoreOrdinaryDraft = async (): Promise<void> => {
    const saved = ordinaryDraftRef.current
    if (!saved || submitPending) return
    const profile = modelsQuery.data?.find(
      (item) => item.id === saved.values.model_profile_id && item.enabled
    )
    if (!profile) {
      toast.error(
        t('The original model is unavailable. Choose a model to continue.')
      )
      return
    }
    const maxImages = getImageProfileMaxReferenceImages(profile)
    const targetLimits = {
      scopeKey: [
        profile.id,
        profile.specification_version,
        maxImages,
        maxReferenceBytes,
        maxReferenceTotalBytes,
      ].join(':'),
      maxReferenceImages: maxImages,
      maxReferenceBytes,
      maxReferenceTotalBytes,
    }
    const expectedRevision = editorRevisionRef.current
    setLoadingRequest(true)
    try {
      await reference.replace(saved.referenceFiles, {
        limits: targetLimits,
        canCommit: () =>
          mountedRef.current &&
          activeUserRef.current === userId &&
          editorRevisionRef.current === expectedRevision,
        onCommit: () => {
          setReuseContext(null)
          setMode(saved.mode)
          setReuseModeConfirmed(true)
          setUsingSavedReferences(false)
          setSavedReferenceIds([])
          setSettingsReviewed(false)
          setStaleQuoteToken(null)
          initializedRef.current = true
          form.reset(saved.values)
          ordinaryDraftRef.current = null
          setCanRestoreDraft(false)
        },
      })
    } finally {
      if (mountedRef.current) setLoadingRequest(false)
    }
  }

  let activityPhase: ImageComposerActivity['phase'] = 'idle'
  let activityProgress: number | undefined
  if (loadingRequest || reference.processing) activityPhase = 'preparing'
  else if (directReferences.processing) {
    activityPhase =
      directReferences.progress.phase === 'validating'
        ? 'validating'
        : 'uploading'
    activityProgress = directReferences.progress.progress
  } else if (submitPending) activityPhase = 'submitting'
  const reportActivity = useEffectEvent(() => {
    props.onActivityChange?.({
      phase: activityPhase,
      progress: activityProgress,
    })
  })
  useEffect(() => {
    reportActivity()
  }, [activityPhase, activityProgress])

  const changeModel = (profileId: number): void => {
    const profile = modelsQuery.data?.find(
      (candidate) => candidate.id === profileId
    )
    if (!profile) return
    setSettingsReviewed(true)
    setStaleQuoteToken(null)
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
    setSavedReferenceIds([])
    setStaleQuoteToken(null)
    reference.clear()
    if (selectedProfile) {
      if (reuseGeneration) {
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
    const submittedEditorRevision = editorRevisionRef.current
    const snapshot: ImageComposerSubmissionSnapshot = {
      mode,
      values: {
        model_profile_id:
          selectedProfile?.id ?? form.getValues('model_profile_id'),
        prompt: quoteRequest.prompt,
        parameters: { ...quoteRequest.parameters },
        sample_id: quoteRequest.sample_id,
      },
      referenceFiles: [...reference.files],
      referenceIds: usingSavedReferences ? [...savedReferenceIds] : [],
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
    const existing = listPendingImageStudioSubmissions(userId).find(
      (receipt) => receipt.idempotencyKey === idempotencyKey
    )
    try {
      let generation: ImageGeneration | undefined
      if (existing) {
        setCheckingSubmission(true)
        const result = await lookupImageSubmission(idempotencyKey)
        if (result.state === 'pending') {
          toast.info(
            t(
              'Your submission is still being checked. No new image request was sent.'
            )
          )
          return
        }
        if (result.state === 'found') generation = result.generation
        else if (isImageSubmissionReceiptExpired(existing.expiresAt)) {
          toast.error(
            t(
              'This submission receipt has expired. Check your image history or contact support before submitting again.'
            )
          )
          return
        }
      }
      if (!generation) {
        if (existing || isImageQuoteExpiring(quote.expires_at)) {
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
        if (existing && isImageSubmissionReceiptExpired(existing.expiresAt)) {
          toast.error(
            t(
              'This submission receipt has expired. Check your image history or contact support before submitting again.'
            )
          )
          return
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
        if (
          persistDraft &&
          !reuseGeneration &&
          !canRestoreDraft &&
          mode === 'generation' &&
          resolution.clearDraft &&
          submittedEditorRevision === editorRevisionRef.current &&
          mountedRef.current &&
          activeUserRef.current === userId
        ) {
          clearDraft(userId)
        }
        toast.success(
          mode === 'edit'
            ? t('imageStudio.editSubmitted')
            : t('imageStudio.submitted')
        )
      }
      if (mountedRef.current && activeUserRef.current === userId) {
        if (workbench) props.onSubmitted(generation, snapshot)
        else props.onSubmitted(generation)
      }
    } catch (error) {
      const status = isAxiosError(error) ? error.response?.status : undefined
      if (
        sentSubmission &&
        !existing &&
        status &&
        status >= 400 &&
        status < 500 &&
        status !== 408 &&
        status !== 409
      ) {
        clearImageStudioSubmissionKey(userId, requestFingerprint)
      }
      if (sentSubmission && isImageQuoteStaleResponse(error)) {
        if (!existing) {
          clearImageStudioSubmissionKey(userId, requestFingerprint)
        }
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

  let promptLabel = t('imageStudio.prompt')
  if (workbench) {
    promptLabel =
      mode === 'edit' ? t('Editing instructions') : t('Scene description')
  }

  return (
    <Form {...form}>
      <form
        className={
          workbench
            ? 'image-create-editor image-create-form'
            : 'flex min-h-0 flex-1 flex-col'
        }
        onSubmit={submit}
        noValidate
      >
        <div
          className={
            workbench
              ? 'image-create-fields space-y-5'
              : 'min-h-0 flex-1 space-y-5 overflow-y-auto p-4 sm:p-5'
          }
        >
          {workbench && (
            <div className='image-create-editor-heading'>
              <h2>{t('Creation settings')}</h2>
              <span className='image-create-draft-label'>
                <span aria-hidden='true' />
                {t('Your creative workspace')}
              </span>
            </div>
          )}
          {canRestoreDraft && (
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={submitPending}
              onClick={() => void restoreOrdinaryDraft()}
            >
              <Undo2 aria-hidden='true' />
              {t('Restore previous draft')}
            </Button>
          )}
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
          {reuseGeneration && !modelsQuery.isLoading && !selectedProfile && (
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
          <div className='image-create-section space-y-2'>
            <span className={workbench ? 'sr-only' : 'text-sm font-medium'}>
              {t('imageStudio.mode')}
            </span>
            <ToggleGroup
              value={reuseModeConfirmed ? [mode] : []}
              onValueChange={(next) => {
                const nextMode = next.at(0) as
                  | ImageStudioComposerMode
                  | undefined
                if (nextMode) changeMode(nextMode)
              }}
              variant='outline'
              className='image-create-mode grid w-full grid-cols-2'
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
              <FormItem className='image-create-section'>
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
                    {reuseGeneration &&
                      !selectedProfile &&
                      field.value === reuseGeneration.model_profile_id && (
                        <NativeSelectOption
                          value={String(field.value)}
                          disabled
                        >
                          {reuseGeneration.model}
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
          <FormField
            control={form.control}
            name='prompt'
            render={({ field, fieldState }) => (
              <FormItem className='image-create-section image-create-prompt'>
                <div className='image-create-prompt-heading'>
                  <FormLabel>{promptLabel}</FormLabel>
                  {workbench && (
                    <span className='text-muted-foreground text-xs tabular-nums'>
                      {field.value.length} / 8000
                    </span>
                  )}
                </div>
                <FormControl
                  aria-invalid={Boolean(promptSizeError || fieldState.error)}
                >
                  <Textarea
                    {...field}
                    rows={workbench ? 5 : 7}
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
                <FormDescription
                  className={
                    workbench && promptSizeApplied
                      ? 'image-create-recognition is-applied'
                      : 'image-create-recognition'
                  }
                  aria-live='polite'
                >
                  {promptSizeError && (
                    <span className='text-destructive'>{promptSizeError}</span>
                  )}
                  {!promptSizeError &&
                    (!workbench || !promptSizeApplied) &&
                    t(
                      'Include a size such as 2048x1536 in your prompt to apply it automatically.'
                    )}
                  {promptSizeApplied && promptSize.kind === 'size' && (
                    <span className={workbench ? undefined : 'mt-1 block'}>
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
          <div className='image-create-references'>
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
          </div>
          {workbench && selectedProfile ? (
            <>
              <section className='image-create-section image-create-output'>
                <div className='image-create-section-title'>
                  <h3>{t('Output settings')}</h3>
                  {sizeParameter && (
                    <span className='text-muted-foreground text-xs tabular-nums'>
                      {String(currentSize ?? '').replace('x', ' × ')}
                    </span>
                  )}
                </div>
                <ImageResolutionField
                  control={form.control}
                  profile={selectedProfile}
                  disabled={submitPending}
                  appliedSize={
                    !promptSizeError &&
                    promptSize.kind === 'size' &&
                    currentSize === promptSize.size
                      ? promptSize.size
                      : undefined
                  }
                />
                <div className='image-create-output-row'>
                  {mode === 'generation' && (
                    <div className='image-create-quantity'>
                      <ImageOutputQuantityField
                        control={form.control}
                        profile={selectedProfile}
                        disabled={submitPending}
                      />
                    </div>
                  )}
                  <ImageParameterFields
                    control={form.control}
                    profile={selectedProfile}
                    group='quality'
                    disabled={submitPending}
                  />
                </div>
                {mode === 'edit' && (
                  <p className='text-muted-foreground text-xs'>
                    {t('Image editing produces one image at a time.')}
                  </p>
                )}
              </section>
              {advancedParameters.length > 0 && (
                <details
                  className='image-create-parameters'
                  open={advancedOpen}
                  onToggle={(event) => {
                    const open = event.currentTarget.open
                    if (open !== advancedOpen) {
                      setAdvancedDisclosure({ scope: advancedScope, open })
                    }
                  }}
                >
                  <summary>
                    <span>
                      <SlidersHorizontal aria-hidden='true' />
                      {t('More model parameters')}
                      <small>{advancedParameters.length}</small>
                      {advancedHasErrors && (
                        <small className='text-destructive' role='status'>
                          {t('Error')}
                        </small>
                      )}
                      {!advancedHasErrors && advancedModified && (
                        <small role='status'>{t('Changed')}</small>
                      )}
                    </span>
                    <ChevronDown aria-hidden='true' />
                  </summary>
                  <ImageParameterFields
                    control={form.control}
                    profile={selectedProfile}
                    group='advanced'
                    disabled={submitPending}
                  />
                </details>
              )}
            </>
          ) : (
            <>
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
                  disabled={submitPending}
                />
              )}
            </>
          )}
        </div>
        <div
          className={
            workbench
              ? 'image-create-footer space-y-3'
              : 'bg-background shrink-0 space-y-3 border-t p-4'
          }
        >
          {sizeParameter && (
            <p
              className='image-create-current-size text-muted-foreground text-sm'
              aria-live='polite'
            >
              {currentSizeValid
                ? t('Current output size: {{size}}', {
                    size: String(currentSize).replace('x', ' × '),
                  })
                : t('Choose a valid output size.')}
            </p>
          )}
          <div className='image-create-price flex items-center justify-between gap-3 text-sm'>
            <span className='text-muted-foreground'>
              {workbench
                ? t('Estimated cost')
                : t('imageStudio.estimatedPrice')}
            </span>
            <span
              className='font-medium'
              aria-busy={quoteBusy}
              aria-live='polite'
            >
              {workbench && quoteBusy ? (
                <>
                  <span className='sr-only'>
                    {t('Calculating estimated cost')}
                  </span>
                  <span
                    className='image-create-quote-placeholder'
                    aria-hidden='true'
                  >
                    <span />
                  </span>
                </>
              ) : (
                <span
                  key={displayedQuote}
                  className={workbench ? 'image-create-quote-value' : undefined}
                >
                  {displayedQuote}
                </span>
              )}
            </span>
          </div>
          <Button
            type='submit'
            className='image-create-submit w-full'
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
            <span>{submitLabel}</span>
            {workbench && <ArrowUpRight aria-hidden='true' />}
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
          {workbench && (
            <p className='image-create-submit-hint text-muted-foreground text-center text-xs'>
              {t(
                'Each generation creates a new task. Your previous work is kept.'
              )}
            </p>
          )}
        </div>
      </form>
      <AlertDialog
        open={Boolean(pendingLoad)}
        onOpenChange={(open) => {
          if (!open && pendingLoad) {
            props.onLoadRequestSettled?.(pendingLoad.revision, false)
            setPendingLoad(null)
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {pendingLoad?.kind === 'edit-output'
                ? t('Replace the current reference images?')
                : t('Load these creation settings?')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {pendingLoad?.kind === 'edit-output'
                ? t(
                    'The selected image will replace your current references. Your prompt and output size will be kept.'
                  )
                : t(
                    'Your current inputs will be replaced. Previous results will be kept.'
                  )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>
              {t('Keep editing current draft')}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (pendingLoad) void applyLoadRequest(pendingLoad)
              }}
            >
              {pendingLoad?.kind === 'edit-output'
                ? t('Use this image to edit')
                : t('Load settings')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <ImageTokenSetupDialog gate={props.tokenGate} />
    </Form>
  )
}
