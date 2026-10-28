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

import { AxiosError } from 'axios'
import { nanoid } from 'nanoid'
import { useLayoutEffect, useRef, useState } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  useVideoStudioSubmissionStore,
  VideoSubmissionStorageError,
} from '@/stores/video-studio-submission-store'

import { useCreateVideoGeneration } from '../queries'
import { validateComposerForProfile } from '../schemas'
import type {
  VideoComposerValues,
  VideoModelProfile,
  VideoQuote,
  VideoQuoteRequest,
  VideoStudioApiError,
  VideoSubmissionReceipt,
} from '../types'
import {
  buildCreateVideoRequest,
  buildVideoQuoteRequest,
  getVideoSubmissionRequestKey,
  isVideoQuoteStaleError,
  videoQuoteHasExpired,
} from '../video-domain'
import {
  getVideoSubmissionFingerprint,
  getVideoSubmissionIdentity,
  getUnknownVideoSubmissionTaskId,
  isCurrentVideoSubmissionIdentity,
  isVideoSubmissionRejected,
  normalizeVideoSubmissionRequest,
  parseVideoSubmissionReceipt,
} from '../video-submission-protocol'
import { getVideoTokenErrorKind } from '../video-token-access'
import type { VideoTokenGateState } from './use-video-token-gate'

type VideoSubmissionOptions = {
  form: UseFormReturn<VideoComposerValues>
  selectedProfile?: VideoModelProfile
  videoTokenGate: VideoTokenGateState
  appliedSampleId?: number
  quote?: VideoQuote
  quoteRequest: VideoQuoteRequest | null
  quoteKey: string | null
  quoteRequestKey: string | null
  quoteReady: boolean
  referencesReady: boolean
  refetchQuote: () => Promise<unknown>
  onQuoteInvalidated: (key: string) => void
  onSubmitted?: (receipt: VideoSubmissionReceipt) => void
  onSubmissionUnknown?: (taskId: string) => void
}

export function useVideoSubmission(options: VideoSubmissionOptions) {
  const { t } = useTranslation()
  const {
    form,
    selectedProfile,
    videoTokenGate,
    appliedSampleId,
    quote,
    quoteRequest,
    quoteKey,
    quoteRequestKey,
    quoteReady,
  } = options
  const videoTokenId = videoTokenGate.tokenId
  const createMutation = useCreateVideoGeneration()
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [preparingSubmission, setPreparingSubmission] = useState(false)
  const submissionBusyRef = useRef(false)
  const mountedRef = useRef(true)
  const latestContextRef = useRef({
    tokenId: videoTokenId,
    group: videoTokenGate.effectiveGroup,
    quoteKey,
    requestKey: quoteRequestKey,
    referencesReady: options.referencesReady,
    quoteReady,
    profileId: selectedProfile?.id,
  })
  useLayoutEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])
  useLayoutEffect(() => {
    latestContextRef.current = {
      tokenId: videoTokenId,
      group: videoTokenGate.effectiveGroup,
      quoteKey,
      requestKey: quoteRequestKey,
      referencesReady: options.referencesReady,
      quoteReady,
      profileId: selectedProfile?.id,
    }
  }, [
    videoTokenId,
    videoTokenGate.effectiveGroup,
    quoteKey,
    quoteRequestKey,
    options.referencesReady,
    quoteReady,
    selectedProfile?.id,
  ])
  const submit = async (submittedValues: VideoComposerValues) => {
    if (submissionBusyRef.current || !selectedProfile || !videoTokenId) return
    submissionBusyRef.current = true
    setPreparingSubmission(true)
    const identity = getVideoSubmissionIdentity()
    const group = videoTokenGate.effectiveGroup
    let operationId: string | undefined
    let postInvoked = false
    try {
      if (!isCurrentVideoSubmissionIdentity(identity) || !group) {
        setSubmitError(
          t('Your video access is being verified. Please try again shortly.')
        )
        return
      }
      const validationError = validateComposerForProfile(
        submittedValues,
        selectedProfile
      )
      if (validationError) {
        setSubmitError(t(validationError))
        return
      }
      if (!options.referencesReady) {
        setSubmitError(
          t('Wait until every reference is ready before generating.')
        )
        return
      }
      if (!quoteReady || !quote || !quoteRequest) {
        setSubmitError(t('videoStudio.quoteUnavailable'))
        void options.refetchQuote()
        return
      }
      const submissionRequest = normalizeVideoSubmissionRequest(
        buildVideoQuoteRequest(
          submittedValues,
          selectedProfile,
          videoTokenId,
          appliedSampleId
        )
      )
      const submissionRequestKey =
        getVideoSubmissionRequestKey(submissionRequest)
      if (submissionRequestKey !== getVideoSubmissionRequestKey(quoteRequest)) {
        setSubmitError(t('videoStudio.quoteChanged'))
        return
      }
      const fingerprint = await getVideoSubmissionFingerprint(
        identity,
        group,
        submissionRequest
      )
      const verifyCurrentContext = () => {
        const current = latestContextRef.current
        const currentRequest = normalizeVideoSubmissionRequest(
          buildVideoQuoteRequest(
            form.getValues(),
            selectedProfile,
            videoTokenId,
            appliedSampleId
          )
        )
        if (
          !mountedRef.current ||
          !isCurrentVideoSubmissionIdentity(identity) ||
          !current.referencesReady ||
          !current.quoteReady ||
          current.profileId !== selectedProfile.id ||
          form.getValues('model_profile_id') !== selectedProfile.id ||
          current.tokenId !== videoTokenId ||
          current.group !== group ||
          current.quoteKey !== quoteKey ||
          current.requestKey !== submissionRequestKey ||
          getVideoSubmissionRequestKey(currentRequest) !==
            submissionRequestKey ||
          videoQuoteHasExpired(quote.expires_at, Math.floor(Date.now() / 1000))
        ) {
          throw new Error('Video submission context changed')
        }
      }
      verifyCurrentContext()
      const recovery = useVideoStudioSubmissionStore.getState()
      recovery.hydrate(identity.userId)
      const stored = useVideoStudioSubmissionStore.getState()
      if (stored.storageError) throw new VideoSubmissionStorageError()
      const existing = stored.records.find(
        (record) =>
          record.state !== 'accepted' &&
          record.requestFingerprint === fingerprint
      )
      if (existing) {
        setSubmitError(
          t(
            'This request is awaiting verification. Check its status instead of submitting again.'
          )
        )
        if (existing.taskId) options.onSubmissionUnknown?.(existing.taskId)
        return
      }
      operationId = nanoid()
      // oxlint-disable-next-line react/purity -- This callback runs only after the user submits.
      const submittedAt = Date.now()
      recovery.begin({
        version: 1,
        userId: identity.userId,
        operationId,
        requestFingerprint: fingerprint,
        createdAt: submittedAt,
        updatedAt: submittedAt,
        state: 'sending',
      })
      setSubmitError(null)
      const result = await createMutation.mutateAsync({
        request: buildCreateVideoRequest(submissionRequest, quote),
        idempotencyKey: operationId,
        identity,
        beforeSend: () => {
          if (postInvoked) throw new Error('Video submission already sent')
          verifyCurrentContext()
          postInvoked = true
        },
      })
      const receipt = parseVideoSubmissionReceipt(result)
      const taskId = receipt.task_id ?? receipt.id
      if (!taskId) throw new Error('Missing accepted task identity')
      recovery.resolve(identity.userId, operationId, taskId)
      if (!mountedRef.current || !isCurrentVideoSubmissionIdentity(identity)) {
        return
      }
      toast.success(t('videoStudio.generationQueued'))
      options.onSubmitted?.(receipt)
    } catch (error) {
      let recoveryError = error instanceof VideoSubmissionStorageError
      if (operationId) {
        const recovery = useVideoStudioSubmissionStore.getState()
        try {
          if (!postInvoked || isVideoSubmissionRejected(error)) {
            recovery.resolve(identity.userId, operationId)
          } else {
            recovery.markUnknown(
              identity.userId,
              operationId,
              getUnknownVideoSubmissionTaskId(error)
            )
          }
        } catch {
          recoveryError = true
        }
      }
      if (!mountedRef.current || !isCurrentVideoSubmissionIdentity(identity)) {
        return
      }
      if (recoveryError) {
        setSubmitError(
          t(
            'Recovery storage is unavailable. Generation is paused to prevent duplicate charges.'
          )
        )
        return
      }
      if (!postInvoked) {
        setSubmitError(
          t(
            'Your input or access changed. Review the current quote and try again.'
          )
        )
        return
      }
      const responseError =
        error instanceof AxiosError
          ? (error.response?.data as VideoStudioApiError | undefined)
          : undefined
      if (!isVideoSubmissionRejected(error)) {
        setSubmitError(
          t(
            'This request is awaiting verification. Check its status instead of submitting again.'
          )
        )
        const taskId = getUnknownVideoSubmissionTaskId(error)
        if (taskId) options.onSubmissionUnknown?.(taskId)
        return
      }
      const responseStatus =
        error instanceof AxiosError ? error.response?.status : undefined
      if (isVideoQuoteStaleError(responseStatus, responseError)) {
        if (quoteKey) options.onQuoteInvalidated(quoteKey)
        setSubmitError(t('videoStudio.quoteChanged'))
        void options.refetchQuote()
        return
      }
      videoTokenGate.blockAndRecheck(
        getVideoTokenErrorKind(responseError?.code)
      )
      setSubmitError(t('videoStudio.generationFailed'))
    } finally {
      submissionBusyRef.current = false
      if (mountedRef.current && isCurrentVideoSubmissionIdentity(identity)) {
        setPreparingSubmission(false)
      }
    }
  }

  return {
    submit,
    error: submitError,
    setError: setSubmitError,
    isPreparing: preparingSubmission || createMutation.isPending,
  }
}
