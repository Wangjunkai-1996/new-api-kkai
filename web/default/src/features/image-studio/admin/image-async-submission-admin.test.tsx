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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { ImageAdminPage } from '../image-admin-page'

vi.mock('@/components/layout', () => {
  const Container = (props: { children?: ReactNode }) => (
    <div>{props.children}</div>
  )
  return {
    SectionPageLayout: Object.assign(Container, {
      Title: Container,
      Content: Container,
    }),
  }
})
vi.mock('./image-model-admin', () => ({ ImageModelAdmin: () => null }))
vi.mock('./image-pricing-admin', () => ({ ImagePricingAdmin: () => null }))
vi.mock('./image-sample-admin', () => ({ ImageSampleAdmin: () => null }))

const optionKey = 'image_studio.async_submission_enabled'
const label = 'Accept asynchronous image requests'

function renderSettings() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <ImageAdminPage />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'root', role: ROLE.SUPER_ADMIN })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      message: '',
      data: [{ key: optionKey, value: 'false' }],
    },
  })
  vi.spyOn(api, 'put').mockResolvedValue({
    data: { success: true, message: '' },
  })
})

afterEach(() => useAuthStore.getState().auth.setUser(null))

test('only root sees the control or requests system options', () => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 2, username: 'admin', role: ROLE.ADMIN })
  renderSettings()
  expect(screen.queryByRole('switch', { name: label })).not.toBeInTheDocument()
  expect(api.get).not.toHaveBeenCalled()
  expect(api.put).not.toHaveBeenCalled()
})

test('root enables and disables only new asynchronous admission using the existing option API', async () => {
  let enabled = false
  vi.mocked(api.get).mockImplementation(async () => ({
    data: {
      success: true,
      message: '',
      data: [{ key: optionKey, value: String(enabled) }],
    },
  }))
  vi.mocked(api.put).mockImplementation(async (_url, request) => {
    enabled = (request as { value: string }).value === 'true'
    return { data: { success: true, message: '' } }
  })
  const user = userEvent.setup()
  renderSettings()
  const control = await screen.findByRole('switch', { name: label })
  expect(control).not.toBeChecked()
  await user.click(control)
  await waitFor(() => expect(control).toBeChecked())
  await waitFor(() => expect(control).toBeEnabled())
  expect(api.put).toHaveBeenLastCalledWith('/api/option/', {
    key: optionKey,
    value: 'true',
  })
  await user.click(control)
  await waitFor(() => expect(control).not.toBeChecked())
  expect(api.put).toHaveBeenLastCalledWith('/api/option/', {
    key: optionKey,
    value: 'false',
  })
  expect(api.put).toHaveBeenCalledTimes(2)
  expect(
    screen.getByText(
      'Applies to everyone with Image Studio access. Turning this off stops new asynchronous submissions; accepted tasks continue.'
    )
  ).toBeInTheDocument()
})

test('a rejected save leaves the displayed server value unchanged', async () => {
  vi.mocked(api.put).mockResolvedValue({
    data: { success: false, message: 'Setting update rejected' },
  })
  const user = userEvent.setup()
  renderSettings()
  const control = await screen.findByRole('switch', { name: label })
  await user.click(control)
  await waitFor(() => expect(control).toBeEnabled())
  expect(control).not.toBeChecked()
  expect(api.put).toHaveBeenCalledTimes(1)
})

test.each([
  { name: 'missing', data: [] },
  { name: 'invalid', data: [{ key: optionKey, value: 'invalid' }] },
])('$name settings cannot be changed', async ({ data }) => {
  vi.mocked(api.get).mockResolvedValue({
    data: { success: true, message: '', data },
  })
  renderSettings()
  expect(
    await screen.findByText('Unable to load asynchronous submission settings.')
  ).toBeInTheDocument()
  expect(screen.queryByRole('switch', { name: label })).not.toBeInTheDocument()
  expect(api.put).not.toHaveBeenCalled()
})
