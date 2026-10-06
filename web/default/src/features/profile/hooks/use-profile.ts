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
import { useQuery } from '@tanstack/react-query'
import i18next from 'i18next'
import { useState, useEffect, useCallback } from 'react'
import { toast } from 'sonner'

import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { getUserProfile, updateUserProfile, updateUserSettings } from '../api'
import type { UpdateUserRequest, UpdateUserSettingsRequest } from '../types'

// ============================================================================
// Profile Hook
// ============================================================================

export function useProfile() {
  const userId = useAuthStore((state) => state.auth.user?.id)
  const sessionId = useAuthStore((state) => state.auth.session?.sid)
  const [updating, setUpdating] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const query = useQuery({
    queryKey: ['profile', userId, sessionId],
    queryFn: async () => {
      const response = await getUserProfile()
      if (!response.success || !response.data) {
        throw createServerError(response, i18next.t('Failed to load profile'))
      }
      return response.data
    },
    retry: false,
  })
  const reload = query.refetch
  const queryError = query.error
  useEffect(() => {
    if (queryError) {
      handleServerError(queryError, i18next.t('Failed to load profile'))
    }
  }, [queryError])

  const fetchProfile = useCallback(
    async (silent = false) => {
      if (!silent) setRefreshing(true)
      try {
        await reload()
      } finally {
        if (!silent) setRefreshing(false)
      }
    },
    [reload]
  )
  const refreshProfile = useCallback(async () => {
    await fetchProfile(true)
  }, [fetchProfile])

  // Update user profile
  const updateProfile = useCallback(
    async (data: UpdateUserRequest): Promise<boolean> => {
      try {
        setUpdating(true)
        const response = await updateUserProfile(data)

        if (response.success) {
          toast.success(i18next.t('Profile updated successfully'))
          await refreshProfile() // Refresh profile silently
          return true
        }

        handleServerError(response, i18next.t('Failed to update profile'))
        return false
      } catch (error) {
        handleServerError(error, i18next.t('Failed to update profile'))
        return false
      } finally {
        setUpdating(false)
      }
    },
    [refreshProfile]
  )

  // Update user settings
  const updateSettings = useCallback(
    async (data: UpdateUserSettingsRequest): Promise<boolean> => {
      try {
        setUpdating(true)
        const response = await updateUserSettings(data)

        if (response.success) {
          toast.success(i18next.t('Settings updated successfully'))
          await refreshProfile() // Refresh profile silently
          return true
        }

        handleServerError(response, i18next.t('Failed to update settings'))
        return false
      } catch (error) {
        handleServerError(error, i18next.t('Failed to update settings'))
        return false
      } finally {
        setUpdating(false)
      }
    },
    [refreshProfile]
  )

  return {
    profile: query.data ?? null,
    loading: query.isPending || refreshing,
    updating,
    fetchProfile,
    refreshProfile,
    updateProfile,
    updateSettings,
  }
}
