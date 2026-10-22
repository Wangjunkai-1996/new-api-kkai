import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  getRequestNotifications,
  markAllRequestNotificationsRead,
  markRequestNotificationRead,
  requestQueryKey,
} from '@/features/requests/api'
import { useAuthStore } from '@/stores/auth-store'
import { useNotificationStore } from '@/stores/notification-store'

import { useNotifications } from '../use-notifications'

const state = vi.hoisted(() => ({ tenantId: 0 }))

vi.mock('@/features/site-config', () => ({
  useSiteConfig: () => ({ data: { tenant_id: state.tenantId } }),
}))
vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({ status: {}, loading: false }),
}))
vi.mock('@/lib/api', () => ({
  getNotice: vi.fn().mockResolvedValue({ success: true, data: '' }),
}))
vi.mock('@/features/requests/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/requests/api')>()),
  requestQueryKey: (userId: number | undefined, resource: string) => [
    'requests',
    'test-host',
    userId,
    resource,
  ],
  getRequestNotifications: vi.fn(),
  markRequestNotificationRead: vi.fn(),
  markAllRequestNotificationsRead: vi.fn(),
}))

function notifications() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  function Wrapper(props: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        {props.children}
      </QueryClientProvider>
    )
  }
  return {
    ...renderHook(() => useNotifications(), { wrapper: Wrapper }),
    client,
  }
}

beforeEach(() => {
  state.tenantId = 0
  useAuthStore.getState().auth.reset()
  useNotificationStore.setState({
    lastReadNotice: '',
    readAnnouncementKeys: [],
  })
  vi.mocked(getRequestNotifications).mockResolvedValue({
    items: [
      {
        id: 7,
        event_id: 7,
        ticket_id: 4,
        title: 'Better history',
        status: 'accepted',
        feedback: 'We accepted your request.',
        created_at: 1000,
        read_at: 0,
      },
    ],
    total: 1,
    unread_count: 1,
  })
  vi.mocked(markRequestNotificationRead).mockResolvedValue(undefined)
  vi.mocked(markAllRequestNotificationsRead).mockResolvedValue(undefined)
})

afterEach(() => {
  useAuthStore.getState().auth.reset()
  vi.clearAllMocks()
})

describe('personal request notifications', () => {
  it('hides unavailable request updates and enables them after the next successful query', async () => {
    useAuthStore.getState().auth.setUser({ id: 1, username: 'alice', role: 1 })
    vi.mocked(getRequestNotifications).mockRejectedValueOnce({
      response: {
        status: 503,
        data: { code: 'REQUEST_TICKET_SCHEMA_NOT_READY' },
      },
    })
    const { result, client } = notifications()
    expect(result.current.requestUpdates.enabled).toBe(false)
    await waitFor(() => expect(result.current.requestUpdates.error).toBe(true))
    expect(result.current.requestUpdates.enabled).toBe(false)
    expect(result.current.requestUpdates.items).toEqual([])

    await act(async () => {
      await client.invalidateQueries({
        queryKey: requestQueryKey(1, 'notifications'),
      })
    })
    await waitFor(() =>
      expect(result.current.requestUpdates.enabled).toBe(true)
    )
    expect(result.current.requestUpdates.unreadCount).toBe(1)
    act(() => result.current.setActiveTab('requests'))
    vi.mocked(getRequestNotifications).mockRejectedValueOnce({
      response: {
        status: 503,
        data: { code: 'REQUEST_TICKET_SCHEMA_NOT_READY' },
      },
    })
    await act(async () => {
      await client.invalidateQueries({
        queryKey: requestQueryKey(1, 'notifications'),
      })
    })
    await waitFor(() =>
      expect(result.current.requestUpdates.enabled).toBe(false)
    )
    expect(result.current.activeTab).toBe('notice')
    expect(result.current.requestUpdates.items).toEqual([])
    expect(result.current.requestUpdates.unreadCount).toBe(0)
    client.clear()
  })

  it.each(['guest', 'tenant'] as const)(
    'does not request private notifications for %s headers',
    async (mode) => {
      if (mode === 'tenant') {
        state.tenantId = 9
        useAuthStore
          .getState()
          .auth.setUser({ id: 1, username: 'alice', role: 1 })
      }
      const { result, client } = notifications()
      await act(async () => result.current.openPopover())
      expect(result.current.requestUpdates.enabled).toBe(false)
      expect(result.current.requestUpdates.items).toEqual([])
      expect(getRequestNotifications).not.toHaveBeenCalled()
      client.clear()
    }
  )

  it('preserves unread updates when opening the bell or switching to the request tab', async () => {
    useAuthStore.getState().auth.setUser({ id: 1, username: 'alice', role: 1 })
    const { result, client } = notifications()
    await waitFor(() =>
      expect(result.current.requestUpdates.unreadCount).toBe(1)
    )
    act(() => result.current.openPopover())
    act(() => result.current.setActiveTab('requests'))
    expect(result.current.popoverOpen).toBe(true)
    expect(result.current.activeTab).toBe('requests')
    expect(result.current.unreadCount).toBe(1)
    expect(markRequestNotificationRead).not.toHaveBeenCalled()
    expect(markAllRequestNotificationsRead).not.toHaveBeenCalled()
    client.clear()
  })

  it('persists an explicit read action and refreshes the server unread count', async () => {
    useAuthStore.getState().auth.setUser({ id: 1, username: 'alice', role: 1 })
    const { result, client } = notifications()
    await waitFor(() =>
      expect(result.current.requestUpdates.unreadCount).toBe(1)
    )
    vi.mocked(getRequestNotifications).mockResolvedValue({
      items: [],
      total: 0,
      unread_count: 0,
    })
    act(() => result.current.requestUpdates.onRead(7))
    await waitFor(() =>
      expect(result.current.requestUpdates.unreadCount).toBe(0)
    )
    expect(markRequestNotificationRead).toHaveBeenCalledExactlyOnceWith(
      7,
      expect.anything()
    )
    act(() => result.current.requestUpdates.onMarkAllRead())
    await waitFor(() =>
      expect(markAllRequestNotificationsRead).toHaveBeenCalledTimes(1)
    )
    client.clear()
  })

  it('keeps the unread marker when persisting a read action fails', async () => {
    useAuthStore.getState().auth.setUser({ id: 1, username: 'alice', role: 1 })
    vi.mocked(markRequestNotificationRead).mockRejectedValue(
      new Error('Network unavailable')
    )
    const { result, client } = notifications()
    await waitFor(() =>
      expect(result.current.requestUpdates.unreadCount).toBe(1)
    )
    act(() => result.current.requestUpdates.onRead(7))
    await waitFor(() => expect(client.isMutating()).toBe(0))
    expect(result.current.requestUpdates.unreadCount).toBe(1)
    expect(result.current.requestUpdates.items[0].read_at).toBe(0)
    client.clear()
  })
})
