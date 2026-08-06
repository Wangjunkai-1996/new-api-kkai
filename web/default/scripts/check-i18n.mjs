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
import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const SOURCE_DIR = path.resolve('src')
const STATIC_KEYS_FILE = path.resolve('src/i18n/static-keys.ts')
const LOCALES_DIR = path.resolve('src/i18n/locales')
const REQUIRED_LOCALES = ['en', 'zh']
const SOURCE_EXTENSIONS = new Set(['.js', '.jsx', '.ts', '.tsx'])

const isIdentifierStart = (char) => /[A-Za-z_$]/.test(char ?? '')
const isIdentifierPart = (char) => /[A-Za-z0-9_$]/.test(char ?? '')

const readQuotedString = (source, start) => {
  const quote = source[start]
  let value = ''
  let index = start + 1

  while (index < source.length) {
    const char = source[index]
    if (char === quote) return { end: index + 1, value }
    if (char !== '\\') {
      value += char
      index += 1
      continue
    }

    const escaped = source[index + 1]
    const escapeValues = { n: '\n', r: '\r', t: '\t' }
    value += escapeValues[escaped] ?? escaped ?? ''
    index += escaped === undefined ? 1 : 2
  }

  return { end: source.length, value }
}

const skipComment = (source, start) => {
  if (source[start + 1] === '/') {
    const end = source.indexOf('\n', start + 2)
    return end === -1 ? source.length : end + 1
  }
  if (source[start + 1] === '*') {
    const end = source.indexOf('*/', start + 2)
    return end === -1 ? source.length : end + 2
  }
  return start
}

const skipWhitespace = (source, start) => {
  let index = start
  while (/\s/.test(source[index] ?? '')) index += 1
  return index
}

export const extractStringLiterals = (source) => {
  const values = []
  let index = 0

  while (index < source.length) {
    if (source[index] === '/') {
      const next = skipComment(source, index)
      if (next !== index) {
        index = next
        continue
      }
    }
    if (source[index] === "'" || source[index] === '"') {
      const literal = readQuotedString(source, index)
      values.push(literal.value)
      index = literal.end
      continue
    }
    if (source[index] === '`') {
      index = readQuotedString(source, index).end
      continue
    }
    index += 1
  }

  return values
}

export const extractLiteralTranslationKeys = (source) => {
  const keys = new Set()
  let index = 0

  while (index < source.length) {
    if (source[index] === '/') {
      const next = skipComment(source, index)
      if (next !== index) {
        index = next
        continue
      }
    }
    if (
      source[index] === "'" ||
      source[index] === '"' ||
      source[index] === '`'
    ) {
      index = readQuotedString(source, index).end
      continue
    }
    if (!isIdentifierStart(source[index])) {
      index += 1
      continue
    }

    const identifierStart = index
    index += 1
    while (isIdentifierPart(source[index])) index += 1
    if (source.slice(identifierStart, index) !== 't') continue

    let callStart = skipWhitespace(source, index)
    if (source[callStart] !== '(') continue
    callStart = skipWhitespace(source, callStart + 1)
    if (source[callStart] !== "'" && source[callStart] !== '"') continue

    const literal = readQuotedString(source, callStart)
    keys.add(literal.value)
    index = literal.end
  }

  return keys
}

export const extractStaticKeys = (source) => {
  const marker = 'export const STATIC_I18N_KEYS = ['
  const start = source.indexOf(marker)
  const end = source.indexOf('] as const', start)
  if (start === -1 || end === -1) {
    throw new Error('Unable to locate STATIC_I18N_KEYS array.')
  }
  return new Set(
    extractStringLiterals(source.slice(start + marker.length, end))
  )
}

const findSourceFiles = async (directory) => {
  const entries = await fs.readdir(directory, { withFileTypes: true })
  const files = await Promise.all(
    entries.map(async (entry) => {
      const fullPath = path.join(directory, entry.name)
      if (entry.isDirectory()) return findSourceFiles(fullPath)
      return SOURCE_EXTENSIONS.has(path.extname(entry.name)) ? [fullPath] : []
    })
  )
  return files.flat()
}

const loadRequiredLocale = async (locale) => {
  const filename = path.join(LOCALES_DIR, `${locale}.json`)
  const json = JSON.parse(await fs.readFile(filename, 'utf8'))
  if (!json.translation || typeof json.translation !== 'object') {
    throw new Error(`${filename} does not contain a translation object.`)
  }
  return json.translation
}

const main = async () => {
  const keys = new Set()
  const sourceFiles = await findSourceFiles(SOURCE_DIR)
  for (const filename of sourceFiles) {
    const source = await fs.readFile(filename, 'utf8')
    for (const key of extractLiteralTranslationKeys(source)) keys.add(key)
  }

  const staticSource = await fs.readFile(STATIC_KEYS_FILE, 'utf8')
  for (const key of extractStaticKeys(staticSource)) keys.add(key)

  let failed = false
  for (const locale of REQUIRED_LOCALES) {
    const translation = await loadRequiredLocale(locale)
    const missing = [...keys].filter((key) => !(key in translation)).sort()
    if (missing.length === 0) continue

    failed = true
    console.error(`${locale}.json is missing ${missing.length} source keys:`)
    for (const key of missing) console.error(`  - ${key}`)
  }

  if (failed) {
    process.exitCode = 1
    return
  }
  console.log(
    `i18n source contract passed: ${keys.size} keys exist in ${REQUIRED_LOCALES.join(', ')}.`
  )
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    console.error(error)
    process.exitCode = 1
  })
}
