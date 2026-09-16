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
*/

export type AutoGroupChains = Readonly<Record<string, readonly string[]>>

function cleanChain(value: unknown): string[] {
  if (!Array.isArray(value)) return []

  return value.filter(
    (group): group is string => typeof group === 'string' && group.length > 0
  )
}

/**
 * Normalize both the legacy `auto_groups` array and the named-chain map.
 * The legacy array is intentionally treated as the `auto` chain.
 */
export function normalizeAutoGroupChains(
  namedChains: unknown,
  legacyGroups: unknown = []
): AutoGroupChains {
  if (namedChains && typeof namedChains === 'object' && !Array.isArray(namedChains)) {
    const chains: Record<string, string[]> = {}
    for (const [name, value] of Object.entries(namedChains)) {
      const chain = cleanChain(value)
      if (name && chain.length > 0) chains[name] = chain
    }
    if (Object.keys(chains).length > 0) return chains
  }

  const legacyChain = cleanChain(legacyGroups)
  return legacyChain.length > 0 ? { auto: legacyChain } : {}
}

export function getAutoGroupNames(chains: AutoGroupChains): string[] {
  return Object.keys(chains)
}

export function getAutoGroupChain(
  chains: AutoGroupChains,
  name: string
): readonly string[] {
  return chains[name] ?? []
}

export function isAutoGroupName(
  name: string | null | undefined,
  chains?: AutoGroupChains | ReadonlySet<string>
): boolean {
  if (!name) return false
  if (chains instanceof Set) return chains.has(name) || name === 'auto'
  if (chains && Object.hasOwn(chains, name)) return true
  return name === 'auto'
}
