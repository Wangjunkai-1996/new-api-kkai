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

export const useVideoTokenGate = (model?: string) => {
  const { t } = useTranslation()
  const capabilityQuery = useVideoTokenCapability(model)
  const createMutation = useCreateVideoToken()
  const queryClient = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const creating = useIsCreatingVideoToken(userId, model)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [tokenBlocker, setTokenBlocker] =
    useState<VideoTokenScopeBlocker | null>(null)
  const promptedScopesRef = useRef<Set<string>>(new Set())
  const currentScopeRef = useRef({ userId, model })
  currentScopeRef.current = { userId, model }
  const queriedAccess = capabilityQuery.data
    ? resolveVideoTokenAccess(capabilityQuery.data)
    : null
  const access = getVideoTokenScopeAccess(
    queriedAccess,
    tokenBlocker,
    userId,
    model
  )
  const refetch = capabilityQuery.refetch
  const requiredGroup =
    access?.requiredGroup ||
    capabilityQuery.data?.required_group ||
    t('videoStudio.videoKey.videoGroup')
  const checking =
    Boolean(model) && !capabilityQuery.data && capabilityQuery.isFetching
  const checkFailed =
    Boolean(model) &&
    !capabilityQuery.data &&
    capabilityQuery.isError &&
    !capabilityQuery.isFetching
  const gateAction = getVideoTokenGateAction(access, checkFailed)

  useEffect(() => {
    setDialogOpen(false)
    setCreateError(null)
    setTokenBlocker(null)
  }, [model, userId])

  useEffect(() => {
    if (!access || access.kind === 'missing') return
    setDialogOpen(false)
    setCreateError(null)
  }, [access])

  useEffect(() => {
    if (
      !model ||
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
  }, [access, model, userId])

  const recheckCapability = useCallback(async () => {
    if (!model || userId <= 0) return
    const result = await refetch()
    const currentScope = currentScopeRef.current
    if (currentScope.userId !== userId || currentScope.model !== model) {
      return
    }
    setTokenBlocker((current) =>
      releaseVideoTokenScopeBlocker(current, userId, model, result.isSuccess)
    )
  }, [model, refetch, userId])

  const blockAndRecheck = useCallback(
    (errorKind: VideoTokenErrorKind): boolean => {
      if (!model || userId <= 0) return false
      const blockedAccess = getVideoTokenRequestFailureAccess(
        errorKind,
        requiredGroup
      )
      if (!blockedAccess) return false

      setTokenBlocker({ userId, model, access: blockedAccess })
      setDialogOpen(false)
      setCreateError(null)
      void recheckCapability()
      return true
    },
    [model, recheckCapability, requiredGroup, userId]
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
      !model ||
      userId <= 0 ||
      access?.kind !== 'missing' ||
      !canStartVideoTokenCreate(queryClient, userId, model)
    ) {
      return
    }

    const variables = { userId, model }
    const isCurrentRequestScope = () => {
      const currentScope = currentScopeRef.current
      return (
        currentScope.userId === variables.userId &&
        currentScope.model === variables.model
      )
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
          setTokenBlocker({ userId, model, access: nextAccess })
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
    model,
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
