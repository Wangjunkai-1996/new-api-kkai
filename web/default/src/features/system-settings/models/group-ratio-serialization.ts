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

import { safeJsonParse } from '../utils/json-parser'

export type GroupPricingRow = {
  _id: string
  name: string
  displayName: string
  ratio: string
  topupRatio: string
  selectable: boolean
  description: string
  hasRatio: boolean
  hasTopupRatio: boolean
  hasUserUsable: boolean
  editedFields: Partial<
    Record<'ratio' | 'topupRatio' | 'selectable' | 'description', boolean>
  >
  isNew?: boolean
}

function parseRatioMap(value: string): Record<string, number> {
  return safeJsonParse<Record<string, number>>(value, {
    fallback: {},
    silent: true,
  })
}

function parseUsableMap(value: string): Record<string, string> {
  return safeJsonParse<Record<string, string>>(value, {
    fallback: {},
    silent: true,
  })
}

function parseDisplayNameMap(value: string): Record<string, string> {
  return safeJsonParse<Record<string, string>>(value, {
    fallback: {},
    silent: true,
  })
}

export function normalizeRatio(value: unknown): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : 1
}

export function buildGroupPricingRows(
  groupRatio: string,
  userUsableGroups: string,
  topupGroupRatio: string,
  groupDisplayNames: string,
  createId: () => string
): GroupPricingRow[] {
  const ratioMap = parseRatioMap(groupRatio)
  const usableMap = parseUsableMap(userUsableGroups)
  const topupMap = parseRatioMap(topupGroupRatio)
  const displayNamesMap = parseDisplayNameMap(groupDisplayNames)
  const names = new Set([
    ...Object.keys(ratioMap),
    ...Object.keys(usableMap),
    ...Object.keys(topupMap),
    ...Object.keys(displayNamesMap),
  ])

  return [...names].map((name) => ({
    _id: createId(),
    name,
    // Display names decorate an existing canonical group. An orphaned label
    // must not create a new group with the default ratio when the form saves.
    displayName: String(displayNamesMap[name] ?? ''),
    ratio: String(normalizeRatio(ratioMap[name])),
    topupRatio: Object.hasOwn(topupMap, name) ? String(topupMap[name]) : '',
    selectable: Object.hasOwn(usableMap, name),
    description: String(usableMap[name] ?? ''),
    hasRatio: Object.hasOwn(ratioMap, name),
    hasTopupRatio: Object.hasOwn(topupMap, name),
    hasUserUsable: Object.hasOwn(usableMap, name),
    editedFields: {},
  }))
}

export function serializeGroupPricingRows(rows: GroupPricingRow[]) {
  const groupRatio: Record<string, number> = {}
  const userUsableGroups: Record<string, string> = {}
  const topupGroupRatio: Record<string, number> = {}
  const groupDisplayNames: Record<string, string> = {}

  for (const row of rows) {
    const rawName = String(row.name || '')
    // Existing identifiers are canonical values. Only trim names introduced
    // by the add-row flow; changing a label must never rewrite the key.
    const name = row.isNew ? rawName.trim() : rawName
    if (!name) continue

    const editedFields = row.editedFields ?? {}
    // A row can be present only in one of the auxiliary maps. Keep it out of
    // the other maps unless the operator explicitly edits that field (or adds
    // a new row), so display-only/auto/special references stay inert.
    if (row.isNew || row.hasRatio || editedFields.ratio) {
      groupRatio[name] = normalizeRatio(row.ratio)
    }
    const displayName = row.displayName.trim()
    if (displayName) {
      groupDisplayNames[name] = displayName
    }
    if (
      row.isNew ||
      row.hasUserUsable ||
      editedFields.selectable ||
      editedFields.description
    ) {
      if (row.selectable) {
        userUsableGroups[name] = row.description
      }
    }
    if (row.isNew || row.hasTopupRatio || editedFields.topupRatio) {
      const topup = row.topupRatio.trim()
      if (topup !== '' && Number.isFinite(Number(topup))) {
        topupGroupRatio[name] = Number(topup)
      }
    }
  }

  return {
    GroupRatio: JSON.stringify(groupRatio, null, 2),
    UserUsableGroups: JSON.stringify(userUsableGroups, null, 2),
    TopupGroupRatio: JSON.stringify(topupGroupRatio, null, 2),
    GroupDisplayNames: JSON.stringify(groupDisplayNames, null, 2),
  }
}

export function groupPricingSignature(rows: GroupPricingRow[]): string {
  const serialized = serializeGroupPricingRows(rows)
  return JSON.stringify({
    groupRatio: parseRatioMap(serialized.GroupRatio),
    userUsableGroups: parseUsableMap(serialized.UserUsableGroups),
    topupGroupRatio: parseRatioMap(serialized.TopupGroupRatio),
    groupDisplayNames: parseDisplayNameMap(serialized.GroupDisplayNames),
  })
}

export function sourceGroupPricingSignature(
  groupRatio: string,
  userUsableGroups: string,
  topupGroupRatio: string,
  groupDisplayNames: string
): string {
  return JSON.stringify({
    groupRatio: parseRatioMap(groupRatio),
    userUsableGroups: parseUsableMap(userUsableGroups),
    topupGroupRatio: parseRatioMap(topupGroupRatio),
    groupDisplayNames: parseDisplayNameMap(groupDisplayNames),
  })
}
