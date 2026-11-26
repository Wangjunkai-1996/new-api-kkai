import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { VideoWebhookDialog } from './video-webhook-dialog'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (label: string) => label }),
}))

test('creating a version displays its one-time key without caching it; subsequent verification and revocation target that version', async () => {
  const client = new QueryClient()
  const base = '/api/token/42/video-webhook'
  const endpoint = {
    id: 'wh_example',
    url: 'https://example.com/callback',
    state: 'pending',
  }
  const secret = 'one-time-secret'
  let created = false
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: { success: true, data: url === base && created ? [endpoint] : [] },
  }))
  const put = vi.spyOn(api, 'put').mockImplementation(async () => {
    created = true
    return { data: { success: true, data: { endpoint, secret } } }
  })
  const verify = vi.spyOn(api, 'post').mockImplementation(async () => {
    endpoint.state = 'active'
    return { data: { success: true } }
  })
  const revoke = vi.spyOn(api, 'delete').mockImplementation(async () => {
    created = false
    return { data: { success: true } }
  })
  const view = render(
    <QueryClientProvider client={client}>
      <VideoWebhookDialog tokenId={42} close={() => {}} />
    </QueryClientProvider>
  )
  try {
    await screen.findByText('No records')
    fireEvent.change(screen.getByLabelText('Webhook URL'), {
      target: { value: endpoint.url },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    await screen.findByDisplayValue(secret)
    expect(put).toHaveBeenCalledWith(base, { url: endpoint.url })
    expect(
      JSON.stringify(
        client
          .getQueryCache()
          .getAll()
          .map((q) => q.state.data)
      )
    ).not.toContain(secret)
    fireEvent.click(await screen.findByRole('button', { name: 'Verify' }))
    await waitFor(() =>
      expect(verify).toHaveBeenCalledWith(`${base}/verify`, { id: endpoint.id })
    )
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: 'Verify' })).toBeNull()
    )
    fireEvent.click(screen.getByRole('button', { name: 'Revoke' }))
    expect(revoke).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }))
    await waitFor(() =>
      expect(revoke).toHaveBeenCalledWith(`${base}/${endpoint.id}`)
    )
    await waitFor(() => expect(screen.queryByDisplayValue(secret)).toBeNull())
  } finally {
    view.unmount()
    client.clear()
    vi.restoreAllMocks()
  }
})
