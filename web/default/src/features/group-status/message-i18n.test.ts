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

import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { test } from 'vitest'

import en from '@/i18n/locales/en.json'
import zh from '@/i18n/locales/zh.json'

const backendSource = readFileSync(
  resolve(process.cwd(), '../../service/kkai_group_status_types.go'),
  'utf8'
)

const backendMessageKeys = Array.from(
  backendSource.matchAll(
    /kkaiGroupStatusMessage\w+\s*=\s*"(Group status message: [^"]+)"/g
  ),
  (match) => match[1]
).filter((key): key is string => Boolean(key))

test('every backend group status message has a user-facing translation', () => {
  assert.ok(backendMessageKeys.length >= 10)
  assert.equal(new Set(backendMessageKeys).size, backendMessageKeys.length)

  const locales: Record<string, Record<string, string>> = {
    en: en.translation,
    zh: zh.translation,
  }

  for (const [locale, translations] of Object.entries(locales)) {
    for (const key of backendMessageKeys) {
      assert.ok(Object.hasOwn(translations, key), `${locale} is missing ${key}`)
      assert.notEqual(translations[key], key)
    }
  }
})
