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
import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { WalletPaymentError } from '../payment-flow-api'
import { WalletRequestError } from './wallet-request-error'

afterEach(() => vi.useRealTimers())

it('announces a fixed retry time once and only unlocks the button at the deadline', () => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-10-25T04:00:00Z'))
  const retry = vi.fn()
  render(
    <WalletRequestError
      error={
        new WalletPaymentError('WALLET_RATE_LIMITED', undefined, {
          status: 429,
          retryAt: Date.now() + 60_000,
        })
      }
      onRetry={retry}
    />
  )
  const alert = screen.getByRole('alert')
  const announcement = alert.textContent
  const button = screen.getByRole('button', { name: 'Retry' })
  expect(announcement).toContain('Too many requests. Try again at')
  expect(alert).not.toContainElement(button)
  expect(button).toBeDisabled()
  act(() => {
    vi.advanceTimersByTime(59_000)
  })
  expect(alert.textContent).toBe(announcement)
  expect(button).toBeDisabled()
  expect(retry).not.toHaveBeenCalled()
  act(() => {
    vi.advanceTimersByTime(1000)
  })
  expect(alert.textContent).toBe(announcement)
  expect(button).toBeEnabled()
  fireEvent.click(button)
  expect(retry).toHaveBeenCalledTimes(1)
})

it('uses the cached Axios failure time and preserves the stale-data explanation', () => {
  vi.useFakeTimers()
  vi.setSystemTime(200_000)
  render(
    <WalletRequestError
      error={{
        isAxiosError: true,
        response: {
          status: 429,
          headers: { 'retry-after': '60' },
          data: {},
        },
      }}
      errorUpdatedAt={170_000}
      onRetry={vi.fn()}
      message='The displayed totals may be out of date.'
    />
  )
  expect(screen.getByRole('alert')).toHaveTextContent(
    'The displayed totals may be out of date.'
  )
  expect(screen.getByRole('button', { name: 'Retry' })).toBeDisabled()
  act(() => {
    vi.advanceTimersByTime(30_000)
  })
  expect(screen.getByRole('button', { name: 'Retry' })).toBeEnabled()
})

it('does not invent a cooldown or resend a request when 429 has no reliable retry metadata', () => {
  const retry = vi.fn()
  render(
    <WalletRequestError
      error={{
        isAxiosError: true,
        response: {
          status: 429,
          headers: {},
          data: '',
        },
      }}
      onRetry={retry}
    />
  )
  expect(screen.getByRole('alert')).toHaveTextContent(
    'Too many requests. Please wait before trying again.'
  )
  expect(screen.getByRole('button', { name: 'Retry' })).toBeEnabled()
  expect(retry).not.toHaveBeenCalled()
})
