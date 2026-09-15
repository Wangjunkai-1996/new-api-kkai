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
import { describe, expect, test, vi } from 'vitest'

import { GeneralError } from './general-error'

vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => vi.fn(),
  useRouter: () => ({ history: { go: vi.fn() } }),
}))

describe('chunk failure recovery', () => {
  test.each([
    Object.assign(new Error('Loading chunk 42 failed.'), {
      name: 'ChunkLoadError',
    }),
    new Error('Loading CSS chunk 42 failed.'),
    new Error('Failed to fetch dynamically imported module: /static/old.js'),
    new Error('Importing a module script failed.'),
    new Error('error loading dynamically imported module'),
  ])('offers explicit page refresh for $message', (error) => {
    render(<GeneralError error={error} />)
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeVisible()
    expect(screen.queryByText('500')).not.toBeInTheDocument()
  })

  test('keeps refresh available in a minimal error boundary', () => {
    render(
      <GeneralError
        minimal
        error={Object.assign(new Error(), { name: 'ChunkLoadError' })}
      />
    )
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeVisible()
  })

  test('keeps normal error navigation without treating an API failure as a missing chunk', () => {
    render(<GeneralError error={{ response: { status: 429 } }} />)
    expect(
      screen.queryByRole('button', { name: 'Refresh' })
    ).not.toBeInTheDocument()
    expect(screen.getByText('Too many requests')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Go Back' })).toBeVisible()
  })
})
