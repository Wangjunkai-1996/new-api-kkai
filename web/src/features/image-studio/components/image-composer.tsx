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
import { LoaderCircle, Sparkles } from 'lucide-react'
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
import { useAuthStore } from '@/stores/auth-store'
import {
  clearImageStudioSubmissionKey,
  getOrCreateImageStudioSubmissionKey,
  useImageStudioDraftStore,
} from '@/stores/image-studio-draft-store'

import type { ImageTokenGateState } from '../hooks/use-image-token-gate'
import {
  buildCreateImageRequest,
  buildImageComposerValues,
  classifyImageGenerationStatus,
  imageRequestFingerprint,
  imageSubmissionFingerprint,
  normalizeImageParameters,
} from '../image-domain'
import {
  useCreateImageGeneration,
  useImageModels,
  useImageQuote,
} from '../queries'
import { imageComposerSchema } from '../schemas'
import type {
  ImageComposerValues,
  ImageGeneration,
  ImageQuoteRequest,
  ImageSample,
} from '../types'
import { ImageParameterFields } from './image-parameter-fields'
import { ImageTokenSetupDialog } from './image-token-setup-dialog'

const EMPTY_VALUES: ImageComposerValues = {
  model_profile_id: 0,
  prompt: '',
  parameters: {},
}

function useDebouncedRequest(
  request: ImageQuoteRequest | null,
  delay: number
): ImageQuoteRequest | null {
  const [debounced, setDebounced] = useState(request)
  const latestRequestRef = useRef(request)
  latestRequestRef.current = request
  const fingerprint = imageRequestFingerprint(request)
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
  const submitMutation = useCreateImageGeneration()
  const form = useForm<ImageComposerValues>({
    resolver: zodResolver(imageComposerSchema),
    defaultValues: EMPTY_VALUES,
  })
  const values = useWatch({ control: form.control })
  const initializedRef = useRef(false)
  const appliedSampleRef = useRef<number | undefined>(undefined)

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
    const profile = modelsQuery.data.find(
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
  }, [form, modelsQuery.data, props.sample])

  useEffect(() => {
    if (!initializedRef.current || userId <= 0) return
    const parsed = imageComposerSchema.safeParse(values)
    if (!parsed.success) return
    const timer = window.setTimeout(() => saveDraft(userId, parsed.data), 300)
    return () => window.clearTimeout(timer)
  }, [saveDraft, userId, values])

  const selectedProfile = modelsQuery.data?.find(
    (profile) => profile.id === values.model_profile_id
  )
  const quoteRequest = useMemo<ImageQuoteRequest | null>(() => {
    if (!props.tokenGate.tokenId || !selectedProfile) return null
    const parsed = imageComposerSchema.safeParse(values)
    if (!parsed.success) return null
    const parameters = normalizeImageParameters(
      selectedProfile,
      parsed.data.parameters
    )
    const missingRequired = selectedProfile.specification.parameters.some(
      (parameter) =>
        parameter.required && parameters[parameter.key] === undefined
    )
    if (missingRequired) return null
    return {
      token_id: props.tokenGate.tokenId,
      model: selectedProfile.model,
      prompt: parsed.data.prompt.trim(),
      parameters,
      sample_id: parsed.data.sample_id,
    }
  }, [props.tokenGate.tokenId, selectedProfile, values])
  const debouncedRequest = useDebouncedRequest(quoteRequest, 400)
  const quoteQuery = useImageQuote(debouncedRequest)
  const quoteMatchesRequest =
    debouncedRequest !== null &&
    quoteRequest !== null &&
    imageRequestFingerprint(debouncedRequest) ===
      imageRequestFingerprint(quoteRequest)
  const currentQuote = quoteMatchesRequest ? quoteQuery.data : undefined
  let quoteDisplay = currentQuote?.display_amount || '—'
  if (quoteMatchesRequest && quoteQuery.isFetching) {
    quoteDisplay = t('imageStudio.pricing')
  } else if (quoteMatchesRequest && quoteQuery.isError) {
    quoteDisplay = t('imageStudio.quoteFailed')
  }

  const changeModel = (profileId: number): void => {
    const profile = modelsQuery.data?.find(
      (candidate) => candidate.id === profileId
    )
    if (!profile) return
    form.reset(
      buildImageComposerValues(profile, { prompt: form.getValues('prompt') })
    )
    appliedSampleRef.current = undefined
  }

  const submit = form.handleSubmit(async () => {
    if (!quoteRequest || !currentQuote) return
    let quote = currentQuote
    if (quote.expires_at <= Math.floor(Date.now() / 1000) + 5) {
      const refreshed = await quoteQuery.refetch()
      if (!refreshed.data) return
      quote = refreshed.data
    }
    const request = buildCreateImageRequest(quoteRequest, quote)
    let requestFingerprint: string
    try {
      requestFingerprint = await imageSubmissionFingerprint(quoteRequest)
    } catch {
      toast.error(t('imageStudio.submitFailed'))
      return
    }
    const idempotencyKey = getOrCreateImageStudioSubmissionKey(
      userId,
      requestFingerprint
    )
    if (!idempotencyKey) {
      toast.error(t('imageStudio.submitFailed'))
      return
    }
    try {
      const generation = await submitMutation.mutateAsync({
        request,
        idempotencyKey,
      })
      clearImageStudioSubmissionKey(userId, requestFingerprint)
      const outcome = classifyImageGenerationStatus(generation.status)
      if (outcome === 'failure') {
        toast.error(t('imageStudio.generationFailed'))
      } else {
        clearDraft(userId)
        toast.success(t('imageStudio.submitted'))
      }
      props.onSubmitted(generation)
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('imageStudio.submitFailed')
      )
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
          <FormField
            control={form.control}
            name='model_profile_id'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('imageStudio.model')}</FormLabel>
                <FormControl>
                  <NativeSelect
                    value={field.value > 0 ? String(field.value) : ''}
                    disabled={!props.tokenGate.tokenId || modelsQuery.isLoading}
                    onChange={(event) =>
                      changeModel(Number(event.target.value))
                    }
                  >
                    <NativeSelectOption value=''>
                      {t('imageStudio.selectModel')}
                    </NativeSelectOption>
                    {modelsQuery.data?.map((profile) => (
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
          {selectedProfile && (
            <ImageParameterFields
              control={form.control}
              profile={selectedProfile}
            />
          )}
        </div>
        <div className='bg-background shrink-0 space-y-3 border-t p-4'>
          <div className='flex items-center justify-between gap-3 text-sm'>
            <span className='text-muted-foreground'>
              {t('imageStudio.estimatedPrice')}
            </span>
            <span className='font-medium' aria-live='polite'>
              {quoteDisplay}
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
              submitMutation.isPending
            }
          >
            {submitMutation.isPending ? (
              <LoaderCircle
                className='animate-spin motion-reduce:animate-none'
                aria-hidden='true'
              />
            ) : (
              <Sparkles aria-hidden='true' />
            )}
            {submitMutation.isPending
              ? t('imageStudio.generating')
              : t('imageStudio.generate')}
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
