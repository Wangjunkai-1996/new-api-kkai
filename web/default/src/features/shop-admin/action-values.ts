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

export function evidenceIds(value: string) {
  const ids = [
    ...new Set(
      value
        .split(/[\s,]+/)
        .map((part) => part.trim())
        .filter(Boolean)
    ),
  ].sort()
  if (ids.length > 20) throw new Error('invalid evidence')
  return ids
}
export function utcTime(value: string) {
  const time = new Date(value)
  if (!Number.isFinite(time.getTime()) || time.getTime() <= 0) {
    throw new Error('invalid time')
  }
  return time.toISOString()
}
