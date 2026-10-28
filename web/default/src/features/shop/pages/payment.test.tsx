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
import { webcrypto } from 'node:crypto'

import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { ShopError } from '../api'
import { orderSchema, type Order } from '../schemas'
import { PaymentPage } from './payment'

const query = vi.hoisted(() => vi.fn())
vi.mock('../hooks/use-shop', async (original) => ({
  ...(await original<typeof import('../hooks/use-shop')>()),
  useShopQuery: query,
}))
vi.mock('@/lib/api', () => ({ api: { request: vi.fn() } }))
vi.mock('@tanstack/react-router', () => ({
  Link: (props: { to: string; children: ReactNode }) => (
    <a href={props.to}>{props.children}</a>
  ),
}))
const request = vi.mocked(api.request)
let currentOrder: Order
const refetch = vi.fn()

beforeEach(() => {
  Object.defineProperty(globalThis, 'crypto', {
    configurable: true,
    value: webcrypto,
  })
  Object.defineProperty(document, 'hidden', {
    configurable: true,
    value: false,
  })
  useAuthStore.getState().auth.setUser({ id: 1, username: 'buyer', role: 1 })
  request.mockReset()
  currentOrder = orderSchema.parse({
    order_no: 'so_1',
    version: '1',
    product_snapshot: { name: 'Digital item' },
    quantity: 1,
    total: { amount_minor: '1000', currency: 'CNY', exponent: 2 },
    created_at: '2026-10-28T00:00:00Z',
    purchase_state: 'open',
    payment_state: 'unpaid',
    delivery_state: 'not_ready',
    aftersale_state: 'none',
    refund_state: 'none',
    display_status: 'awaiting_payment',
    allowed_actions: ['pay'],
  })
  query.mockImplementation((path: string) => ({
    data:
      path === '/api/shop/config'
        ? {
            payment_methods: [
              { id: 'alipay', name: 'Alipay' },
              { id: 'wxpay', name: 'WeChat Pay' },
            ],
          }
        : currentOrder,
    isPending: false,
    refetch,
  }))
})

function fixture() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const view = render(<PaymentPage orderNo='so_1' />, {
    wrapper: (props: { children: ReactNode }) => (
      <QueryClientProvider client={client}>
        {props.children}
      </QueryClientProvider>
    ),
  })
  fireEvent.change(screen.getByLabelText('Payment method'), {
    target: { value: 'alipay' },
  })
  return { ...view, client }
}

function checkout(state: string) {
  return {
    status: 200,
    data: {
      success: true,
      data: {
        order: currentOrder,
        attempt: { id: 'attempt_1', state },
        checkout: null,
      },
    },
  }
}

async function pay() {
  await act(async () => {
    fireEvent.click(
      screen.getByRole('button', { name: 'Create or resume payment' })
    )
  })
}

describe('payment recovery', () => {
  it.each([400, 409])(
    'unlocks a first definitive %s rejection and uses the refreshed version',
    async (status) => {
      request.mockRejectedValueOnce({
        isAxiosError: true,
        response: {
          status,
          data: {
            code: 'SHOP_VERSION_CONFLICT',
            data: { request_outcome: 'not_committed' },
          },
        },
      })
      const view = fixture()
      await pay()
      expect(screen.getByLabelText('Payment method')).toBeEnabled()
      const firstKey = request.mock.calls[0][0].headers?.['Idempotency-Key']
      currentOrder = { ...currentOrder, version: '2' }
      view.rerender(<PaymentPage orderNo='so_1' />)
      fireEvent.change(screen.getByLabelText('Payment method'), {
        target: { value: 'wxpay' },
      })
      request.mockResolvedValueOnce(checkout('unknown'))
      await pay()
      expect(request.mock.calls[1][0].data).toEqual({
        payment_method: 'wxpay',
        expected_version: '2',
      })
      expect(request.mock.calls[1][0].headers?.['Idempotency-Key']).not.toBe(
        firstKey
      )
    }
  )

  it.each(['transport', 'unknown'])(
    'retains the key, version and method after %s uncertainty followed by marked 409',
    async (kind) => {
      if (kind === 'transport') {
        request.mockRejectedValueOnce(new ShopError('SHOP_INTERNAL_ERROR', 503))
      } else {
        request.mockResolvedValueOnce(checkout('unknown'))
      }
      const view = fixture()
      await pay()
      const first = request.mock.calls[0][0]
      expect(screen.getByLabelText('Payment method')).toBeDisabled()
      currentOrder = { ...currentOrder, version: '2' }
      view.rerender(<PaymentPage orderNo='so_1' />)
      request.mockRejectedValueOnce(
        new ShopError('SHOP_VERSION_CONFLICT', 409, {}, 'not_committed')
      )
      await pay()
      expect(screen.getByLabelText('Payment method')).toBeDisabled()
      expect(request.mock.calls[1][0].data).toEqual(first.data)
      expect(request.mock.calls[1][0].headers?.['Idempotency-Key']).toBe(
        first.headers?.['Idempotency-Key']
      )
      expect(view.client.getQueryCache().getAll()).toHaveLength(0)
      expect(view.client.getMutationCache().getAll()).toHaveLength(0)
    }
  )

  it.each([409, 404])(
    'keeps the original request for an unmarked first %s error',
    async (status) => {
      request.mockRejectedValueOnce(
        new ShopError('SHOP_VERSION_CONFLICT', status)
      )
      const view = fixture()
      await pay()
      expect(screen.getByLabelText('Payment method')).toBeDisabled()
      const first = request.mock.calls[0][0]
      currentOrder = { ...currentOrder, version: '2' }
      view.rerender(<PaymentPage orderNo='so_1' />)
      request.mockResolvedValueOnce(checkout('ready'))
      await pay()
      expect(request.mock.calls[1][0].headers?.['Idempotency-Key']).toBe(
        first.headers?.['Idempotency-Key']
      )
      expect(request.mock.calls[1][0].data).toEqual(first.data)
    }
  )

  it('releases a retained request only after the server confirms a closed attempt', async () => {
    const view = fixture()
    request.mockResolvedValueOnce(checkout('unknown'))
    await pay()
    const firstKey = request.mock.calls[0][0].headers?.['Idempotency-Key']
    request.mockResolvedValueOnce(checkout('closed'))
    await pay()
    expect(request.mock.calls[1][0].headers?.['Idempotency-Key']).toBe(firstKey)
    expect(screen.getByLabelText('Payment method')).toBeEnabled()
    currentOrder = { ...currentOrder, version: '3' }
    view.rerender(<PaymentPage orderNo='so_1' />)
    request.mockResolvedValueOnce(checkout('ready'))
    await pay()
    expect(request.mock.calls[2][0].headers?.['Idempotency-Key']).not.toBe(
      firstKey
    )
    expect(request.mock.calls[2][0].data).toEqual({
      payment_method: 'alipay',
      expected_version: '3',
    })
  })

  it('shows the no-payment warning only while the server disallows recovery', () => {
    currentOrder = {
      ...currentOrder,
      payment_state: 'verifying',
      display_status: 'payment_verifying',
    }
    const view = fixture()
    expect(
      screen.queryByText('Payment is being verified. Do not pay again.')
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Create or resume payment' })
    ).toBeInTheDocument()
    currentOrder = { ...currentOrder, allowed_actions: ['reconcile'] }
    view.rerender(<PaymentPage orderNo='so_1' />)
    expect(
      screen.getByText('Payment is being verified. Do not pay again.')
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Create or resume payment' })
    ).not.toBeInTheDocument()
  })
})
