import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { RequestAdmin } from './admin'
import * as requestsApi from './api'

const clients: QueryClient[] = []

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
})

test('schema readiness failures keep the admin list unavailable with a friendly explanation', async () => {
  vi.spyOn(requestsApi, 'getAdminRequests').mockRejectedValueOnce({
    response: {
      status: 503,
      data: { code: 'REQUEST_TICKET_SCHEMA_NOT_READY' },
    },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <RequestAdmin />
    </QueryClientProvider>
  )
  expect(
    await screen.findByText(
      'Requests are temporarily unavailable. Please try again later.'
    )
  ).toBeVisible()
  expect(screen.queryByRole('table')).not.toBeInTheDocument()
})

test('later pages remain reachable and choosing a status resets pagination', async () => {
  const list = vi
    .spyOn(requestsApi, 'getAdminRequests')
    .mockResolvedValue({ items: [], total: 21, page: 1, page_size: 20 })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <RequestAdmin />
    </QueryClientProvider>
  )
  const user = userEvent.setup()
  const next = await screen.findByRole('button', { name: 'Next' })
  expect(screen.getByRole('button', { name: 'Previous' })).toBeDisabled()
  await user.click(next)
  await waitFor(() =>
    expect(list).toHaveBeenLastCalledWith({
      page: 2,
      pageSize: 20,
      status: undefined,
    })
  )
  expect(await screen.findByRole('button', { name: 'Previous' })).toBeEnabled()
  await user.selectOptions(screen.getByLabelText('Filter by status'), 'pending')
  await waitFor(() =>
    expect(list).toHaveBeenLastCalledWith({
      page: 1,
      pageSize: 20,
      status: 'pending',
    })
  )
  expect(screen.getByRole('button', { name: 'Previous' })).toBeDisabled()
})
