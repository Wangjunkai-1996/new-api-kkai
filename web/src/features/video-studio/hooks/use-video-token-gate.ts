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
import { toast } from 'sonner'

import { useAuthStore } from '@/stores/auth-store'

import {
  canStartVideoTokenCreate,
  useCreateVideoToken,
  useIsCreatingVideoToken,
  useVideoTokenCapability,
} from '../queries'
import type { VideoStudioApiError } from '../types'
import {
  getVideoTokenGateAction,
  getVideoTokenErrorKind,
  getVideoTokenRequestFailureAccess,
  getVideoTokenScopeAccess,
  releaseVideoTokenScopeBlocker,
  rememberVideoTokenAutoPrompt,
  resolveVideoTokenAccess,
  shouldAutoPromptVideoToken,
  type VideoTokenErrorKind,
  type VideoTokenScopeBlocker,
} from '../video-token-access'

export const useVideoTokenGate = () => {
  const { t } = useTranslation()
  const capabilityQuery = useVideoTokenCapability()
  const createMutation = useCreateVideoToken()
  const queryClient = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const creating = useIsCreatingVideoToken(userId)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [tokenBlocker, setTokenBlocker] =
    useState<VideoTokenScopeBlocker | null>(null)
  const promptedScopesRef = useRef<Set<string>>(new Set())
  const currentUserIdRef = useRef(userId)
  currentUserIdRef.current = userId
  const queriedAccess = capabilityQuery.data
    ? resolveVideoTokenAccess(capabilityQuery.data)
    : null
  const access = getVideoTokenScopeAccess(queriedAccess, tokenBlocker, userId)
  const refetch = capabilityQuery.refetch
  const requiredGroup =
    access?.requiredGroup ||
    capabilityQuery.data?.required_group ||
    t('videoStudio.videoKey.videoGroup')
  const checking =
    userId > 0 && !capabilityQuery.data && capabilityQuery.isFetching
  const checkFailed =
    userId > 0 &&
    !capabilityQuery.data &&
    capabilityQuery.isError &&
    !capabilityQuery.isFetching
  const gateAction = getVideoTokenGateAction(access, checkFailed)

  useEffect(() => {
    setDialogOpen(false)
    setCreateError(null)
    setTokenBlocker(null)
  }, [userId])

  useEffect(() => {
    if (!access || access.kind === 'missing') return
    setDialogOpen(false)
    setCreateError(null)
  }, [access])

  useEffect(() => {
    if (
      !access ||
      !shouldAutoPromptVideoToken(promptedScopesRef.current, userId, access)
    ) {
      return
    }
    rememberVideoTokenAutoPrompt(
      promptedScopesRef.current,
      userId,
      access.requiredGroup
    )
    setDialogOpen(true)
  }, [access, userId])

  const recheckCapability = useCallback(async () => {
    if (userId <= 0) return
    const result = await refetch()
    if (currentUserIdRef.current !== userId) return
    setTokenBlocker((current) =>
      releaseVideoTokenScopeBlocker(current, userId, result.isSuccess)
    )
  }, [refetch, userId])

  const blockAndRecheck = useCallback(
    (errorKind: VideoTokenErrorKind): boolean => {
      if (userId <= 0) return false
      const blockedAccess = getVideoTokenRequestFailureAccess(
        errorKind,
        requiredGroup
      )
      if (!blockedAccess) return false

      setTokenBlocker({ userId, access: blockedAccess })
      setDialogOpen(false)
      setCreateError(null)
      void recheckCapability()
      return true
    },
    [recheckCapability, requiredGroup, userId]
  )

  const openOrRetry = useCallback(() => {
    if (gateAction === 'create') {
      setCreateError(null)
      setDialogOpen(true)
      return
    }
    if (gateAction === 'recheck') void recheckCapability()
  }, [gateAction, recheckCapability])

  const handleDialogOpenChange = useCallback((open: boolean) => {
    setDialogOpen(open)
    if (!open) setCreateError(null)
  }, [])

  const createAndContinue = useCallback(async () => {
    if (
      userId <= 0 ||
      access?.kind !== 'missing' ||
      !canStartVideoTokenCreate(queryClient, userId)
    ) {
      return
    }

    const variables = { userId }
    const isCurrentRequestScope = () => {
      return currentUserIdRef.current === variables.userId
    }
    setCreateError(null)
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
          setDialogOpen(false)
          void recheckCapability()
          return
        }
        setCreateError(t('videoStudio.videoKey.invalidResponse'))
        return
      }

      setDialogOpen(false)
      let successMessage = t('videoStudio.videoKey.ready')
      if (capability.created) {
        successMessage = t('videoStudio.videoKey.created')
      }
      toast.success(successMessage)
    } catch (error) {
      if (!isCurrentRequestScope()) return
      const responseError =
        error instanceof AxiosError
          ? (error.response?.data as VideoStudioApiError | undefined)
          : undefined
      const errorKind = getVideoTokenErrorKind(responseError?.code)
      if (blockAndRecheck(errorKind)) return
      setCreateError(t('videoStudio.videoKey.createFailed'))
    }
  }, [
    access,
    blockAndRecheck,
    createMutation,
    queryClient,
    recheckCapability,
    t,
    userId,
  ])

  return {
    access,
    tokenId: access?.kind === 'ready' ? access.tokenId : null,
    requiredGroup,
    checking,
    checkFailed,
    gateAction,
    actionAvailable: gateAction !== 'none',
    queryFetching: capabilityQuery.isFetching,
    dialogOpen,
    creating,
    createError,
    blockAndRecheck,
    openOrRetry,
    handleDialogOpenChange,
    createAndContinue,
  }
}

export type VideoTokenGateState = ReturnType<typeof useVideoTokenGate>
