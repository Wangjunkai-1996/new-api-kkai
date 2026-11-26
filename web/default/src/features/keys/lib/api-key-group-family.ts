/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { resolveModelProvider } from '@/lib/model-provider'

export type ApiKeyGroupFamily = {
  kind: 'provider' | 'auto' | 'mixed' | 'other'
  name: string
  icon?: string
  aliases: readonly string[]
}

const AUTO_FAMILY: ApiKeyGroupFamily = {
  kind: 'auto',
  name: 'Automatic routing',
  aliases: ['auto'],
}
const OTHER_FAMILY: ApiKeyGroupFamily = {
  kind: 'other',
  name: 'Other',
  aliases: [],
}

// The first name is the section label. A representative model supplies the
// same icon already used for this family elsewhere in the console.
const PROVIDER_FAMILIES = [
  ['gpt-4o', 'OpenAI', 'GPT', 'ChatGPT', 'Codex', 'Sora', 'o1', 'o3', 'o4'],
  ['claude-sonnet-4', 'Claude', 'Anthropic', 'Claude Code'],
  ['gemini-2.5-pro', 'Gemini', 'Gemma', 'Imagen', 'Veo', 'Nano Banana'],
  ['deepseek-v3', 'DeepSeek', '深度求索'],
  ['glm-5', 'GLM', 'Zhipu', 'ChatGLM', '智谱', '智譜'],
  ['qwen-3', 'Qwen', 'Tongyi', '通义', '通義', '千问', '千問'],
  ['kimi-k2', 'Kimi', 'Moonshot', '月之暗面'],
  ['grok-4', 'Grok', 'xAI'],
  ['minimax-m2', 'MiniMax', '海螺', 'Hailuo'],
  ['doubao-1', 'Doubao', 'Seedance', 'Seedream', '豆包'],
  ['wan2.2', 'Wan', '万相', '萬相'],
  ['mistral-large', 'Mistral', 'Mixtral', 'Codestral'],
  ['llama-4', 'Llama', 'Meta'],
  ['kling-v2', 'Kling', '可灵', '可靈'],
  ['vidu-2', 'Vidu'],
  ['midjourney-v7', 'Midjourney'],
].map(([model, name, ...aliases]): ApiKeyGroupFamily => ({
  kind: 'provider',
  name,
  aliases,
  icon: resolveModelProvider(model)?.icon,
}))

const PROVIDERS_BY_ALIAS = new Map(
  PROVIDER_FAMILIES.flatMap((family) =>
    [family.name, ...family.aliases].map(
      (alias) => [alias.toLowerCase().replace(' ', ''), family] as const
    )
  )
)
const LOCALIZED_PROVIDER_ALIASES = [...PROVIDERS_BY_ALIAS.keys()].filter(
  (alias) => /[^a-z0-9]/u.test(alias)
)

export function normalizeApiKeyGroupSearch(text: string): string {
  return text
    .normalize('NFKC')
    .trim()
    .toLowerCase()
    .replaceAll(/\s*[‐‑‒–—−-]\s*/gu, '-')
    .replaceAll(/\s+/gu, ' ')
}

function getNameFamilies(name: string): ApiKeyGroupFamily[] | null {
  const families = new Set<ApiKeyGroupFamily>()

  for (const part of normalizeApiKeyGroupSearch(name).split(/[+/&|、]/u)) {
    const words =
      part
        .replaceAll(/\bnano[- ]banana\b/gu, 'nanobanana')
        .match(/[a-z0-9]+/gu) ?? []
    const matches = new Set<ApiKeyGroupFamily>()
    for (const word of words) {
      const family =
        PROVIDERS_BY_ALIAS.get(word) ??
        PROVIDERS_BY_ALIAS.get(word.replace(/\d+[a-z]?$/u, ''))
      if (family) matches.add(family)
    }
    for (const alias of LOCALIZED_PROVIDER_ALIASES) {
      const family = PROVIDERS_BY_ALIAS.get(alias)
      if (family && part.includes(alias)) matches.add(family)
    }

    // Adjacent names can describe compatibility or an underlying provider.
    if (matches.size > 1) return null
    for (const family of matches) families.add(family)
  }

  return [...families]
}

export function resolveApiKeyGroupFamily(option: {
  value: string
  label: string
  isAuto?: boolean
}): ApiKeyGroupFamily {
  if (option.isAuto || option.value === 'auto') return AUTO_FAMILY

  // ponytail: the group endpoint supplies names, so unrecognizable names stay
  // in Other. Prefer explicit model-family metadata when the endpoint exposes it.
  const keyFamilies = getNameFamilies(option.value)
  const labelFamilies =
    option.label === option.value ? keyFamilies : getNameFamilies(option.label)
  if (!keyFamilies || !labelFamilies) return OTHER_FAMILY

  const families =
    keyFamilies.length >= labelFamilies.length ? keyFamilies : labelFamilies
  if (
    [...keyFamilies, ...labelFamilies].some(
      (family) => !families.includes(family)
    )
  ) {
    return OTHER_FAMILY
  }

  if (families.length > 1) {
    return {
      kind: 'mixed',
      name: 'Multiple providers',
      aliases: families.flatMap((family) => [family.name, ...family.aliases]),
    }
  }

  return families[0] ?? OTHER_FAMILY
}
