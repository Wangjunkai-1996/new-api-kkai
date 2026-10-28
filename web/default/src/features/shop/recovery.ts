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
import { z } from 'zod'

import { shopIdentity } from './api'

const markerSchema = z.object({
  key: z.string().uuid(),
  identity: z.string(),
  scope: z.enum(['create-order', 'import-commit']),
  target: z.string(),
  created_at: z.number(),
})
export type RecoveryMarker = z.infer<typeof markerSchema>
export function recoveryName(scope: RecoveryMarker['scope'], target: string) {
  return `kk-shop:${scope}:${shopIdentity()}:${target}`
}
export function readRecovery(
  scope: RecoveryMarker['scope'],
  target: string
): RecoveryMarker | null {
  try {
    const result = markerSchema.safeParse(
      JSON.parse(sessionStorage.getItem(recoveryName(scope, target)) ?? 'null')
    )
    return result.success &&
      result.data.identity === shopIdentity() &&
      result.data.scope === scope &&
      result.data.target === target
      ? result.data
      : null
  } catch {
    return null
  }
}
export function beginRecovery(
  scope: RecoveryMarker['scope'],
  target: string
): RecoveryMarker {
  const existing = readRecovery(scope, target)
  if (existing) return existing
  const marker: RecoveryMarker = {
    key: crypto.randomUUID(),
    identity: shopIdentity(),
    scope,
    target,
    created_at: Date.now(),
  }
  // Only an operation reference is stored. TTL never proves a timed-out request failed.
  try {
    sessionStorage.setItem(recoveryName(scope, target), JSON.stringify(marker))
  } catch {
    /* Current component still retains its key. */
  }
  return marker
}
export function finishRecovery(scope: RecoveryMarker['scope'], target: string) {
  try {
    sessionStorage.removeItem(recoveryName(scope, target))
  } catch {
    /* Storage may be disabled. */
  }
}
