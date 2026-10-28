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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { productSchema, type Product } from '@/features/shop/schemas'

import { ProductEditorPage } from './products'

const { query, run, navigate } = vi.hoisted(() => ({
  query: vi.fn(),
  run: vi.fn(),
  navigate: vi.fn(),
}))
let identity = 'admin-session'
vi.mock('@/features/shop/hooks/use-shop', () => ({
  useShopQuery: query,
  useShopAction: () => ({ run, pending: false, error: null }),
  useShopIdentity: () => identity,
}))
vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => navigate,
  Link: (props: { to: string; children: ReactNode }) => (
    <a href={props.to}>{props.children}</a>
  ),
}))
vi.mock('../components/product-cover-upload', () => ({
  ProductCoverUpload: (props: {
    value: string
    onChange: (url: string) => void
    onBusy: (busy: boolean) => void
  }) => (
    <div>
      <output data-testid='cover-value'>{props.value}</output>
      <button
        type='button'
        onClick={() => props.onChange(`/api/shop/covers/${'a'.repeat(64)}.jpg`)}
      >
        Select uploaded cover
      </button>
      <button type='button' onClick={() => props.onBusy(true)}>
        Start cover upload
      </button>
      <button type='button' onClick={() => props.onBusy(false)}>
        Finish cover upload
      </button>
    </div>
  ),
}))

let product: Product | undefined
beforeEach(() => {
  run.mockReset()
  navigate.mockReset()
  run.mockResolvedValue({ id: 'sp_1' })
  identity = 'admin-session'
  product = undefined
  query.mockImplementation((path: string) => {
    let data: unknown = product
    if (path === '/api/admin/shop/settings') {
      data = { currency: 'CNY', currency_exponent: 2 }
    }
    if (path === '/api/admin/shop/categories') data = []
    return { data, isPending: false, error: null }
  })
})

function save() {
  fireEvent.click(
    screen.getByRole('button', { name: /Save draft|Save changes/ })
  )
}

describe('product editor', () => {
  it('creates a card-code draft with publishable automatic defaults and no buyer input', async () => {
    render(<ProductEditorPage />)
    expect(screen.getByLabelText('Specification')).toHaveAttribute(
      'maxLength',
      '8000'
    )
    expect(
      screen.getByText('Advanced settings').closest('details')
    ).not.toHaveAttribute('open')
    fireEvent.change(screen.getByLabelText('Name'), {
      target: { value: 'Gift code' },
    })
    fireEvent.change(screen.getByLabelText('Price · CNY'), {
      target: { value: '19.90' },
    })
    fireEvent.change(screen.getByLabelText('Product type'), {
      target: { value: 'card' },
    })
    save()
    await waitFor(() => expect(run).toHaveBeenCalledTimes(1))
    const [path, , body] = run.mock.calls[0]
    expect(path).toBe('/api/admin/shop/products')
    expect(body).toMatchObject({
      product_type: 'card',
      delivery_type: 'automatic',
      price: { amount_minor: '1990', currency: 'CNY', exponent: 2 },
      delivery_fields: [{ key: 'card_code', required: true }],
      buyer_input_schema: [],
      delivery_policy: { mode: 'automatic', sla: null },
      view_policy: { duration_seconds: 30 * 86400 },
      warranty_policy: { mode: 'none' },
      entitlement_policy: { mode: 'described' },
      refund_policy: {
        goods_refund_mode: 'full_only',
        execution: 'manual_original_channel',
      },
      inventory_identity_domain: 'shop:card',
      identity_normalization_version: '1',
      template_version: '1',
    })
    for (const value of [
      body.usage_conditions,
      body.delivery_policy.public_terms,
      body.view_policy.public_terms,
      body.warranty_policy.public_terms,
      body.entitlement_policy.public_terms,
      body.refund_policy.public_terms,
    ]) {
      expect(value).toBeTruthy()
    }
  })

  it('uses account credentials by default and only requests buyer input when selected', async () => {
    render(<ProductEditorPage />)
    fireEvent.change(screen.getByLabelText('Name'), {
      target: { value: 'Account' },
    })
    save()
    await waitFor(() => expect(run).toHaveBeenCalledTimes(1))
    expect(run.mock.calls[0][2]).toMatchObject({
      delivery_fields: [{ key: 'account_identifier' }, { key: 'password' }],
      buyer_input_schema: [],
    })
    fireEvent.change(screen.getByLabelText('Require contact email'), {
      target: { value: 'true' },
    })
    save()
    await waitFor(() => expect(run).toHaveBeenCalledTimes(2))
    expect(run.mock.calls[1][2].buyer_input_schema).toEqual([
      { key: 'contact_email', label: 'Contact email', required: true },
    ])
  })

  it('preserves existing delivery fields, buyer inputs, policy terms and cover while editing', async () => {
    product = productSchema.parse({
      id: 'sp_1',
      version: '9',
      name: 'Existing account',
      summary: 'Existing summary',
      specification: 'Existing specification',
      cover: { url: `/api/shop/covers/${'b'.repeat(64)}.jpg` },
      price: { amount_minor: '2500', currency: 'CNY', exponent: 2 },
      delivery_type: 'manual',
      availability: 'available',
      product_type: 'account',
      delivery_fields: [
        { key: 'login', label: 'Login', required: true },
        { key: 'secret', label: 'Secret', required: true },
      ],
      buyer_input_schema: [
        { key: 'contact_email', label: 'Email for handoff', required: false },
      ],
      usage_conditions: 'Custom usage terms',
      template_version: '4',
      delivery_policy: {
        mode: 'manual',
        public_terms: 'Custom delivery terms',
        sla: {
          clock: 'business',
          duration_minutes: 120,
          timezone: 'Asia/Shanghai',
          pause_on_missing_input: true,
          weekly_windows: [{ weekday: 1, start: '09:00', end: '18:00' }],
        },
      },
      view_policy: {
        duration_seconds: 7200,
        first_start: 'delivered_at',
        replacement_start: 'delivered_at',
        expired_action: 'deny_reveal',
        public_terms: 'Custom view terms',
      },
      warranty_policy: {
        mode: 'duration',
        start_event: 'first_delivered_at',
        duration_seconds: 3600,
        public_terms: 'Custom warranty',
      },
      entitlement_policy: {
        mode: 'described',
        public_terms: 'Custom validity',
      },
      refund_policy: {
        goods_refund_mode: 'full_only',
        execution: 'manual_original_channel',
        public_terms: 'Custom refund terms',
      },
      manual_capacity: 5,
      inventory_identity_domain: 'private-account',
      identity_normalization_version: '3',
      low_stock_threshold: 2,
    })
    const view = render(<ProductEditorPage productId='sp_1' />)
    expect(screen.getByTestId('cover-value')).toHaveTextContent(
      product.cover?.url ?? ''
    )
    expect(screen.getByLabelText('Product type')).toBeDisabled()
    fireEvent.change(screen.getByLabelText('Name'), {
      target: { value: 'Renamed account' },
    })
    save()
    await waitFor(() => expect(run).toHaveBeenCalledTimes(1))
    const body = run.mock.calls[0][2]
    expect(body).toMatchObject({
      expected_version: '9',
      name: 'Renamed account',
      product_type: 'account',
      cover: { url: product.cover?.url },
      delivery_fields: product.delivery_fields,
      buyer_input_schema: product.buyer_input_schema,
      usage_conditions: 'Custom usage terms',
      delivery_policy: product.delivery_policy,
      view_policy: product.view_policy,
      warranty_policy: product.warranty_policy,
      entitlement_policy: product.entitlement_policy,
      refund_policy: product.refund_policy,
      manual_capacity: 5,
      inventory_identity_domain: 'private-account',
      identity_normalization_version: '3',
      low_stock_threshold: 2,
    })

    product = {
      ...product,
      version: '10',
      usage_conditions: '',
      inventory_identity_domain: '',
      view_policy: {
        ...product.view_policy,
        duration_seconds: 0,
        public_terms: '',
      },
      warranty_policy: { ...product.warranty_policy, public_terms: '' },
      entitlement_policy: { ...product.entitlement_policy, public_terms: '' },
      refund_policy: { ...product.refund_policy, public_terms: '' },
    }
    view.rerender(<ProductEditorPage productId='sp_1' />)
    save()
    await waitFor(() => expect(run).toHaveBeenCalledTimes(2))
    expect(run.mock.calls[1][2]).toMatchObject({
      expected_version: '10',
      usage_conditions: '',
      view_policy: { duration_seconds: 0, public_terms: '' },
      warranty_policy: { public_terms: '' },
      entitlement_policy: { public_terms: '' },
      refund_policy: { public_terms: '' },
      inventory_identity_domain: 'shop:account',
    })
  })

  it('requires manual delivery capacity and disables saving during a cover upload', async () => {
    render(<ProductEditorPage />)
    fireEvent.change(screen.getByLabelText('Delivery type'), {
      target: { value: 'manual' },
    })
    expect(
      screen.getByLabelText('Manual delivery capacity')
    ).toBeInTheDocument()
    expect(
      screen.getByLabelText('Delivery duration in minutes')
    ).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Start cover upload' }))
    expect(screen.getByRole('button', { name: 'Save draft' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Finish cover upload' }))
    fireEvent.change(screen.getByLabelText('Name'), {
      target: { value: 'Manual account' },
    })
    fireEvent.click(
      screen.getByRole('button', { name: 'Select uploaded cover' })
    )
    save()
    await waitFor(() => expect(run).toHaveBeenCalledTimes(1))
    expect(run.mock.calls[0][2]).toMatchObject({
      delivery_type: 'manual',
      manual_capacity: 10,
      delivery_policy: {
        mode: 'manual',
        sla: {
          clock: 'elapsed',
          duration_minutes: 1440,
          timezone: 'Asia/Shanghai',
        },
      },
      cover: { url: `/api/shop/covers/${'a'.repeat(64)}.jpg` },
    })
  })

  it('resets unsaved form state when a different product or admin session opens', () => {
    product = productSchema.parse({
      id: 'sp_a',
      version: '1',
      name: 'Account A',
      price: { amount_minor: '100', currency: 'CNY', exponent: 2 },
      delivery_type: 'automatic',
      availability: 'available',
    })
    const view = render(<ProductEditorPage productId='sp_a' />)
    fireEvent.change(screen.getByLabelText('Name'), {
      target: { value: 'Unsaved edit' },
    })
    product = productSchema.parse({
      id: 'sp_b',
      version: '1',
      name: 'Account B',
      price: { amount_minor: '200', currency: 'CNY', exponent: 2 },
      delivery_type: 'automatic',
      availability: 'available',
    })
    view.rerender(<ProductEditorPage productId='sp_b' />)
    expect(screen.getByLabelText('Name')).toHaveValue('Account B')
    fireEvent.change(screen.getByLabelText('Name'), {
      target: { value: 'Another unsaved edit' },
    })
    identity = 'next-admin-session'
    view.rerender(<ProductEditorPage productId='sp_b' />)
    expect(screen.getByLabelText('Name')).toHaveValue('Account B')
  })
})
