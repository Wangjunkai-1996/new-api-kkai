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
import { useQueryClient } from '@tanstack/react-query'
import { AxiosError } from 'axios'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { useAuthStore } from '@/stores/auth-store'

import {
  canStartVideoTokenCreate,
  useCreateVideoToken,
  useIsCreatingVideoToken,
  useVideoTokenCapability,
  videoStudioQueryKeys,
} from '../queries'
import type { VideoStudioApiError } from '../types'
import {
  forgetVideoTokenAutoEnsure,
  getVideoTokenGateAction,
  getVideoTokenErrorKind,
  getVideoTokenRequestFailureAccess,
  getVideoTokenScopeAccess,
  getVideoTokenTerminalAccess,
  releaseVideoTokenScopeBlocker,
  rememberVideoTokenAutoEnsure,
  resolveVideoTokenAccess,
  shouldAutoEnsureVideoToken,
  type VideoTokenErrorKind,
  type VideoTokenScopeBlocker,
} from '../video-token-access'

type VideoTokenCreateFailure = Readonly<{
  userId: number
  message: string
}>

export const useVideoTokenGate = () => {
  const { t } = useTranslation()
  const capabilityQuery = useVideoTokenCapability()
  const createMutation = useCreateVideoToken()
  const queryClient = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const creating = useIsCreatingVideoToken(userId)
  const [createFailure, setCreateFailure] =
    useState<VideoTokenCreateFailure | null>(null)
  const [tokenBlocker, setTokenBlocker] =
    useState<VideoTokenScopeBlocker | null>(null)
  const attemptedUsersRef = useRef<Set<number>>(new Set())
  const tokenRecoveryAttemptsRef = useRef<Set<string>>(new Set())
  const attemptScopeUserRef = useRef(userId)
  const queriedAccess = capabilityQuery.data
    ? resolveVideoTokenAccess(capabilityQuery.data)
    : null
  const access = getVideoTokenScopeAccess(queriedAccess, tokenBlocker, userId)
  const accessKind = access?.kind
  const queriedTokenId =
    queriedAccess?.kind === 'ready' ? queriedAccess.tokenId : null
  const refetch = capabilityQuery.refetch
  const requiredGroup =
    access?.requiredGroup ||
    capabilityQuery.data?.required_group ||
    t('videoStudio.videoKey.videoGroup')
  const checking = userId > 0 && capabilityQuery.isFetching
  const checkFailed =
    userId > 0 && capabilityQuery.isError && !capabilityQuery.isFetching
  const gateAction = getVideoTokenGateAction(access, checkFailed)

  const clearVideoTokenWorkspaceQueries = useCallback(() => {
    queryClient.removeQueries({
      queryKey: videoStudioQueryKeys.modelsAll(userId),
    })
    queryClient.removeQueries({
      queryKey: videoStudioQueryKeys.samplesAll(userId),
    })
    queryClient.removeQueries({
      queryKey: videoStudioQueryKeys.sampleAll(userId),
    })
    queryClient.removeQueries({
      queryKey: videoStudioQueryKeys.quoteAll(userId),
    })
  }, [queryClient, userId])

  const recheckCapability = useCallback(async () => {
    if (userId <= 0) return
    const result = await refetch()
    if ((useAuthStore.getState().auth.user?.id ?? 0) !== userId) return
    const nextAccess = result.data ? resolveVideoTokenAccess(result.data) : null
    if (nextAccess?.kind === 'ready') {
      clearVideoTokenWorkspaceQueries()
    }
    if (nextAccess?.kind === 'missing') {
      forgetVideoTokenAutoEnsure(attemptedUsersRef.current, userId)
    }
    setTokenBlocker((current) =>
      releaseVideoTokenScopeBlocker(current, userId, result.isSuccess)
    )
  }, [clearVideoTokenWorkspaceQueries, refetch, userId])

  const blockAndRecheck = useCallback(
    (errorKind: VideoTokenErrorKind): boolean => {
      if (userId <= 0) return false
      const blockedAccess = getVideoTokenRequestFailureAccess(
        errorKind,
        requiredGroup
      )
      if (!blockedAccess) return false

      setTokenBlocker({ userId, access: blockedAccess })
      setCreateFailure(null)
      if (errorKind !== 'token-invalid' || !queriedTokenId) return true

      const recoveryScope = `${String(userId)}:${String(queriedTokenId)}`
      if (tokenRecoveryAttemptsRef.current.has(recoveryScope)) return true
      tokenRecoveryAttemptsRef.current.add(recoveryScope)
      void recheckCapability()
      return true
    },
    [queriedTokenId, recheckCapability, requiredGroup, userId]
  )

  const markTokenHealthy = useCallback(
    (tokenId: number) => {
      if (userId <= 0 || tokenId <= 0) return
      tokenRecoveryAttemptsRef.current.delete(
        `${String(userId)}:${String(tokenId)}`
      )
    },
    [userId]
  )

  const createAndContinue = useCallback(async () => {
    if (
      userId <= 0 ||
      accessKind === 'ready' ||
      accessKind === 'invalid' ||
      !canStartVideoTokenCreate(queryClient, userId)
    ) {
      return
    }

    const variables = { userId }
    const isCurrentRequestScope = () => {
      return (useAuthStore.getState().auth.user?.id ?? 0) === variables.userId
    }
    setCreateFailure(null)
    try {
      const capability = await createMutation.mutateAsync(variables)
      if (!isCurrentRequestScope()) return
      const nextAccess = resolveVideoTokenAccess(capability)
      if (nextAccess.kind !== 'ready') {
        if (
          nextAccess.kind === 'group-unavailable' ||
          nextAccess.kind === 'limit-reached' ||
          nextAccess.kind === 'models-unavailable'
        ) {
          setTokenBlocker({ userId, access: nextAccess })
          return
        }
        setCreateFailure({
          userId,
          message: t('videoStudio.workspace.prepareFailedDescription'),
        })
        return
      }

      if (
        accessKind === 'group-unavailable' ||
        accessKind === 'limit-reached' ||
        accessKind === 'models-unavailable'
      ) {
        clearVideoTokenWorkspaceQueries()
      }
      setTokenBlocker(null)
    } catch (error) {
      if (!isCurrentRequestScope()) return
      const responseError =
        error instanceof AxiosError
          ? (error.response?.data as VideoStudioApiError | undefined)
          : undefined
      const errorKind = getVideoTokenErrorKind(responseError?.code)
      const terminalAccess = getVideoTokenTerminalAccess(
        errorKind,
        requiredGroup
      )
      if (terminalAccess) {
        setTokenBlocker({ userId, access: terminalAccess })
        return
      }
      if (errorKind === 'token-invalid' && blockAndRecheck(errorKind)) return
      setCreateFailure({
        userId,
        message: t('videoStudio.workspace.prepareFailedDescription'),
      })
    }
  }, [
    accessKind,
    blockAndRecheck,
    clearVideoTokenWorkspaceQueries,
    createMutation,
    queryClient,
    requiredGroup,
    t,
    userId,
  ])

  useEffect(() => {
    if (attemptScopeUserRef.current !== userId) {
      attemptScopeUserRef.current = userId
      forgetVideoTokenAutoEnsure(attemptedUsersRef.current, userId)
    }
    if (
      !shouldAutoEnsureVideoToken(attemptedUsersRef.current, userId, accessKind)
    ) {
      return
    }
    rememberVideoTokenAutoEnsure(attemptedUsersRef.current, userId)
    void createAndContinue()
  }, [accessKind, createAndContinue, userId])

  const retry = useCallback(() => {
    if (gateAction === 'recheck') {
      void recheckCapability()
      return
    }
    if (gateAction === 'create') void createAndContinue()
  }, [createAndContinue, gateAction, recheckCapability])

  const visibleCreateError =
    (!accessKind || accessKind === 'missing') &&
    createFailure?.userId === userId
      ? createFailure.message
      : null
  const preparing =
    checking ||
    creating ||
    (userId > 0 && !access && !visibleCreateError) ||
    (accessKind === 'missing' && !visibleCreateError)

  return {
    access,
    tokenId: access?.kind === 'ready' ? access.tokenId : null,
    requiredGroup,
    checking,
    preparing,
    checkFailed,
    gateAction,
    actionAvailable: gateAction !== 'none',
    queryFetching: capabilityQuery.isFetching,
    creating,
    createError: visibleCreateError,
    blockAndRecheck,
    markTokenHealthy,
    retry,
    openOrRetry: retry,
    createAndContinue,
  }
}

export type VideoTokenGateState = ReturnType<typeof useVideoTokenGate>
