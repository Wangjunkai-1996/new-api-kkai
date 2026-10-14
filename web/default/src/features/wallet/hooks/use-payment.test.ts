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
import { act, renderHook } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { PAYMENT_TYPES } from '../constants'
import type { AmountResponse } from '../types'
import { requestPaymentAmount, usePayment } from './use-payment'

const quoteMocks = vi.hoisted(() => ({ calculateAmount: vi.fn() }))

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  calculateAmount: quoteMocks.calculateAmount,
}))

beforeEach(() => {
  quoteMocks.calculateAmount.mockReset()
})

describe('payment amount routing', () => {
  test('uses the dedicated Waffo amount calculator', async () => {
    const calls: string[] = []
    const amount = await requestPaymentAmount(120, PAYMENT_TYPES.WAFFO, {
      regular: async () => {
        calls.push('regular')
        return { success: true, data: '1' }
      },
      stripe: async () => {
        calls.push('stripe')
        return { success: true, data: '2' }
      },
      waffo: async (request) => {
        calls.push(`waffo:${request.amount}`)
        return { success: true, data: '18.75' }
      },
      waffoPancake: async () => {
        calls.push('pancake')
        return { success: true, data: '4' }
      },
    })

    expect(amount).toBe(18.75)
    expect(calls).toEqual(['waffo:120'])
  })
})

describe('payment quote state', () => {
  test('ignores an older quote while the latest request is still loading', async () => {
    let resolveOld!: (response: AmountResponse) => void
    let resolveLatest!: (response: AmountResponse) => void
    const oldRequest = new Promise<AmountResponse>((resolve) => {
      resolveOld = resolve
    })
    const latestRequest = new Promise<AmountResponse>((resolve) => {
      resolveLatest = resolve
    })
    quoteMocks.calculateAmount
      .mockReturnValueOnce(oldRequest)
      .mockReturnValueOnce(latestRequest)

    const { result } = renderHook(() => usePayment())
    let oldQuote!: Promise<number>
    let latestQuote!: Promise<number>
    act(() => {
      oldQuote = result.current.calculatePaymentAmount(100, 'alipay')
      latestQuote = result.current.calculatePaymentAmount(200, 'alipay')
    })

    await act(async () => {
      resolveOld({ success: true, data: '98' })
      expect(await oldQuote).toBe(0)
    })
    expect(result.current.amount).toBe(0)
    expect(result.current.calculating).toBe(true)

    await act(async () => {
      resolveLatest({ success: true, data: '192' })
      expect(await latestQuote).toBe(192)
    })
    expect(result.current.amount).toBe(192)
    expect(result.current.calculating).toBe(false)
  })

  test('clears the previous amount and invalidates a pending quote', async () => {
    let resolvePending!: (response: AmountResponse) => void
    const pendingRequest = new Promise<AmountResponse>((resolve) => {
      resolvePending = resolve
    })
    quoteMocks.calculateAmount
      .mockResolvedValueOnce({ success: true, data: '98' })
      .mockReturnValueOnce(pendingRequest)

    const { result } = renderHook(() => usePayment())
    await act(async () => {
      expect(await result.current.calculatePaymentAmount(100, 'alipay')).toBe(
        98
      )
    })
    expect(result.current.amount).toBe(98)

    let pendingQuote!: Promise<number>
    act(() => {
      pendingQuote = result.current.calculatePaymentAmount(200, 'alipay')
    })
    expect(result.current.amount).toBe(0)
    expect(result.current.calculating).toBe(true)

    act(() => result.current.clearPaymentAmount())
    expect(result.current.calculating).toBe(false)

    await act(async () => {
      resolvePending({ success: true, data: '192' })
      expect(await pendingQuote).toBe(0)
    })
    expect(result.current.amount).toBe(0)
    expect(result.current.calculating).toBe(false)
  })
})
