import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import * as requestsApi from './api'
import type { RequestDetail, RequestNotificationPage, RequestPage } from './api'
import { RequestDetailPage } from './detail'

vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  Link: (props: { to: string; children: ReactNode; className?: string }) => (
    <a href={props.to} className={props.className}>
      {props.children}
    </a>
  ),
}))
vi.mock('@/lib/handle-server-error', () => ({ handleServerError: vi.fn() }))

const detail: RequestDetail = {
  ticket: {
    id: 17,
    tenant_id: 0,
    user_id: 42,
    title: 'Named API keys',
    description: 'Identify API keys for each team.',
    status: 'scheduling',
    version: 3,
    created_at: 1000,
    updated_at: 3000,
    latest_feedback: 'We are scheduling this change.',
    unread_count: 2,
  },
  events: [
    {
      id: 1,
      ticket_id: 17,
      actor_id: 42,
      status: 'pending',
      feedback: '',
      created_at: 1000,
      read_at: 1000,
    },
    {
      id: 2,
      ticket_id: 17,
      actor_id: 1,
      status: 'accepted',
      feedback: 'We accepted this idea.',
      created_at: 2000,
      read_at: 0,
    },
    {
      id: 3,
      ticket_id: 17,
      actor_id: 1,
      status: 'scheduling',
      feedback: 'We are scheduling this change.',
      created_at: 3000,
      read_at: 0,
    },
  ],
}
const clients: QueryClient[] = []

beforeEach(() => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 42, username: 'customer', role: 1 })
  vi.spyOn(requestsApi, 'getRequest').mockResolvedValue(detail)
  vi.spyOn(requestsApi, 'markRequestNotificationRead').mockResolvedValue(
    undefined
  )
})

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  useAuthStore.getState().auth.reset()
  vi.restoreAllMocks()
  vi.clearAllMocks()
})

function renderDetail() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  const otherTicket = {
    ...detail.ticket,
    id: 99,
    title: 'Another request',
    unread_count: 1,
  }
  client.setQueryData<RequestPage>(requestsApi.requestQueryKey(42, 'list'), {
    items: [detail.ticket, otherTicket],
    total: 2,
    page: 1,
    page_size: 20,
  })
  client.setQueryData<RequestNotificationPage>(
    requestsApi.requestQueryKey(42, 'notifications'),
    {
      items: [
        {
          id: 99,
          event_id: 99,
          ticket_id: 99,
          title: otherTicket.title,
          status: 'accepted',
          feedback: 'Another request update.',
          created_at: 4000,
          read_at: 0,
        },
      ],
      total: 1,
      unread_count: 1,
    }
  )
  render(
    <QueryClientProvider client={client}>
      <RequestDetailPage requestId={17} />
    </QueryClientProvider>
  )
  return client
}

test('a request link shows a friendly unavailable state before the feature is enabled', async () => {
  vi.mocked(requestsApi.getRequest).mockRejectedValueOnce({
    response: {
      status: 503,
      data: { code: 'REQUEST_TICKET_SCHEMA_NOT_READY' },
    },
  })
  renderDetail()
  expect(
    await screen.findByText(
      'Requests are temporarily unavailable. Please try again later.'
    )
  ).toBeVisible()
  expect(
    screen.queryByRole('list', { name: 'Request timeline' })
  ).not.toBeInTheDocument()
  expect(requestsApi.markRequestNotificationRead).not.toHaveBeenCalled()
})

test('viewing a request acknowledges only its fetched unread events', async () => {
  const client = renderDetail()
  expect(
    await screen.findByRole('heading', { name: 'Named API keys' })
  ).toBeVisible()
  await waitFor(() => {
    const cached = client.getQueryData<RequestDetail>([
      ...requestsApi.requestQueryKey(42, 'detail'),
      17,
    ])
    expect(cached?.ticket.unread_count).toBe(0)
  })
  expect(
    vi
      .mocked(requestsApi.markRequestNotificationRead)
      .mock.calls.map((call) => call[0])
  ).toEqual([2, 3])
  const cached = client.getQueryData<RequestDetail>([
    ...requestsApi.requestQueryKey(42, 'detail'),
    17,
  ])
  expect(cached?.events.find((event) => event.id === 1)?.read_at).toBe(1000)
  expect(
    cached?.events
      .filter((event) => event.id !== 1)
      .every((event) => event.read_at > 0)
  ).toBe(true)
  expect(
    client.getQueryData<RequestNotificationPage>(
      requestsApi.requestQueryKey(42, 'notifications')
    )?.items[0].read_at
  ).toBe(0)
})

test('a detail read failure does not acknowledge any updates', async () => {
  vi.mocked(requestsApi.getRequest).mockRejectedValueOnce(
    new Error('Unavailable')
  )
  renderDetail()
  expect(await screen.findByText('Unable to load this request')).toBeVisible()
  expect(requestsApi.markRequestNotificationRead).not.toHaveBeenCalled()
})

test('partial acknowledgment preserves failed updates and refreshes notification and list queries', async () => {
  const error = new Error('Read update unavailable')
  vi.mocked(requestsApi.markRequestNotificationRead).mockImplementation(
    async (id) => {
      if (id === 3) throw error
    }
  )
  const client = renderDetail()
  await waitFor(() => expect(handleServerError).toHaveBeenCalledWith(error))
  const cached = client.getQueryData<RequestDetail>([
    ...requestsApi.requestQueryKey(42, 'detail'),
    17,
  ])
  expect(cached?.ticket.unread_count).toBe(1)
  expect(
    cached?.events.find((event) => event.id === 2)?.read_at
  ).toBeGreaterThan(0)
  expect(cached?.events.find((event) => event.id === 3)?.read_at).toBe(0)
  expect(
    client.getQueryState(requestsApi.requestQueryKey(42, 'list'))?.isInvalidated
  ).toBe(true)
  expect(
    client.getQueryState(requestsApi.requestQueryKey(42, 'notifications'))
      ?.isInvalidated
  ).toBe(true)
  expect(
    vi
      .mocked(requestsApi.markRequestNotificationRead)
      .mock.calls.map((call) => call[0])
  ).toEqual([2, 3])
})
