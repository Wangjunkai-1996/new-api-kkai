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

import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { USAGE_TOOLS } from '../catalog'
import { UsageGuidePage } from '../index'

vi.mock('../components/quick-setup-dialog', () => ({
  QuickSetupDialog: (props: { preset: string | null }) =>
    props.preset ? <div role='dialog'>{props.preset}</div> : null,
}))

vi.mock('@tanstack/react-router', () => ({
  Link: (props: { to: string; children: ReactNode }) => (
    <a href={props.to}>{props.children}</a>
  ),
}))

describe('usage guide', () => {
  it('keeps the two quick setup presets and all tools in the guide', async () => {
    const user = userEvent.setup()
    render(<UsageGuidePage />)

    expect(screen.getByText('GPT / Codex')).toBeInTheDocument()
    expect(screen.getAllByText('Claude Code').length).toBeGreaterThan(0)
    expect(screen.getByText('ChatGPT')).toBeInTheDocument()
    expect(screen.queryByText('Codex CLI')).not.toBeInTheDocument()
    const featured = within(
      screen.getByRole('region', { name: 'Prepare your tools first' })
    )
    expect(featured.getByText('Claude Desktop')).toBeInTheDocument()
    expect(featured.queryByText('Claude Code')).not.toBeInTheDocument()
    expect(
      featured
        .getAllByRole('button', { name: 'macOS download page' })
        .some(
          (button) =>
            button.getAttribute('href') === 'https://claude.com/download'
        )
    ).toBe(true)
    expect(
      screen
        .getAllByRole('button', { name: 'macOS official page' })
        .some(
          (button) =>
            button.getAttribute('href') === 'https://learn.chatgpt.com/docs/app'
        )
    ).toBe(true)
    await user.click(screen.getByRole('tab', { name: 'Tool downloads' }))

    for (const tool of USAGE_TOOLS) {
      expect(screen.getAllByText(tool.nameKey).length).toBeGreaterThan(0)
    }
    expect(screen.getByText('Codex CLI')).toBeInTheDocument()
    expect(screen.getAllByText('Claude Code').length).toBeGreaterThan(0)
  })

  it('opens a quick setup dialog from a card without unmounting the page', async () => {
    const user = userEvent.setup()
    render(<UsageGuidePage />)

    await user.click(
      screen.getByRole('button', { name: 'Configure GPT in one click' })
    )
    expect(screen.getByRole('dialog')).toHaveTextContent('gpt')
    expect(screen.getByText('Start using AI from here')).toBeInTheDocument()
  })

  it('filters tools by platform', async () => {
    const user = userEvent.setup()
    render(<UsageGuidePage />)
    await user.click(screen.getByRole('tab', { name: 'Tool downloads' }))
    const trigger = screen.getByRole('combobox', {
      name: 'Select operating system',
    })
    await user.click(trigger)
    await user.click(screen.getByRole('option', { name: 'Windows' }))

    expect(screen.getByText('Claude Desktop')).toBeInTheDocument()
    expect(screen.getByText('Codex CLI')).toBeInTheDocument()
    expect(
      screen
        .getAllByRole('button', { name: 'Windows install guide' })
        .some(
          (button) =>
            button.getAttribute('href') ===
            'https://learn.chatgpt.com/docs/codex/cli'
        )
    ).toBe(true)
    expect(
      screen.queryByRole('button', { name: 'macOS install guide' })
    ).not.toBeInTheDocument()
  })

  it('shows the Cherry Studio three-step guide and links existing keys to management', async () => {
    const user = userEvent.setup()
    render(<UsageGuidePage />)

    const keysLink = screen.getByRole('link', { name: 'Open API Keys' })
    expect(keysLink).toHaveAttribute('href', '/keys')
    await user.click(
      screen.getByRole('button', { name: 'View connection guide' })
    )

    expect(
      screen.getByText('Cherry Studio connection guide')
    ).toBeInTheDocument()
    expect(
      screen.getByText('Download and open Cherry Studio')
    ).toBeInTheDocument()
    expect(screen.getByText('Add the gateway and API key')).toBeInTheDocument()
    expect(
      screen.getByText('Choose a model and start chatting')
    ).toBeInTheDocument()
  })
})
