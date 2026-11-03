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
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { runNetworkCheck } from './network-check'
import { NetworkDiagnosticWidget } from './network-diagnostic'

vi.mock('./network-check', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./network-check')>()),
  runNetworkCheck: vi.fn(),
}))

afterEach(() => {
  window.localStorage.clear()
})

it('shows measured latency and starts fresh after closing', async () => {
  vi.mocked(runNetworkCheck).mockImplementation(async (_signal, onSample) => {
    const samples = [90, 100, 110, 100, 100].map((ms) => ({
      kind: 'success' as const,
      ms,
    }))
    onSample(samples)
    return { samples, end: 'complete' }
  })
  const user = userEvent.setup()
  render(<NetworkDiagnosticWidget />)

  await user.click(
    screen.getByRole('button', { name: 'Open network diagnostics' })
  )
  await user.click(screen.getByRole('button', { name: 'Start check' }))

  expect(await screen.findByText('100')).toBeVisible()
  expect(screen.getByText('Connection smooth')).toBeVisible()
  await user.click(
    screen.getByRole('button', { name: 'Close network diagnostics' })
  )
  await user.click(
    screen.getByRole('button', { name: 'Open network diagnostics' })
  )

  expect(screen.getByText('--')).toBeVisible()
  expect(screen.getByText('Ready to test')).toBeVisible()
})

it('aborts an in-flight check when closed', async () => {
  let aborted = false
  vi.mocked(runNetworkCheck).mockImplementation(
    (signal) =>
      new Promise((_resolve, reject) => {
        signal.addEventListener('abort', () => {
          aborted = true
          reject(new DOMException('Aborted', 'AbortError'))
        })
      })
  )
  const user = userEvent.setup()
  render(<NetworkDiagnosticWidget />)

  await user.click(
    screen.getByRole('button', { name: 'Open network diagnostics' })
  )
  await user.click(screen.getByRole('button', { name: 'Start check' }))
  await user.click(
    screen.getByRole('button', { name: 'Close network diagnostics' })
  )

  await waitFor(() => expect(aborted).toBe(true))
  await user.click(
    screen.getByRole('button', { name: 'Open network diagnostics' })
  )
  expect(screen.getByText('Ready to test')).toBeVisible()
})

it.each([
  ['invalid-response', 'The API did not return a valid test response'],
  ['unavailable', 'Browser access may be restricted or the connection failed'],
] as const)(
  'explains %s without judging the connection',
  async (end, detail) => {
    vi.mocked(runNetworkCheck).mockResolvedValue({
      samples: [{ kind: 'failed' }],
      end,
    })
    const user = userEvent.setup()
    render(<NetworkDiagnosticWidget />)

    await user.click(
      screen.getByRole('button', { name: 'Open network diagnostics' })
    )
    await user.click(screen.getByRole('button', { name: 'Start check' }))

    expect(await screen.findByText(detail)).toBeVisible()
    expect(screen.getByText('Unable to diagnose')).toBeVisible()
  }
)

it('swipes the mobile sheet down from its grip', async () => {
  const originalMatchMedia = window.matchMedia
  vi.spyOn(window, 'matchMedia').mockImplementation((query) => {
    const media = originalMatchMedia(query)
    return {
      ...media,
      matches: query.includes('max-width: 500px') || media.matches,
    }
  })
  const user = userEvent.setup()
  render(<NetworkDiagnosticWidget />)
  await user.click(
    screen.getByRole('button', { name: 'Open network diagnostics' })
  )

  const grip = screen.getByRole('button', { name: 'Move network diagnostics' })
  const header = grip.closest('header')
  if (!header) throw new Error('Missing diagnostic header')
  const panel = screen.getByRole('dialog', { name: 'Network diagnostics' })
  header.setPointerCapture = vi.fn()
  header.hasPointerCapture = vi.fn().mockReturnValue(true)
  header.releasePointerCapture = vi.fn()

  fireEvent.pointerDown(grip, {
    button: 0,
    isPrimary: true,
    pointerId: 1,
    clientX: 100,
    clientY: 20,
  })
  fireEvent.pointerMove(header, { pointerId: 1, clientX: 100, clientY: 140 })
  expect(panel.style.getPropertyValue('--network-swipe-y')).toBe('120px')
  fireEvent.pointerUp(header, { pointerId: 1, clientX: 100, clientY: 140 })
  expect(panel).toHaveAttribute('data-closing', 'true')
})
