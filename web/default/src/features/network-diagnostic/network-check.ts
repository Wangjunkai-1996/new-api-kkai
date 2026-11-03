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
export type ProbeSample =
  | { kind: 'success'; ms: number }
  | { kind: 'timeout' }
  | { kind: 'failed' }

export type CheckEnd =
  | 'complete'
  | 'limited'
  | 'invalid-response'
  | 'unavailable'

export type CheckResult = {
  samples: ProbeSample[]
  end: CheckEnd
}

export type ConnectionVerdict =
  | 'smooth'
  | 'fair'
  | 'slow'
  | 'fluctuating'
  | 'unstable'
  | 'incomplete'
  | 'limited'
  | 'unavailable'

const PROBE_URL = 'https://api.kkrich.ltd/api/network-probe'
const SAMPLE_COUNT = 5
const REQUEST_TIMEOUT_MS = 4_000
const SAMPLE_GAP_MS = 150

function cancelled(): DOMException {
  return new DOMException('Network check cancelled', 'AbortError')
}

async function probeOnce(index: number, signal: AbortSignal) {
  if (signal.aborted) throw cancelled()

  const controller = new AbortController()
  const abort = () => controller.abort()
  signal.addEventListener('abort', abort, { once: true })
  let timedOut = false
  const timeout = setTimeout(() => {
    timedOut = true
    controller.abort()
  }, REQUEST_TIMEOUT_MS)

  try {
    const started = performance.now()
    const response = await fetch(`${PROBE_URL}?n=${Date.now()}-${index}`, {
      method: 'GET',
      mode: 'cors',
      credentials: 'omit',
      cache: 'no-store',
      signal: controller.signal,
    })
    if (response.status === 204 && !response.redirected) {
      return {
        kind: 'success',
        ms: Math.round(performance.now() - started),
      } as const
    }
    if (response.status === 429) return { kind: 'limited' } as const
    return { kind: 'invalid-response' } as const
  } catch {
    if (signal.aborted) throw cancelled()
    if (timedOut) return { kind: 'timeout' } as const
    return { kind: 'unavailable' } as const
  } finally {
    clearTimeout(timeout)
    signal.removeEventListener('abort', abort)
  }
}

export async function runNetworkCheck(
  signal: AbortSignal,
  onSample: (samples: ProbeSample[]) => void
): Promise<CheckResult> {
  const samples: ProbeSample[] = []

  for (let index = 0; index < SAMPLE_COUNT; index++) {
    const probe = await probeOnce(index, signal)
    if (
      probe.kind === 'limited' ||
      probe.kind === 'invalid-response' ||
      probe.kind === 'unavailable'
    ) {
      samples.push({ kind: 'failed' })
      onSample([...samples])
      return { samples, end: probe.kind }
    }
    samples.push(probe)
    onSample([...samples])
    if (index < SAMPLE_COUNT - 1) {
      await new Promise((resolve) => setTimeout(resolve, SAMPLE_GAP_MS))
      if (signal.aborted) throw cancelled()
    }
  }

  return { samples, end: 'complete' }
}

export function summarizeCheck(result: CheckResult): {
  averageMs: number | null
  successes: number
  failures: number
  verdict: ConnectionVerdict
} {
  const timings = result.samples.flatMap((sample) =>
    sample.kind === 'success' ? [sample.ms] : []
  )
  const successes = timings.length
  const failures = result.samples.length - successes
  const averageMs = successes
    ? Math.round(timings.reduce((sum, ms) => sum + ms, 0) / successes)
    : null

  if (result.end === 'limited') {
    return { averageMs, successes, failures, verdict: 'limited' }
  }
  if (result.end === 'unavailable' || result.end === 'invalid-response') {
    return { averageMs, successes, failures, verdict: 'unavailable' }
  }
  if (
    successes === 0 &&
    result.samples.some((sample) => sample.kind === 'failed')
  ) {
    return { averageMs, successes, failures, verdict: 'unavailable' }
  }
  if (successes < 3 || averageMs === null) {
    return { averageMs, successes, failures, verdict: 'incomplete' }
  }
  if (successes < SAMPLE_COUNT) {
    return { averageMs, successes, failures, verdict: 'unstable' }
  }

  const spread = Math.max(...timings) - Math.min(...timings)
  if (spread > Math.max(250, averageMs * 0.8)) {
    return { averageMs, successes, failures, verdict: 'fluctuating' }
  }
  if (averageMs > 500) {
    return { averageMs, successes, failures, verdict: 'slow' }
  }
  if (averageMs > 200) {
    return { averageMs, successes, failures, verdict: 'fair' }
  }
  return { averageMs, successes, failures, verdict: 'smooth' }
}
