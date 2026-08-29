/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/

/**
 * Metadata returned by the user-groups endpoint.
 *
 * `desc` is the field emitted by current servers. `display_name` is accepted
 * for the display-name API shape, while `displayName` keeps the client
 * compatible with deployments that expose camelCase JSON.
 */
export type UserGroupInfo = {
  desc?: string | null
  display_name?: string | null
  displayName?: string | null
  ratio?: number | string | null
}

export type UserGroupOption = {
  label: string
  value: string
  ratio: number | string
  desc?: string
}

export type GroupDisplayNameMap = Readonly<
  Record<string, string | null | undefined>
>

function nonEmptyText(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const trimmed = value.trim()
  return trimmed || undefined
}

/**
 * Resolve a label from a server-provided display-name map without changing
 * the canonical group key used by requests, filters, and persisted state.
 */
export function resolveGroupDisplayName(
  group: string,
  displayNames?: GroupDisplayNameMap,
  fallback?: string | null
): string {
  return nonEmptyText(displayNames?.[group]) ?? nonEmptyText(fallback) ?? group
}

/**
 * Resolve a user-facing label without changing the canonical group key.
 */
export function getUserGroupDisplayName(
  group: string,
  info?: UserGroupInfo
): string {
  return (
    nonEmptyText(info?.display_name) ??
    nonEmptyText(info?.displayName) ??
    resolveGroupDisplayName(group, undefined, info?.desc)
  )
}

/**
 * Keep a description only when it adds information beyond the label.
 */
export function getUserGroupDescription(
  info?: UserGroupInfo,
  label?: string
): string | undefined {
  const description = nonEmptyText(info?.desc)
  if (!description || (label && description === label)) return undefined
  return description
}

/**
 * Convert endpoint metadata into a selector option while preserving the key
 * as the submitted value.
 */
export function toUserGroupOption(
  group: string,
  info?: UserGroupInfo
): UserGroupOption {
  const label = getUserGroupDisplayName(group, info)
  const description = getUserGroupDescription(info, label)

  return {
    label,
    value: group,
    ratio: info?.ratio ?? '',
    ...(description ? { desc: description } : {}),
  }
}
