import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  RequestNotificationContent,
  type RequestUpdates,
} from './request-notification-content'

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  // ScrollArea measures active animations; jsdom has no Web Animations API.
  Object.defineProperty(HTMLElement.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
})

afterEach(() => {
  delete (HTMLElement.prototype as Partial<HTMLElement>).getAnimations
})

function requestUpdates(): RequestUpdates {
  return {
    enabled: true,
    items: [
      {
        id: 8,
        event_id: 8,
        ticket_id: 4,
        title: 'Better history',
        status: 'accepted',
        feedback: 'We accepted your request.',
        created_at: 1000,
        read_at: 0,
      },
    ],
    unreadCount: 1,
    loading: false,
    error: false,
    markingAllRead: false,
    onRead: vi.fn(),
    onMarkAllRead: vi.fn(),
    onRetry: vi.fn(),
  }
}

function notificationRouter(updates: RequestUpdates, onNavigate = vi.fn()) {
  const root = createRootRoute({ component: Outlet })
  const index = createRoute({
    getParentRoute: () => root,
    path: '/',
    component: () => (
      <RequestNotificationContent updates={updates} onNavigate={onNavigate} />
    ),
  })
  const request = createRoute({
    getParentRoute: () => root,
    path: '/requests/$requestId',
    validateSearch: (search: Record<string, unknown>) => ({
      event: Number(search.event),
    }),
    component: () => <p>Request timeline</p>,
  })
  const router = createRouter({
    routeTree: root.addChildren([index, request]),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  render(<RouterProvider router={router} />)
  return router
}

describe('request notification actions', () => {
  it('marks only the clicked update read and opens its exact timeline event', async () => {
    const updates = requestUpdates()
    const onNavigate = vi.fn()
    const router = notificationRouter(updates, onNavigate)
    fireEvent.click(await screen.findByRole('link', { name: /Better history/ }))
    await waitFor(() =>
      expect(router.state.location.pathname).toBe('/requests/4')
    )
    expect(router.state.location.search).toEqual({ event: 8 })
    expect(updates.onRead).toHaveBeenCalledExactlyOnceWith(8)
    expect(updates.onMarkAllRead).not.toHaveBeenCalled()
    expect(onNavigate).toHaveBeenCalledTimes(1)
  })

  it('offers mark-all-read as an explicit action', async () => {
    const updates = requestUpdates()
    notificationRouter(updates)
    expect(updates.onMarkAllRead).not.toHaveBeenCalled()
    fireEvent.click(
      await screen.findByRole('button', { name: 'Mark all as read' })
    )
    expect(updates.onMarkAllRead).toHaveBeenCalledTimes(1)
    expect(updates.onRead).not.toHaveBeenCalled()
  })
})
