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
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import React, { type AnchorHTMLAttributes } from 'react'
import { beforeAll, describe, expect, test, vi } from 'vitest'

import { LinkAiLegalConsent } from './legal-consent'

type MockLinkProps = AnchorHTMLAttributes<HTMLAnchorElement> & {
  to: string
}

vi.mock('@tanstack/react-router', () => ({
  Link: ({ to, ...props }: MockLinkProps) =>
    React.createElement('a', { ...props, href: to }),
}))

describe('LinkAI legal consent', () => {
  beforeAll(() => {
    i18next.addResourceBundle('en', 'translation', {
      'I agree to the': 'I agree to the',
      'Terms of Service': 'Terms of Service',
      and: 'and',
      'Privacy Policy': 'Privacy Policy',
    })
  })

  test('uses the same compact consent row and updates the checked state', async () => {
    const user = userEvent.setup()
    const onCheckedChange = vi.fn()

    render(
      <LinkAiLegalConsent
        id='legal-consent-test'
        status={{
          user_agreement_enabled: true,
          privacy_policy_enabled: true,
        }}
        checked={false}
        onCheckedChange={onCheckedChange}
      />
    )

    const checkbox = screen.getByRole('checkbox', {
      name: /I agree to the Terms of Service and Privacy Policy/i,
    })
    const row = checkbox.closest('label')

    expect(row).toHaveClass('flex', 'items-center', 'px-4')
    expect(row).not.toHaveClass('bg-muted/40')
    expect(
      screen.getByRole('link', { name: 'Terms of Service' })
    ).toHaveAttribute('href', '/user-agreement')
    expect(
      screen.getByRole('link', { name: 'Privacy Policy' })
    ).toHaveAttribute('href', '/privacy-policy')

    await user.click(checkbox)
    expect(onCheckedChange).toHaveBeenCalledWith(true)
  })

  test('only renders legal documents enabled by status', () => {
    render(
      <LinkAiLegalConsent
        id='privacy-consent-test'
        status={{ privacy_policy_enabled: true }}
        checked={false}
        onCheckedChange={() => undefined}
      />
    )

    expect(screen.queryByRole('link', { name: 'Terms of Service' })).toBeNull()
    expect(screen.getByRole('link', { name: 'Privacy Policy' })).toBeVisible()
  })
})
