import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { AdminRequestDetail } from './admin-detail'
import * as requestsApi from './api'
import type { RequestDetail } from './api'

vi.mock('@/lib/handle-server-error', () => ({ handleServerError: vi.fn() }))

const detail: RequestDetail = {
  ticket: {
    id: 17,
    tenant_id: 0,
    user_id: 42,
    title: 'Allow named API keys',
    description: 'I need to distinguish API keys for different teams.',
    status: 'pending',
    version: 3,
    created_at: 1000,
    updated_at: 1000,
    latest_feedback: '',
    unread_count: 0,
  },
  events: [
    {
      id: 1,
      ticket_id: 17,
      actor_id: 42,
      status: 'pending',
      feedback: '',
      created_at: 1000,
      read_at: 0,
    },
  ],
}
const clients: QueryClient[] = []

beforeEach(() => {
  vi.spyOn(requestsApi, 'getAdminRequest').mockResolvedValue(detail)
})
afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
})

function renderDetail() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <AdminRequestDetail requestId={17} onClose={vi.fn()} />
    </QueryClientProvider>
  )
}

test('an unavailable request cannot expose a status transition form', async () => {
  vi.mocked(requestsApi.getAdminRequest).mockRejectedValueOnce({
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
    screen.queryByLabelText('Feedback to customer')
  ).not.toBeInTheDocument()
})

test.each(['accepted', 'rejected'] as const)(
  '%s requires feedback and submits the exact reviewed version',
  async (status) => {
    const transition = vi
      .spyOn(requestsApi, 'transitionRequest')
      .mockResolvedValue({
        ...detail,
        ticket: { ...detail.ticket, status, version: 4 },
      })
    renderDetail()
    const user = userEvent.setup()
    const submit = await screen.findByRole('button', {
      name: 'Send feedback and update status',
    })
    await user.selectOptions(screen.getByLabelText('Next status'), status)
    expect(submit).toBeDisabled()
    await user.type(screen.getByLabelText('Feedback to customer'), '   ')
    expect(submit).toBeDisabled()
    expect(transition).not.toHaveBeenCalled()
    await user.type(
      screen.getByLabelText('Feedback to customer'),
      'We reviewed the use case.'
    )
    await user.click(submit)
    await waitFor(() =>
      expect(transition).toHaveBeenCalledExactlyOnceWith(17, {
        status,
        feedback: 'We reviewed the use case.',
        version: 3,
      })
    )
  }
)

test.each([
  { status: 'accepted', options: ['scheduling', 'developing', 'completed'] },
  { status: 'scheduling', options: ['developing', 'completed'] },
] as const)(
  '$status requests allow development or direct completion with feedback',
  async ({ status, options }) => {
    vi.mocked(requestsApi.getAdminRequest).mockResolvedValueOnce({
      ...detail,
      ticket: { ...detail.ticket, status },
    })
    const transition = vi
      .spyOn(requestsApi, 'transitionRequest')
      .mockResolvedValue({
        ...detail,
        ticket: { ...detail.ticket, status: 'completed', version: 4 },
      })
    renderDetail()
    const user = userEvent.setup()
    const select = await screen.findByLabelText('Next status')
    expect(
      within(select)
        .getAllByRole('option')
        .map((option) => (option as HTMLOptionElement).value)
    ).toEqual(options)
    await user.selectOptions(select, 'developing')
    expect(select).toHaveValue('developing')
    await user.selectOptions(select, 'completed')
    const submit = screen.getByRole('button', {
      name: 'Send feedback and update status',
    })
    expect(submit).toBeDisabled()
    await user.type(
      screen.getByLabelText('Feedback to customer'),
      'The requested change has shipped.'
    )
    await user.click(submit)
    await waitFor(() =>
      expect(transition).toHaveBeenCalledExactlyOnceWith(17, {
        status: 'completed',
        feedback: 'The requested change has shipped.',
        version: 3,
      })
    )
    await waitFor(() =>
      expect(screen.queryByLabelText('Next status')).not.toBeInTheDocument()
    )
  }
)

test('a concurrent change keeps feedback and requires reviewing the refreshed status before retry', async () => {
  const transition = vi
    .spyOn(requestsApi, 'transitionRequest')
    .mockRejectedValueOnce({ isAxiosError: true, response: { status: 409 } })
  renderDetail()
  const user = userEvent.setup()
  const feedback = await screen.findByLabelText('Feedback to customer')
  await user.type(feedback, 'We can support this idea.')
  await user.click(
    screen.getByRole('button', { name: 'Send feedback and update status' })
  )
  expect(
    await screen.findByText(
      'This request has changed. Refresh and review its latest status before submitting again. Your feedback is kept.'
    )
  ).toBeVisible()
  expect(feedback).toHaveValue('We can support this idea.')
  expect(
    screen.getByRole('button', { name: 'Send feedback and update status' })
  ).toBeDisabled()
  vi.mocked(requestsApi.getAdminRequest).mockResolvedValueOnce({
    ...detail,
    ticket: { ...detail.ticket, status: 'accepted', version: 4 },
  })
  transition.mockResolvedValueOnce({
    ...detail,
    ticket: { ...detail.ticket, status: 'scheduling', version: 5 },
  })
  await user.click(screen.getByRole('button', { name: 'Refresh' }))
  await waitFor(() =>
    expect(screen.getByLabelText('Next status')).toHaveValue('scheduling')
  )
  expect(feedback).toHaveValue('We can support this idea.')
  await user.click(
    screen.getByRole('button', { name: 'Send feedback and update status' })
  )
  await waitFor(() =>
    expect(transition).toHaveBeenLastCalledWith(17, {
      status: 'scheduling',
      feedback: 'We can support this idea.',
      version: 4,
    })
  )
})

test.each(['completed', 'rejected'] as const)(
  '%s requests keep the timeline and have no further transition form',
  async (status) => {
    vi.mocked(requestsApi.getAdminRequest).mockResolvedValueOnce({
      ...detail,
      ticket: { ...detail.ticket, status },
    })
    renderDetail()
    expect(
      await screen.findByRole('list', { name: 'Request timeline' })
    ).toBeVisible()
    expect(screen.queryByLabelText('Next status')).not.toBeInTheDocument()
  }
)
