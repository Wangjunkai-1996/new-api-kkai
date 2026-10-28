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
import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ShopEventSummary } from './common'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

describe('order timeline summaries', () => {
  it.each([
    {
      event_type: 'order_created',
      public_summary: '',
      expected: 'Order created',
    },
    {
      event_type: 'payment_received',
      public_summary: '  ',
      expected: 'Payment received',
    },
    { event_type: 'delivered', public_summary: '', expected: 'Delivered' },
    {
      event_type: 'order_closed',
      public_summary: 'Buyer requested cancellation',
      expected: 'Buyer requested cancellation',
    },
    {
      event_type: 'delivery_exception',
      public_summary: 'SHOP_SECRET_UNAVAILABLE',
      expected: 'Delivery needs attention',
    },
    {
      event_type: 'future_internal_code',
      public_summary: '',
      expected: 'Order updated',
    },
  ])(
    'renders a readable $event_type event',
    ({ event_type, public_summary, expected }) => {
      render(<ShopEventSummary event={{ event_type, public_summary }} />)
      expect(screen.getByText(expected)).toBeVisible()
      expect(screen.queryByText(event_type)).not.toBeInTheDocument()
    }
  )
})
