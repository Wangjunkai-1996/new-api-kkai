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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, useMemo } from 'react'

import {
  markAllRequestNotificationsRead,
  markRequestNotificationRead,
  requestQueryKey,
} from '@/features/requests/api'
import { useRequestAvailability } from '@/features/requests/use-request-availability'
import { useSiteConfig } from '@/features/site-config'
import { useStatus } from '@/hooks/use-status'
import { getNotice } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'
import { getCurrentHost } from '@/lib/host-scope'
import { requireServerSuccess } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'
import { useNotificationStore } from '@/stores/notification-store'

export type NotificationTab = 'notice' | 'announcements' | 'requests'

function hashString(input: string): string {
  let hash = 0
  if (!input) return '0'

  for (let i = 0; i < input.length; i += 1) {
    const chr = input.charCodeAt(i)
    hash = (hash << 5) - hash + chr
    hash |= 0
  }

  return hash.toString(36)
}

/**
 * Generate a unique key for an announcement
 * Prefer backend id, fall back to a content hash so edits register
 */
function getAnnouncementKey(item: Record<string, unknown>): string {
  if (!item) return ''

  if (item.id !== undefined && item.id !== null) {
    return `id:${item.id}`
  }

  const fingerprint = JSON.stringify({
    publishDate: (item?.publishDate as string) || '',
    content: ((item?.content as string) || '').trim(),
    extra: ((item?.extra as string) || '').trim(),
    type: (item?.type as string) || '',
    title: ((item?.title as string) || '').trim(),
    link: ((item?.link as string) || '').trim(),
  })
  return `hash:${hashString(fingerprint)}`
}

/**
 * Hook to manage notifications (Notice + Announcements)
 * Provides unread counts and read status management
 */
export function useNotifications() {
  const queryClient = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const { data: site } = useSiteConfig()
  const isTenant = (site?.tenant_id ?? 0) > 0
  const requestAvailability = useRequestAvailability()
  const requestsEnabled = requestAvailability.available
  const [popoverOpen, setPopoverOpen] = useState(false)
  const [selectedTab, setActiveTab] = useState<NotificationTab>('notice')
  const activeTab =
    selectedTab === 'requests' && !requestsEnabled ? 'notice' : selectedTab
  const notificationsQueryKey = requestQueryKey(userId, 'notifications')
  const requestNotifications = requestAvailability.query
  const readRequestNotification = useMutation({
    mutationFn: markRequestNotificationRead,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: notificationsQueryKey })
      void queryClient.invalidateQueries({
        queryKey: requestQueryKey(userId, 'list'),
      })
    },
    onError: (error) => handleServerError(error),
  })
  const readAllRequestNotifications = useMutation({
    mutationFn: markAllRequestNotificationsRead,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: notificationsQueryKey })
      void queryClient.invalidateQueries({
        queryKey: requestQueryKey(userId, 'list'),
      })
    },
    onError: (error) => handleServerError(error),
  })
  const unreadRequestCount = requestsEnabled
    ? (requestNotifications.data?.unread_count ?? 0)
    : 0

  // Fetch Notice from API
  const {
    data: noticeResponse,
    isLoading: noticeLoading,
    refetch: refetchNotice,
  } = useQuery({
    queryKey: ['notice', getCurrentHost()],
    queryFn: async () => requireServerSuccess(await getNotice()),
    staleTime: 1000 * 60 * 5, // 5 minutes
    enabled: Boolean(site) && !isTenant,
  })

  // Fetch Announcements from status
  const { status, loading: statusLoading } = useStatus()
  const announcementsEnabled =
    !isTenant && (status?.announcements_enabled ?? false)
  const announcements = useMemo<Record<string, unknown>[]>(() => {
    if (!announcementsEnabled) return []
    return ((status?.announcements || []) as Record<string, unknown>[]).slice(
      0,
      20
    )
  }, [announcementsEnabled, status?.announcements])

  // Notification store
  const {
    lastReadNotice,
    markNoticeRead,
    markAnnouncementsRead,
    isAnnouncementRead,
  } = useNotificationStore()

  // Extract notice content
  const noticeContent = isTenant
    ? (site?.announcement ?? '').trim()
    : (noticeResponse?.data ?? '').trim()

  // Calculate unread counts
  const unreadCounts = useMemo(() => {
    const noticeUnread =
      noticeContent && noticeContent !== lastReadNotice ? 1 : 0

    const announcementsUnread = announcements.filter(
      (item: Record<string, unknown>) => {
        const key = getAnnouncementKey(item)
        return !isAnnouncementRead(key)
      }
    ).length

    return {
      notice: noticeUnread,
      announcements: announcementsUnread,
      total: noticeUnread + announcementsUnread,
    }
  }, [noticeContent, lastReadNotice, announcements, isAnnouncementRead])

  const markAnnouncementsAsRead = () => {
    if (announcements.length > 0) {
      const allKeys = announcements.map((item: Record<string, unknown>) =>
        getAnnouncementKey(item)
      )
      markAnnouncementsRead(allKeys)
    }
  }

  // Handle popover open
  const handleOpenPopover = (tab?: NotificationTab) => {
    const nextTab = tab || activeTab

    // Mark currently visible content as read when opening the notification center
    if (noticeContent) {
      markNoticeRead(noticeContent)
    }
    if (nextTab === 'announcements') {
      markAnnouncementsAsRead()
    }

    setActiveTab(nextTab)
    setPopoverOpen(true)
    if (requestAvailability.eligible) void requestNotifications.refetch()
  }

  const handlePopoverOpenChange = (open: boolean) => {
    if (open) {
      handleOpenPopover(activeTab)
      return
    }

    setPopoverOpen(false)
  }

  // Handle tab change - mark announcements as read when switching to that tab
  const handleTabChange = (tab: NotificationTab) => {
    setActiveTab(tab)

    if (tab === 'announcements') {
      markAnnouncementsAsRead()
    }
  }

  return {
    // Data
    notice: noticeContent,
    announcements,
    loading: noticeLoading || statusLoading,

    // Unread counts
    unreadCount: unreadCounts.total + unreadRequestCount,
    unreadNoticeCount: unreadCounts.notice,
    unreadAnnouncementsCount: unreadCounts.announcements,

    // Popover state
    popoverOpen,
    setPopoverOpen: handlePopoverOpenChange,
    activeTab,
    setActiveTab: handleTabChange,

    // Actions
    openPopover: handleOpenPopover,
    closePopover: () => setPopoverOpen(false),
    refetchNotice,
    requestUpdates: {
      enabled: requestsEnabled,
      items: requestsEnabled ? (requestNotifications.data?.items ?? []) : [],
      unreadCount: unreadRequestCount,
      loading: requestNotifications.isPending,
      error: requestNotifications.isError,
      markingAllRead: readAllRequestNotifications.isPending,
      onRead: (id: number) => readRequestNotification.mutate(id),
      onMarkAllRead: () => readAllRequestNotifications.mutate(),
      onRetry: () => {
        void requestNotifications.refetch()
      },
    },
  }
}
