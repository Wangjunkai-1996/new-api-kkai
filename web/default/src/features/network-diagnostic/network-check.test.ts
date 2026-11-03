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
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  runNetworkCheck,
  summarizeCheck,
  type CheckResult,
} from './network-check'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('network diagnosis', () => {
  it.each([
    [
      {
        samples: [91, 101, 102, 105, 101].map((ms) => ({
          kind: 'success' as const,
          ms,
        })),
        end: 'complete' as const,
      },
      100,
      'smooth',
    ],
    [
      {
        samples: [293, 293, 293, 293, 293].map((ms) => ({
          kind: 'success' as const,
          ms,
        })),
        end: 'complete' as const,
      },
      293,
      'smooth',
    ],
    [
      {
        samples: [500, 500, 500, 500, 500].map((ms) => ({
          kind: 'success' as const,
          ms,
        })),
        end: 'complete' as const,
      },
      500,
      'smooth',
    ],
    [
      {
        samples: [501, 501, 501, 501, 501].map((ms) => ({
          kind: 'success' as const,
          ms,
        })),
        end: 'complete' as const,
      },
      501,
      'fair',
    ],
    [
      {
        samples: [1_000, 1_000, 1_000, 1_000, 1_000].map((ms) => ({
          kind: 'success' as const,
          ms,
        })),
        end: 'complete' as const,
      },
      1_000,
      'fair',
    ],
    [
      {
        samples: [1_001, 1_001, 1_001, 1_001, 1_001].map((ms) => ({
          kind: 'success' as const,
          ms,
        })),
        end: 'complete' as const,
      },
      1_001,
      'slow',
    ],
    [
      {
        samples: [80, 90, 100, 110, 900].map((ms) => ({
          kind: 'success' as const,
          ms,
        })),
        end: 'complete' as const,
      },
      256,
      'fluctuating',
    ],
    [
      {
        samples: [
          { kind: 'success' as const, ms: 120 },
          { kind: 'timeout' as const },
          { kind: 'success' as const, ms: 140 },
          { kind: 'success' as const, ms: 160 },
          { kind: 'failed' as const },
        ],
        end: 'complete' as const,
      },
      140,
      'unstable',
    ],
    [
      { samples: [{ kind: 'failed' as const }], end: 'complete' as const },
      null,
      'unavailable',
    ],
    [
      {
        samples: [{ kind: 'timeout' as const }, { kind: 'timeout' as const }],
        end: 'complete' as const,
      },
      null,
      'incomplete',
    ],
    [
      {
        samples: [
          { kind: 'success' as const, ms: 90 },
          { kind: 'failed' as const },
        ],
        end: 'limited' as const,
      },
      90,
      'limited',
    ],
  ])('summarizes successful samples and %s', (result, averageMs, verdict) => {
    const summary = summarizeCheck(result as CheckResult)
    expect(summary.averageMs).toBe(averageMs)
    expect(summary.verdict).toBe(verdict)
  })

  it('uses only readable 204 responses and stops on rate limiting', async () => {
    vi.useFakeTimers()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
      .mockResolvedValueOnce(new Response(null, { status: 429 }))
    vi.stubGlobal('fetch', fetchMock)
    const onSample = vi.fn()

    const check = runNetworkCheck(new AbortController().signal, onSample)
    await vi.advanceTimersByTimeAsync(150)
    const result = await check

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(fetchMock.mock.calls[0][0]).toMatch(
      /^https:\/\/api\.kkrich\.ltd\/api\/network-probe\?n=/
    )
    expect(fetchMock.mock.calls[0][1]).toMatchObject({
      credentials: 'omit',
      cache: 'no-store',
      mode: 'cors',
    })
    expect(result.end).toBe('limited')
    expect(result.samples.map((sample) => sample.kind)).toEqual([
      'success',
      'failed',
    ])
    expect(onSample).toHaveBeenCalledTimes(2)
  })

  it('rejects a non-204 response instead of recording it as latency', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response('challenge', { status: 200 }))
    )

    const result = await runNetworkCheck(new AbortController().signal, vi.fn())

    expect(result).toEqual({
      samples: [{ kind: 'failed' }],
      end: 'invalid-response',
    })
  })

  it('stops when the browser cannot read the response', async () => {
    const fetchMock = vi
      .fn()
      .mockRejectedValue(new TypeError('Failed to fetch'))
    vi.stubGlobal('fetch', fetchMock)

    const result = await runNetworkCheck(new AbortController().signal, vi.fn())

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(result).toEqual({
      samples: [{ kind: 'failed' }],
      end: 'unavailable',
    })
  })

  it('continues after a timeout and excludes it from the average', async () => {
    vi.useFakeTimers()
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(
        (_url: string, options: RequestInit) =>
          new Promise((_resolve, reject) => {
            options.signal?.addEventListener('abort', () =>
              reject(new DOMException('Aborted', 'AbortError'))
            )
          })
      )
      .mockResolvedValue(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)

    const check = runNetworkCheck(new AbortController().signal, vi.fn())
    await vi.advanceTimersByTimeAsync(4_600)
    const result = await check

    expect(fetchMock).toHaveBeenCalledTimes(5)
    expect(result.end).toBe('complete')
    expect(result.samples.map((sample) => sample.kind)).toEqual([
      'timeout',
      'success',
      'success',
      'success',
      'success',
    ])
    expect(summarizeCheck(result)).toMatchObject({
      successes: 4,
      failures: 1,
      verdict: 'unstable',
    })
  })

  it('does not publish a result after cancellation', async () => {
    const controller = new AbortController()
    vi.stubGlobal(
      'fetch',
      vi.fn(
        (_url: string, options: RequestInit) =>
          new Promise((_resolve, reject) => {
            options.signal?.addEventListener('abort', () =>
              reject(new DOMException('Aborted', 'AbortError'))
            )
          })
      )
    )

    const check = runNetworkCheck(controller.signal, vi.fn())
    controller.abort()

    await expect(check).rejects.toMatchObject({ name: 'AbortError' })
  })
})
