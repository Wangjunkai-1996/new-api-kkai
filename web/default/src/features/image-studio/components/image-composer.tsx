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
import { Images, LoaderCircle, Pencil, Sparkles } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
import { useAuthStore } from '@/stores/auth-store'
import {
  clearImageStudioSubmissionKey,
  getOrCreateImageStudioSubmissionKey,
  useImageStudioDraftStore,
} from '@/stores/image-studio-draft-store'

import { useImageComposerSchema } from '../hooks/use-image-composer-schema'
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
  IMAGE_STUDIO_EDIT_MODEL,
  isImageEditQuoteRequest,
  isImageQuoteStaleResponse,
} from '../image-edit-domain'
import {
  getImageOutputCount,
  getImageOutputParameter,
  parseImageParameters,
} from '../image-parameters'
import {
  useCreateImageEdit,
  useCreateImageGeneration,
  useImageEditQuote,
  useImageModels,
  useImageQuote,
} from '../queries'
import { imageComposerSchema } from '../schemas'
import type {
  ImageComposerValues,
  ImageEditQuoteRequest,
  ImageGeneration,
  ImageQuoteRequest,
  ImageSample,
  ImageStudioComposerMode,
} from '../types'
import { ImageOutputQuantityField } from './image-output-quantity-field'
import { ImageParameterFields } from './image-parameter-fields'
import { ImageReferenceField } from './image-reference-field'
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
  const modelsQuery = useImageModels(props.tokenGate.tokenId)
  const generationMutation = useCreateImageGeneration()
  const editMutation = useCreateImageEdit()
  const composerSchema = useImageComposerSchema(modelsQuery.data)
  const form = useForm<ImageComposerValues>({
    resolver: zodResolver(composerSchema),
    defaultValues: EMPTY_VALUES,
    mode: 'onChange',
  })
  const values = useWatch({ control: form.control })
  const [mode, setMode] = useState<ImageStudioComposerMode>('generation')
  const [staleQuoteToken, setStaleQuoteToken] = useState<string | null>(null)
  const {
    profile: editProfile,
    maxImages: maxReferenceImages,
    references: reference,
  } = useImageEditReferences(modelsQuery.data, props.tokenGate.capability)
  const initializedRef = useRef(false)
  const appliedSampleRef = useRef<number | undefined>(undefined)
  const selectedProfile = modelsQuery.data?.find(
    (profile) => profile.id === values.model_profile_id
  )
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

  const submittedRequest = generationMutation.variables?.request
  const submittedProfile = submittedRequest
    ? modelsQuery.data?.find(
        (profile) => profile.model === submittedRequest.model
      )
    : undefined
  const submittedOutputCount =
    submittedProfile && submittedRequest
      ? getImageOutputCount(
          submittedProfile,
          submittedRequest.parameters
        )
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
    hydrateDraft(userId)
    initializedRef.current = false
  }, [hydrateDraft, userId])

  useEffect(() => {
    const models = modelsQuery.data
    if (
      !models ||
      models.length === 0 ||
      initializedRef.current ||
      draftUserId !== userId
    ) {
      return
    }
    const scopedDraft = draftUserId === userId ? draft : null
    const draftProfile = models.find(
      (profile) => profile.id === scopedDraft?.model_profile_id
    )
    const profile = draftProfile ?? models[0]
    form.reset(buildImageComposerValues(profile, scopedDraft ?? undefined))
    initializedRef.current = true
  }, [draft, draftUserId, form, modelsQuery.data, userId])

  useEffect(() => {
    if (!props.sample || !modelsQuery.data) return
    if (appliedSampleRef.current === props.sample.id) return
    const profile =
      mode === 'edit'
        ? editProfile
        : modelsQuery.data.find(
            (candidate) => candidate.id === props.sample?.model_profile_id
          )
    if (!profile) return
    form.reset(
      buildImageComposerValues(profile, {
        prompt: props.sample.prompt,
        parameters: props.sample.parameters,
        sample_id: props.sample.id,
      })
    )
    appliedSampleRef.current = props.sample.id
    initializedRef.current = true
  }, [editProfile, form, mode, modelsQuery.data, props.sample])

  useEffect(() => {
    if (mode !== 'generation' || !initializedRef.current || userId <= 0) return
    if (form.getValues('prompt').trim() === '') {
      clearDraft(userId)
      return
    }
    const parsed = composerSchema.safeParse(values)
    if (!parsed.success) return
    const timer = window.setTimeout(() => saveDraft(userId, parsed.data), 300)
    return () => window.clearTimeout(timer)
  }, [clearDraft, composerSchema, form, mode, saveDraft, userId, values])

  const quoteRequest = useMemo<
    ImageQuoteRequest | ImageEditQuoteRequest | null
  >(() => {
    if (!props.tokenGate.tokenId || !selectedProfile) return null
    const parsed = imageComposerSchema.safeParse(values)
    if (!parsed.success) return null
    const parsedParameters = parseImageParameters(
      selectedProfile,
      parsed.data.parameters
    )
    if (!parsedParameters.success) return null
    const request: ImageQuoteRequest = {
      token_id: props.tokenGate.tokenId,
      model: selectedProfile.model,
      prompt: parsed.data.prompt.trim(),
      parameters: parsedParameters.parameters,
      sample_id: parsed.data.sample_id,
    }
    if (mode === 'generation') return request
    if (
      selectedProfile.model !== IMAGE_STUDIO_EDIT_MODEL ||
      reference.metadata.length === 0
    ) {
      return null
    }
    return buildImageEditQuoteRequest(request, reference.metadata)
  }, [
    mode,
    props.tokenGate.tokenId,
    reference.metadata,
    selectedProfile,
    values,
  ])
  const debouncedRequest = useDebouncedRequest(quoteRequest, 400)
  const debouncedIsEdit =
    debouncedRequest !== null && isImageEditQuoteRequest(debouncedRequest)
  const generationQuoteQuery = useImageQuote(
    mode === 'generation' && !debouncedIsEdit ? debouncedRequest : null
  )
  const editQuoteQuery = useImageEditQuote(
    mode === 'edit' && debouncedIsEdit
      ? (debouncedRequest as ImageEditQuoteRequest)
      : null
  )
  const quoteQuery = mode === 'edit' ? editQuoteQuery : generationQuoteQuery
  const quoteMatchesRequest =
    debouncedRequest !== null &&
    quoteRequest !== null &&
    imageRequestFingerprint(debouncedRequest) ===
      imageRequestFingerprint(quoteRequest)
  const currentQuote =
    quoteMatchesRequest && quoteQuery.data?.quote_token !== staleQuoteToken
      ? quoteQuery.data
      : undefined
  const submitPending = editMutation.isPending || generationMutation.isPending
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
    if (
      !profile ||
      (mode === 'edit' && profile.model !== IMAGE_STUDIO_EDIT_MODEL)
    ) {
      return
    }
    form.reset(
      buildImageComposerValues(profile, { prompt: form.getValues('prompt') })
    )
    appliedSampleRef.current = undefined
  }

  const changeMode = (nextMode: ImageStudioComposerMode): void => {
    if (nextMode === mode || submitPending) return
    setMode(nextMode)
    reference.clear()
    if (nextMode === 'edit' && editProfile) {
      form.reset(
        buildImageComposerValues(editProfile, {
          prompt: form.getValues('prompt'),
        })
      )
    }
  }

  const submit = form.handleSubmit(async () => {
    if (!quoteRequest || !currentQuote) return
    if (
      mode === 'edit' &&
      (reference.files.length === 0 || !isImageEditQuoteRequest(quoteRequest))
    ) {
      return
    }
    let quote = currentQuote
    if (isImageQuoteExpiring(quote.expires_at)) {
      const refreshed = await quoteQuery.refetch()
      if (!refreshed.data) return
      quote = refreshed.data
    }
    let requestFingerprint: string
    try {
      requestFingerprint = await imageSubmissionFingerprint(quoteRequest)
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
    try {
      let generation: ImageGeneration
      if (isImageEditQuoteRequest(quoteRequest)) {
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
      clearImageStudioSubmissionKey(userId, requestFingerprint)
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
        if (mode === 'generation' && resolution.clearDraft) clearDraft(userId)
        toast.success(
          mode === 'edit'
            ? t('imageStudio.editSubmitted')
            : t('imageStudio.submitted')
        )
      }
      props.onSubmitted(generation)
    } catch (error) {
      if (isImageQuoteStaleResponse(error)) {
        setStaleQuoteToken(quote.quote_token)
        toast.error(t('imageStudio.quoteChanged'))
        await quoteQuery.refetch()
        return
      }
      let message = t('imageStudio.submitFailed')
      if (mode === 'edit') message = t('imageStudio.editSubmitFailed')
      if (error instanceof Error) message = error.message
      toast.error(message)
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

  let selectableProfiles = modelsQuery.data
  if (mode === 'edit') selectableProfiles = editProfile ? [editProfile] : []

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
          {gateMessage && (
            <Alert variant='destructive'>
              <AlertTitle>{t('imageStudio.token.unavailable')}</AlertTitle>
              <AlertDescription>{gateMessage}</AlertDescription>
            </Alert>
          )}
          <div className='space-y-2'>
            <span className='text-sm font-medium'>{t('imageStudio.mode')}</span>
            <ToggleGroup
              value={[mode]}
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
          {mode === 'edit' && !modelsQuery.isLoading && !editProfile && (
            <Alert variant='destructive'>
              <AlertTitle>{t('imageStudio.editUnavailable')}</AlertTitle>
              <AlertDescription>
                {t('imageStudio.editModelUnavailable')}
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
                      mode === 'edit' ||
                      submitPending
                    }
                    onChange={(event) =>
                      changeModel(Number(event.target.value))
                    }
                  >
                    <NativeSelectOption value=''>
                      {t('imageStudio.selectModel')}
                    </NativeSelectOption>
                    {selectableProfiles?.map((profile) => (
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
          {mode === 'edit' && editProfile && (
            <ImageReferenceField
              controller={reference}
              maxReferenceImages={maxReferenceImages}
              disabled={submitPending || reference.processing}
            />
          )}
          <FormField
            control={form.control}
            name='prompt'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('imageStudio.prompt')}</FormLabel>
                <FormControl>
                  <Textarea
                    {...field}
                    rows={7}
                    maxLength={8000}
                    placeholder={t('imageStudio.promptPlaceholder')}
                  />
                </FormControl>
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
              hideOutputCount={mode === 'generation'}
            />
          )}
        </div>
        <div className='bg-background shrink-0 space-y-3 border-t p-4'>
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
