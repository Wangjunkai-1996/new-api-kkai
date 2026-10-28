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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { overviewSchema, settingsSchema } from '@/features/shop/schemas'

import { AdminOverviewPage, AdminSettingsPage } from './overview'

const { query, run } = vi.hoisted(() => ({ query: vi.fn(), run: vi.fn() }))
vi.mock('@/features/shop/hooks/use-shop', () => ({
  useShopQuery: query,
  useShopAction: () => ({ run, pending: false, error: null }),
}))
vi.mock('@tanstack/react-router', () => ({
  Link: (props: { to: string; children: ReactNode }) => (
    <a href={props.to}>{props.children}</a>
  ),
}))

const settings = {
  version: '7',
  new_orders_enabled: false,
  provider: 'epay',
  provider_account_ref: 'epay:custom-merchant',
  environment: 'test',
  currency: 'CNY',
  currency_exponent: 2,
  payment_ready: true,
  payment_methods: ['alipay', 'wxpay'],
  reservation_minutes: 23,
  pending_order_limit: 4,
  min_amount_minor: '19',
  max_amount_minor: '98765',
  retention_policy: {
    secret_after_delivery_seconds: 1234567,
    buyer_input_after_closure_seconds: 2345678,
    evidence_after_settlement_seconds: 3456789,
    audit_after_settlement_seconds: 4567890,
    idempotency_after_closure_seconds: 5678901,
  },
  reminder_policy: {
    payment_unknown_seconds: 310,
    delivery_exception_seconds: 910,
    refund_verification_seconds: 86410,
  },
}

beforeEach(() => {
  query.mockReturnValue({
    data: settings,
    isPending: false,
    error: null,
    refetch: vi.fn(),
  })
  run.mockResolvedValue(settings)
})

describe('shop administration settings', () => {
  it('keeps the overview focused on operations without loading or rendering settings', () => {
    query.mockReturnValue({
      data: undefined,
      isPending: false,
      error: null,
      refetch: vi.fn(),
    })
    render(<AdminOverviewPage />)
    expect(
      screen.queryByRole('switch', { name: 'Enable new purchases' })
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: 'Open order queue' })
    ).toHaveAttribute('href', '/shop-admin/orders')
    expect(query).toHaveBeenCalledWith(
      '/api/admin/shop/overview',
      overviewSchema
    )
    expect(
      query.mock.calls.every(([path]) => path === '/api/admin/shop/overview')
    ).toBe(true)
  })

  it('changes sales while preserving every existing hidden payment and operating value', async () => {
    render(<AdminSettingsPage />)
    expect(
      screen.getByText('Advanced settings').closest('details')
    ).not.toHaveAttribute('open')
    expect(screen.queryByLabelText('Payment provider')).not.toBeInTheDocument()
    expect(
      screen.queryByLabelText('Provider account reference')
    ).not.toBeInTheDocument()
    fireEvent.click(
      screen.getByRole('switch', { name: 'Enable new purchases' })
    )
    fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))
    await waitFor(() => expect(run).toHaveBeenCalledTimes(1))
    expect(run).toHaveBeenCalledWith(
      '/api/admin/shop/settings',
      settingsSchema,
      {
        expected_version: '7',
        new_orders_enabled: true,
        provider: settings.provider,
        provider_account_ref: settings.provider_account_ref,
        environment: settings.environment,
        currency: settings.currency,
        reservation_minutes: settings.reservation_minutes,
        pending_order_limit: settings.pending_order_limit,
        min_amount_minor: settings.min_amount_minor,
        max_amount_minor: settings.max_amount_minor,
        retention_policy: settings.retention_policy,
        reminder_policy: settings.reminder_policy,
        reason: 'Store settings updated from admin console',
      },
      { method: 'PATCH' }
    )
  })

  it('rejects inverted money limits and opens advanced settings for correction', async () => {
    render(<AdminSettingsPage />)
    fireEvent.change(screen.getByLabelText('Minimum order amount (CNY)'), {
      target: { value: '1000.00' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))
    await screen.findByRole('alert')
    expect(run).not.toHaveBeenCalled()
    expect(
      screen.getByText('Advanced settings').closest('details')
    ).toHaveAttribute('open')
  })

  it('prevents starting unready payments while still allowing an active store to pause', async () => {
    query.mockReturnValue({
      data: { ...settings, payment_ready: false },
      isPending: false,
    })
    const view = render(<AdminSettingsPage />)
    expect(
      screen.getByRole('switch', { name: 'Enable new purchases' })
    ).toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByRole('status')).toHaveTextContent(
      'Complete the existing payment setup before enabling purchases.'
    )
    query.mockReturnValue({
      data: {
        ...settings,
        version: '8',
        new_orders_enabled: true,
        payment_ready: false,
      },
      isPending: false,
    })
    view.rerender(<AdminSettingsPage />)
    expect(
      screen.getByRole('switch', { name: 'Enable new purchases' })
    ).not.toHaveAttribute('aria-disabled', 'true')
    fireEvent.click(
      screen.getByRole('switch', { name: 'Enable new purchases' })
    )
    fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))
    await waitFor(() => expect(run).toHaveBeenCalledTimes(1))
    expect(run.mock.calls[0][2]).toMatchObject({
      expected_version: '8',
      new_orders_enabled: false,
    })
  })

  it('requires an explicit action before switching the existing merchant reference', async () => {
    query.mockReturnValue({
      data: {
        ...settings,
        payment_ready: false,
        payment_config_ready: true,
        payment_account_changed: true,
        current_provider_account_ref: 'epay:new-merchant',
      },
      isPending: false,
    })
    render(<AdminSettingsPage />)
    expect(
      screen.getByRole('switch', { name: 'Enable new purchases' })
    ).toHaveAttribute('aria-disabled', 'true')
    expect(
      screen.getByRole('button', { name: 'Use current payment account' })
    ).toBeEnabled()
    fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))
    await waitFor(() => expect(run).toHaveBeenCalledTimes(1))
    expect(run.mock.calls[0][2].provider_account_ref).toBe(
      'epay:custom-merchant'
    )

    run.mockClear()
    fireEvent.click(
      screen.getByRole('button', { name: 'Use current payment account' })
    )
    expect(screen.getByText('Ready after saving')).toBeInTheDocument()
    expect(
      screen.queryByText('Ready to accept payments')
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('switch', { name: 'Enable new purchases' })
    ).not.toHaveAttribute('aria-disabled', 'true')
    fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))
    await waitFor(() => expect(run).toHaveBeenCalledTimes(1))
    expect(run.mock.calls[0][2].provider_account_ref).toBe('epay:new-merchant')
  })
})
