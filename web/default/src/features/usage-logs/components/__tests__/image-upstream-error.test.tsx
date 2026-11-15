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
import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { usageLogSchema } from '../../data/schema'
import type { LogOtherData } from '../../types'
import { DetailsDialog } from '../dialogs/details-dialog'

const { copyToClipboard } = vi.hoisted(() => ({
  copyToClipboard: vi.fn().mockResolvedValue(true),
}))
vi.mock('@/lib/copy-to-clipboard', () => ({ copyToClipboard }))

const clients: QueryClient[] = []
const diagnostic = {
  message: '图片服务返回明确失败 Authorization: [redacted]',
  type: 'upstream_error',
  code: 'image_provider_error',
  status_code: 502,
  truncated: false,
}

function renderDetails(
  isAdmin: boolean,
  error:
    | NonNullable<LogOtherData['admin_info']>['image_upstream_error']
    | null = diagnostic,
  isRoot = false
): void {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  client.setQueryData(['status'], {}, { updatedAt: Date.now() + 60_000 })
  render(
    <QueryClientProvider client={client}>
      <DetailsDialog
        log={usageLogSchema.parse({
          id: 1,
          user_id: 7,
          created_at: 1,
          type: 5,
          content: 'image provider request failed',
          other: JSON.stringify({
            admin_info: { image_upstream_error: error ?? undefined },
          }),
        })}
        isAdmin={isAdmin}
        isRoot={isRoot}
        open
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )
}

afterEach(() => {
  for (const client of clients) client.clear()
  clients.length = 0
})

test.each([false, true])(
  'shows and copies sanitized diagnostics for admin/root (root=%s)',
  (isRoot) => {
    renderDetails(true, diagnostic, isRoot)
    expect(screen.getByText('Image upstream error details')).toBeVisible()
    expect(screen.getByText(diagnostic.message)).toBeVisible()
    expect(screen.getByText('image_provider_error')).toBeVisible()
    expect(screen.getByText('502')).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: 'Copy error details' }))
    expect(copyToClipboard).toHaveBeenCalledWith(
      JSON.stringify(diagnostic, null, 2)
    )
  }
)

test('hides admin diagnostics and their copy action from a customer', () => {
  renderDetails(false)
  expect(
    screen.queryByText('Image upstream error details')
  ).not.toBeInTheDocument()
  expect(screen.queryByText(diagnostic.message)).not.toBeInTheDocument()
  expect(screen.queryByText('image_provider_error')).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Copy error details' })
  ).not.toBeInTheDocument()
})

test('supports structured-only diagnostics and marks truncated details', () => {
  renderDetails(true, { status_code: 502, truncated: true })
  expect(screen.getByText('502')).toBeVisible()
  expect(screen.getByText('Error details were truncated.')).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'Copy error details' }))
  expect(copyToClipboard).toHaveBeenCalledWith(
    JSON.stringify({ status_code: 502, truncated: true }, null, 2)
  )
})

test('does not show diagnostic controls for an older log without the field', () => {
  renderDetails(true, null)
  expect(
    screen.queryByText('Image upstream error details')
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Copy error details' })
  ).not.toBeInTheDocument()
})
