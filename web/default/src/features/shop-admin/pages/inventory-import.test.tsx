/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { productSchema } from '@/features/shop/schemas'
import { useAuthStore } from '@/stores/auth-store'

import { AdminInventoryPage } from './inventory'

const { request, run, beginRecovery, finishRecovery, readRecovery } =
  vi.hoisted(() => ({
    request: vi.fn(),
    run: vi.fn(),
    beginRecovery: vi.fn(),
    finishRecovery: vi.fn(),
    readRecovery: vi.fn(),
  }))
vi.mock('@/features/shop/api', async (original) => ({
  ...(await original<typeof import('@/features/shop/api')>()),
  shopRequest: request,
}))
vi.mock('@/features/shop/recovery', () => ({
  beginRecovery,
  finishRecovery,
  readRecovery,
}))

const account = productSchema.parse({
  id: 'sp_account',
  version: '2',
  name: 'Accounts',
  product_type: 'account',
  delivery_type: 'automatic',
  availability: 'available',
  template_version: '3',
  price: { amount_minor: '1000', currency: 'CNY', exponent: 2 },
  delivery_fields: [
    { key: 'account_identifier', label: 'Account', required: true },
    { key: 'password', label: 'Password', required: true },
  ],
})
const card = productSchema.parse({
  id: 'sp_card',
  version: '4',
  name: 'Codes',
  product_type: 'card',
  delivery_type: 'automatic',
  availability: 'available',
  template_version: '1',
  price: { amount_minor: '500', currency: 'CNY', exponent: 2 },
  delivery_fields: [{ key: 'card_code', label: 'Card code', required: true }],
})
const custom = productSchema.parse({
  id: 'sp_custom',
  version: '5',
  name: 'Custom',
  product_type: 'account',
  delivery_type: 'automatic',
  availability: 'available',
  template_version: '6',
  price: { amount_minor: '800', currency: 'CNY', exponent: 2 },
  delivery_fields: [{ key: 'license', label: 'License', required: true }],
})

vi.mock('@/features/shop/hooks/use-shop', async (original) => ({
  ...(await original<typeof import('@/features/shop/hooks/use-shop')>()),
  useShopQuery: (path: string) => {
    let data: unknown
    if (path === '/api/admin/shop/products?page_size=100') {
      data = {
        items: [account, card, custom],
        total: 3,
        page: 1,
        page_size: 100,
      }
    } else if (path.startsWith('/api/admin/shop/products/sp_')) {
      data = [account, card, custom].find((value) => path.endsWith(value.id))
    } else {
      data = { items: [], total: 0, page: 1, page_size: 20 }
    }
    return { data, isPending: false, error: null }
  },
  useShopAction: () => ({ run, pending: false, error: null }),
}))

beforeEach(() => {
  useAuthStore.getState().auth.setUser({ id: 20, username: 'admin', role: 100 })
  Object.defineProperty(document, 'hidden', {
    configurable: true,
    value: false,
  })
  request.mockReset()
  run.mockReset()
  beginRecovery.mockReset()
  finishRecovery.mockReset()
  readRecovery.mockReset()
  readRecovery.mockReturnValue(null)
  beginRecovery.mockReturnValue({ key: '11111111-1111-4111-8111-111111111111' })
  request.mockResolvedValue({
    preview_digest: 'digest',
    valid_count: 2,
    duplicate_count: 0,
    invalid_count: 0,
    errors: [],
  })
  run.mockResolvedValue({ batch_id: 'batch_1', inserted_count: 2 })
})

function mount(productId: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const wrapper = (props: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{props.children}</QueryClientProvider>
  )
  const view = render(<AdminInventoryPage />, { wrapper })
  fireEvent.change(screen.getByLabelText('Product'), {
    target: { value: productId },
  })
  return view
}

describe('inventory import', () => {
  it('preselects the product supplied by the inventory link', () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(<AdminInventoryPage productId={card.id} />, {
      wrapper: (props: { children: ReactNode }) => (
        <QueryClientProvider client={client}>
          {props.children}
        </QueryClientProvider>
      ),
    })
    expect(screen.getByLabelText('Product')).toHaveValue(card.id)
    expect(screen.getByLabelText('Redemption codes')).toBeInTheDocument()
  })

  it('previews account/password lines and commits exactly those encrypted-delivery fields', async () => {
    const view = mount(account.id)
    expect(
      screen.getByText('Advanced JSON import').closest('details')
    ).not.toHaveAttribute('open')
    fireEvent.change(screen.getByLabelText('Accounts and passwords'), {
      target: { value: 'alice@example.com\t pa ss \nbob\tsecret\n' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Preview import' }))
    await waitFor(() => expect(request).toHaveBeenCalledTimes(1))
    const [path, , options] = request.mock.calls[0]
    expect(path).toBe('/api/admin/shop/inventory/import-preview')
    expect(options.body).toEqual({
      product_id: account.id,
      product_version: '2',
      template_version: '3',
      rows: [
        {
          identity: 'alice@example.com',
          secret_fields: [
            { key: 'account_identifier', value: 'alice@example.com' },
            { key: 'password', value: ' pa ss ' },
          ],
        },
        {
          identity: 'bob',
          secret_fields: [
            { key: 'account_identifier', value: 'bob' },
            { key: 'password', value: 'secret' },
          ],
        },
      ],
    })
    view.rerender(<AdminInventoryPage />)
    fireEvent.click(screen.getByRole('button', { name: 'Commit import' }))
    await waitFor(() => expect(run).toHaveBeenCalledTimes(1))
    expect(run.mock.calls[0][2]).toMatchObject({
      product_id: account.id,
      product_version: '2',
      template_version: '3',
      preview_digest: 'digest',
      rows: options.body.rows,
    })
    expect(run.mock.calls[0][3]).toMatchObject({
      key: '11111111-1111-4111-8111-111111111111',
    })
    expect(screen.getByLabelText('Accounts and passwords')).toHaveValue('')
    expect(finishRecovery).toHaveBeenCalledWith('import-commit', account.id)
  })

  it('maps each code to one identity and card_code', async () => {
    mount(card.id)
    fireEvent.change(screen.getByLabelText('Redemption codes'), {
      target: { value: 'ABC-123\nXYZ-987\n' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Preview import' }))
    await waitFor(() => expect(request).toHaveBeenCalledTimes(1))
    expect(request.mock.calls[0][2].body.rows).toEqual([
      {
        identity: 'ABC-123',
        secret_fields: [{ key: 'card_code', value: 'ABC-123' }],
      },
      {
        identity: 'XYZ-987',
        secret_fields: [{ key: 'card_code', value: 'XYZ-987' }],
      },
    ])
  })

  it('rejects malformed simple rows without sending credentials or echoing them in errors', async () => {
    mount(account.id)
    fireEvent.change(screen.getByLabelText('Accounts and passwords'), {
      target: { value: 'alice\tsecret\textra' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Preview import' }))
    await screen.findByRole('alert')
    expect(request).not.toHaveBeenCalled()
    expect(screen.getByRole('alert')).not.toHaveTextContent('secret')
  })

  it('retains advanced JSON for custom fields and clears sensitive input when switching products', async () => {
    const view = mount(custom.id)
    expect(
      screen.queryByLabelText('Accounts and passwords')
    ).not.toBeInTheDocument()
    expect(
      screen.getByText('Advanced JSON import').closest('details')
    ).toHaveAttribute('open')
    const rows = [
      {
        identity: 'custom',
        secret_fields: [{ key: 'license', value: 'secret-license' }],
      },
    ]
    fireEvent.change(screen.getByLabelText('Inventory data'), {
      target: { value: JSON.stringify(rows) },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Preview JSON import' }))
    await waitFor(() => expect(request).toHaveBeenCalledTimes(1))
    expect(request.mock.calls[0][2].body.rows).toEqual(rows)
    fireEvent.change(screen.getByLabelText('Product'), {
      target: { value: card.id },
    })
    view.rerender(<AdminInventoryPage />)
    expect(screen.getByLabelText('Redemption codes')).toHaveValue('')
    expect(screen.getByLabelText('Inventory data')).toHaveValue('')
  })

  it('clears sensitive input and a completed preview when the page becomes hidden', async () => {
    mount(card.id)
    fireEvent.change(screen.getByLabelText('Redemption codes'), {
      target: { value: 'SECRET-CODE' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Preview import' }))
    await screen.findByRole('button', { name: 'Commit import' })
    act(() => {
      Object.defineProperty(document, 'hidden', {
        configurable: true,
        value: true,
      })
      document.dispatchEvent(new Event('visibilitychange'))
    })
    expect(screen.getByLabelText('Redemption codes')).toHaveValue('')
    expect(
      screen.queryByRole('button', { name: 'Commit import' })
    ).not.toBeInTheDocument()
  })

  it('recovers an uncertain import before allowing another commit and clears a recorded result', async () => {
    const key = '22222222-2222-4222-8222-222222222222'
    readRecovery.mockReturnValue({ key })
    mount(card.id)
    fireEvent.change(screen.getByLabelText('Redemption codes'), {
      target: { value: 'SECRET-CODE' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Preview import' }))
    const commit = await screen.findByRole('button', { name: 'Commit import' })
    expect(commit).toBeDisabled()
    request.mockResolvedValueOnce({ status: 'not_observed' })
    fireEvent.click(
      screen.getByRole('button', { name: 'Recover import result' })
    )
    await waitFor(() => expect(commit).toBeEnabled())
    expect(request.mock.calls[1][0]).toBe(
      `/api/admin/shop/inventory/import-requests/${key}?product_id=${card.id}`
    )
    fireEvent.click(commit)
    await waitFor(() => expect(run).toHaveBeenCalledTimes(1))
    expect(run.mock.calls[0][3]).toEqual({ key })
    expect(beginRecovery).not.toHaveBeenCalled()
    expect(finishRecovery).toHaveBeenCalledWith('import-commit', card.id)
  })
})
