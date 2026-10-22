import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import * as requestsApi from './api'
import type { RequestDetail } from './api'
import { RequestComposeDialog } from './compose-dialog'

const navigate = vi.hoisted(() => vi.fn())
vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  useNavigate: () => navigate,
}))
vi.mock('@/lib/handle-server-error', () => ({ handleServerError: vi.fn() }))

const detail: RequestDetail = {
  ticket: {
    id: 17,
    tenant_id: 0,
    user_id: 42,
    title: 'Named API keys',
    description: 'Identify API keys for each team.',
    status: 'pending',
    version: 1,
    created_at: 1000,
    updated_at: 1000,
    latest_feedback: '',
    unread_count: 0,
  },
  events: [],
}
const clients: QueryClient[] = []

beforeEach(() => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 42, username: 'customer', role: 1 })
  vi.spyOn(requestsApi, 'submitRequest').mockResolvedValue(detail)
  Object.defineProperty(HTMLElement.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
})

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  useAuthStore.getState().auth.reset()
  delete (HTMLElement.prototype as Partial<HTMLElement>).getAnimations
  vi.restoreAllMocks()
  vi.clearAllMocks()
})

function ComposeHarness() {
  const [open, setOpen] = useState(true)
  return (
    <>
      <button type='button' onClick={() => setOpen(true)}>
        Open request form
      </button>
      <RequestComposeDialog open={open} onOpenChange={setOpen} />
    </>
  )
}

function renderCompose() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <ComposeHarness />
    </QueryClientProvider>
  )
  return client
}

test('blank fields cannot create a request', async () => {
  renderCompose()
  const user = userEvent.setup()
  await user.type(screen.getByLabelText('Request title'), '   ')
  await user.type(screen.getByLabelText('Request description'), '   ')
  await user.click(screen.getByRole('button', { name: 'Send request' }))
  expect(
    await screen.findByText('Enter a request title (1–80 characters).')
  ).toBeVisible()
  expect(
    screen.getByText('Enter a request description (1–2,000 characters).')
  ).toBeVisible()
  expect(requestsApi.submitRequest).not.toHaveBeenCalled()
})

test('canceling and reopening keeps the unfinished request', async () => {
  renderCompose()
  const user = userEvent.setup()
  await user.type(screen.getByLabelText('Request title'), 'Named API keys')
  await user.type(
    screen.getByLabelText('Request description'),
    'Identify API keys for each team.'
  )
  await user.click(screen.getByRole('button', { name: 'Cancel' }))
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  await user.click(screen.getByRole('button', { name: 'Open request form' }))
  expect(await screen.findByLabelText('Request title')).toHaveValue(
    'Named API keys'
  )
  expect(screen.getByLabelText('Request description')).toHaveValue(
    'Identify API keys for each team.'
  )
  expect(requestsApi.submitRequest).not.toHaveBeenCalled()
})

test('successful submission trims input and opens the saved request', async () => {
  const client = renderCompose()
  const user = userEvent.setup()
  await user.type(screen.getByLabelText('Request title'), '  Named API keys  ')
  await user.type(
    screen.getByLabelText('Request description'),
    '  Identify API keys for each team.  '
  )
  await user.click(screen.getByRole('button', { name: 'Send request' }))
  await waitFor(() =>
    expect(requestsApi.submitRequest).toHaveBeenCalledExactlyOnceWith(
      {
        title: 'Named API keys',
        description: 'Identify API keys for each team.',
      },
      expect.anything()
    )
  )
  await waitFor(() =>
    expect(navigate).toHaveBeenCalledExactlyOnceWith({
      to: '/requests/$requestId',
      params: { requestId: '17' },
      search: { submitted: true },
    })
  )
  expect(
    client.getQueryData([...requestsApi.requestQueryKey(42, 'detail'), 17])
  ).toEqual(detail)
})

test('a failed submission keeps the draft available for retry', async () => {
  const error = new Error('Network unavailable')
  vi.mocked(requestsApi.submitRequest).mockRejectedValueOnce(error)
  renderCompose()
  const user = userEvent.setup()
  await user.type(screen.getByLabelText('Request title'), 'Named API keys')
  await user.type(
    screen.getByLabelText('Request description'),
    'Identify API keys for each team.'
  )
  await user.click(screen.getByRole('button', { name: 'Send request' }))
  await waitFor(() => expect(handleServerError).toHaveBeenCalledWith(error))
  expect(screen.getByLabelText('Request title')).toHaveValue('Named API keys')
  expect(screen.getByLabelText('Request description')).toHaveValue(
    'Identify API keys for each team.'
  )
  expect(screen.getByRole('button', { name: 'Send request' })).toBeEnabled()
  expect(navigate).not.toHaveBeenCalled()
})
