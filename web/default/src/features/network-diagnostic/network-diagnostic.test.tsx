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
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { runNetworkCheck } from './network-check'
import { NetworkDiagnosticWidget } from './network-diagnostic'

vi.mock('./network-check', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./network-check')>()),
  runNetworkCheck: vi.fn(),
}))

beforeEach(() => {
  // JSDOM does not implement pointer capture.
  HTMLElement.prototype.setPointerCapture = vi.fn()
  HTMLElement.prototype.hasPointerCapture = vi.fn().mockReturnValue(true)
  HTMLElement.prototype.releasePointerCapture = vi.fn()
})

afterEach(() => {
  window.localStorage.clear()
  for (const method of [
    'setPointerCapture',
    'hasPointerCapture',
    'releasePointerCapture',
  ]) {
    Reflect.deleteProperty(HTMLElement.prototype, method)
  }
})

it('shows measured latency and starts fresh after closing', async () => {
  vi.mocked(runNetworkCheck).mockImplementation(async (_signal, onSample) => {
    const samples = [280, 290, 295, 310, 290].map((ms) => ({
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

  expect(await screen.findByText('293')).toBeVisible()
  expect(screen.getByText('Connection excellent')).toBeVisible()
  await user.click(
    screen.getByRole('button', { name: 'Close network diagnostics' })
  )
  await user.click(
    screen.getByRole('button', { name: 'Open network diagnostics' })
  )

  expect(screen.getByText('--')).toBeVisible()
  expect(screen.getByText('Ready to test')).toBeVisible()
})

it.each([false, true])(
  'drags the collapsed launcher without opening (mobile: %s)',
  async (mobile) => {
    const originalMatchMedia = window.matchMedia
    vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({
      ...originalMatchMedia(query),
      matches: query.includes('max-width: 500px')
        ? mobile
        : originalMatchMedia(query).matches,
    }))
    const user = userEvent.setup()
    const view = render(<NetworkDiagnosticWidget />)
    const launcher = screen.getByRole('button', {
      name: 'Open network diagnostics',
    })
    vi.spyOn(launcher, 'getBoundingClientRect').mockReturnValue(
      new DOMRect(700, 400, 104, 116)
    )
    launcher.setPointerCapture = vi.fn()
    launcher.hasPointerCapture = vi.fn().mockReturnValue(true)
    launcher.releasePointerCapture = vi.fn()

    fireEvent.pointerDown(launcher, {
      button: 0,
      isPrimary: true,
      pointerId: 1,
      clientX: 720,
      clientY: 420,
    })
    fireEvent.pointerMove(launcher, {
      pointerId: 1,
      clientX: 520,
      clientY: 320,
    })
    fireEvent.pointerUp(launcher, { pointerId: 1, clientX: 520, clientY: 320 })
    fireEvent.click(launcher, { detail: 1 })

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(launcher).toHaveStyle({ left: '500px', top: '300px' })
    expect(launcher.releasePointerCapture).toHaveBeenCalledWith(1)
    await user.click(launcher)
    expect(
      screen.getByRole('dialog', { name: 'Network diagnostics' })
    ).toBeVisible()
    await user.click(
      screen.getByRole('button', { name: 'Close network diagnostics' })
    )
    await waitFor(() => expect(launcher).toBeVisible())
    expect(launcher).toHaveStyle({ left: '500px', top: '300px' })

    view.unmount()
    render(<NetworkDiagnosticWidget />)
    expect(
      screen.getByRole('button', { name: 'Open network diagnostics' })
    ).toHaveStyle({ left: '500px', top: '300px' })
  }
)

it('keeps the collapsed launcher on screen when dragged or the viewport shrinks', () => {
  render(<NetworkDiagnosticWidget />)
  const launcher = screen.getByRole('button', {
    name: 'Open network diagnostics',
  })
  vi.spyOn(launcher, 'getBoundingClientRect').mockReturnValue(
    new DOMRect(700, 400, 104, 116)
  )
  launcher.setPointerCapture = vi.fn()
  launcher.hasPointerCapture = vi.fn().mockReturnValue(true)
  launcher.releasePointerCapture = vi.fn()
  fireEvent.pointerDown(launcher, {
    button: 0,
    isPrimary: true,
    pointerId: 1,
    clientX: 720,
    clientY: 420,
  })
  fireEvent.pointerMove(launcher, {
    pointerId: 1,
    clientX: 5000,
    clientY: 5000,
  })
  fireEvent.pointerUp(launcher, { pointerId: 1, clientX: 5000, clientY: 5000 })
  expect(launcher).toHaveStyle({
    left: `${window.innerWidth - 116}px`,
    top: `${window.innerHeight - 128}px`,
  })

  vi.spyOn(window, 'innerWidth', 'get').mockReturnValue(390)
  vi.spyOn(window, 'innerHeight', 'get').mockReturnValue(600)
  fireEvent(window, new Event('resize'))
  expect(launcher).toHaveStyle({ left: '274px', top: '472px' })
})

it('allows a click with slight pointer movement and keyboard movement', () => {
  render(<NetworkDiagnosticWidget />)
  const launcher = screen.getByRole('button', {
    name: 'Open network diagnostics',
  })
  vi.spyOn(launcher, 'getBoundingClientRect').mockReturnValue(
    new DOMRect(700, 400, 104, 116)
  )
  launcher.setPointerCapture = vi.fn()
  launcher.hasPointerCapture = vi.fn().mockReturnValue(true)
  launcher.releasePointerCapture = vi.fn()
  fireEvent.keyDown(launcher, { key: 'ArrowLeft' })
  expect(launcher).toHaveStyle({ left: '688px', top: '400px' })
  fireEvent.pointerDown(launcher, {
    button: 0,
    isPrimary: true,
    pointerId: 1,
    clientX: 720,
    clientY: 420,
  })
  fireEvent.pointerMove(launcher, { pointerId: 1, clientX: 723, clientY: 422 })
  fireEvent.pointerUp(launcher, { pointerId: 1, clientX: 723, clientY: 422 })
  fireEvent.click(launcher, { detail: 1 })
  expect(
    screen.getByRole('dialog', { name: 'Network diagnostics' })
  ).toBeVisible()
})

it('keeps a dragged panel position after closing and reopening', async () => {
  const user = userEvent.setup()
  render(<NetworkDiagnosticWidget />)
  await user.click(
    screen.getByRole('button', { name: 'Open network diagnostics' })
  )
  const panel = screen.getByRole('dialog', { name: 'Network diagnostics' })
  vi.spyOn(panel, 'getBoundingClientRect').mockReturnValue(
    new DOMRect(500, 250, 380, 420)
  )
  const header = panel.querySelector('header')
  if (!header) throw new Error('Missing diagnostic header')
  fireEvent.pointerDown(header, {
    button: 0,
    isPrimary: true,
    pointerId: 1,
    clientX: 550,
    clientY: 280,
  })
  fireEvent.pointerMove(header, { pointerId: 1, clientX: 500, clientY: 230 })
  fireEvent.pointerUp(header, { pointerId: 1, clientX: 500, clientY: 230 })
  const saved = { left: panel.style.left, top: panel.style.top }
  await user.click(
    screen.getByRole('button', { name: 'Close network diagnostics' })
  )
  await user.click(
    screen.getByRole('button', { name: 'Open network diagnostics' })
  )
  expect(
    screen.getByRole('dialog', { name: 'Network diagnostics' })
  ).toHaveStyle(saved)
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
