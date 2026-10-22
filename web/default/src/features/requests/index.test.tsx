import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { RequestsPage } from '.'
import * as requestsApi from './api'

vi.mock('./compose-dialog', () => ({ RequestComposeDialog: () => null }))

afterEach(() => {
  cleanup()
  useAuthStore.getState().auth.reset()
  vi.restoreAllMocks()
})

test('an unavailable request feature explains the state and prevents submission', async () => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 42, username: 'customer', role: 1 })
  vi.spyOn(requestsApi, 'getRequests').mockRejectedValueOnce({
    response: {
      status: 503,
      data: { code: 'REQUEST_TICKET_SCHEMA_NOT_READY' },
    },
  })
  const client = new QueryClient()
  render(
    <QueryClientProvider client={client}>
      <RequestsPage />
    </QueryClientProvider>
  )

  expect(
    await screen.findByText(
      'Requests are temporarily unavailable. Please try again later.'
    )
  ).toBeVisible()
  expect(
    screen.getByRole('button', { name: 'Submit a request' })
  ).toBeDisabled()
  expect(screen.queryByText('Good ideas start here')).not.toBeInTheDocument()
  client.clear()
})
