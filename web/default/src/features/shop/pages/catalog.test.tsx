import { fireEvent, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { expect, it, vi } from 'vitest'

import { CatalogPage } from './catalog'

const navigate = vi.hoisted(() => vi.fn())
vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => navigate,
  Link: (props: { to: string; children: ReactNode }) => (
    <a href={props.to}>{props.children}</a>
  ),
}))
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))
vi.mock('../hooks/use-shop', () => ({
  useShopQuery: (path: string) => {
    if (path.startsWith('/api/shop/products')) {
      return { data: { items: [], total: 0, page_size: 20 } }
    }
    if (path.endsWith('/config')) return { data: { new_orders_enabled: false } }
    return { data: [{ id: 'cards', name: 'Cards' }] }
  },
}))

it('keeps the orders link reachable without offering unusable filters on an empty shelf', () => {
  render(<CatalogPage page={1} />)
  expect(screen.getByRole('link', { name: 'My orders' })).toHaveAttribute(
    'href',
    '/shop/orders'
  )
  expect(screen.getByRole('status')).toHaveTextContent(
    'New purchases are paused'
  )
  expect(screen.queryByRole('search')).not.toBeInTheDocument()
  expect(
    screen.queryByRole('navigation', { name: 'Pagination' })
  ).not.toBeInTheDocument()
})

it('clears an unsuccessful search, but keeps the filters when recovering an empty later page', () => {
  const { rerender } = render(
    <CatalogPage q='missing' category_id='cards' page={1} />
  )
  fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))
  expect(navigate).toHaveBeenLastCalledWith(
    expect.objectContaining({
      search: { q: undefined, category_id: undefined, page: 1 },
    })
  )
  rerender(<CatalogPage q='another' category_id='cards' page={4} />)
  expect(screen.getByRole('searchbox')).toHaveValue('another')
  fireEvent.click(screen.getByRole('button', { name: 'Go to first page' }))
  expect(navigate).toHaveBeenLastCalledWith(
    expect.objectContaining({
      search: { q: 'another', category_id: 'cards', page: 1 },
    })
  )
})
