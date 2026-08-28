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
import React, { type AnchorHTMLAttributes } from 'react'
import { describe, expect, test, vi } from 'vitest'

import { LinkAiAuthShell } from './auth-shell'

type MockLinkProps = AnchorHTMLAttributes<HTMLAnchorElement> & {
  to: string
}

vi.mock('@tanstack/react-router', () => ({
  Link: ({ to, ...props }: MockLinkProps) =>
    React.createElement('a', { ...props, href: to }),
}))

describe('LinkAI auth shell proportions', () => {
  test('keeps the desktop panel and brand mark compact', () => {
    const { container } = render(
      <LinkAiAuthShell>
        <p>Auth content</p>
      </LinkAiAuthShell>
    )

    const panel = container.querySelector('[data-linkai-auth-panel]')
    const panelLayout = panel?.parentElement
    const logo = container.querySelector('[data-linkai-auth-logo] img')

    expect(panelLayout).toHaveClass('max-w-[600px]')
    expect(logo).toHaveClass('h-8')
    expect(logo).not.toHaveClass('sm:h-10')
    expect(screen.getByText('Auth content')).toBeVisible()
  })

  test('uses the same compact height for the split registration logo', () => {
    const { container } = render(
      <LinkAiAuthShell splitLogo>
        <p>Registration content</p>
      </LinkAiAuthShell>
    )

    const logoImages = container.querySelectorAll('[data-linkai-auth-logo] img')

    expect(logoImages).toHaveLength(2)
    expect(logoImages[0]).toHaveClass('h-8')
    expect(logoImages[1]).toHaveClass('h-[30px]')
  })
})
