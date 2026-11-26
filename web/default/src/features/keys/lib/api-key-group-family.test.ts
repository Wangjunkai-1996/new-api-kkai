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
import { describe, expect, it } from 'vitest'

import {
  normalizeApiKeyGroupSearch,
  resolveApiKeyGroupFamily,
} from './api-key-group-family'

describe('resolveApiKeyGroupFamily', () => {
  it.each([
    ['gpt-fast', 'GPT – 企业 Fast', 'OpenAI'],
    ['enterprise', 'ＧＰＴ － 企业专用', 'OpenAI'],
    ['enterprise-legacy', 'Claude–Cursor', 'Claude'],
    ['claude-max', 'Claude–Max 0注入', 'Claude'],
    ['deepseek-official', 'DeepSeek–官 KEY', 'DeepSeek'],
    ['zhipu-official', 'GLM–官 KEY', 'GLM'],
    ['glm5.3-official', '企业专线', 'GLM'],
    ['o3-pro', '优先通道', 'OpenAI'],
    ['group-a', '通義千問专线', 'Qwen'],
    ['gemini-nano-banana', 'Nano Banana Pro', 'Gemini'],
  ])('uses the key or display name of %s (%s)', (value, label, name) => {
    expect(resolveApiKeyGroupFamily({ value, label })).toMatchObject({
      kind: 'provider',
      name,
      icon: expect.any(String),
    })
  })

  it('prioritizes auto routing metadata and the legacy auto key', () => {
    expect(
      resolveApiKeyGroupFamily({
        value: 'auto-2',
        label: 'Claude + GPT',
        isAuto: true,
      }).kind
    ).toBe('auto')
    expect(resolveApiKeyGroupFamily({ value: 'auto', label: 'GPT' }).kind).toBe(
      'auto'
    )
    expect(
      resolveApiKeyGroupFamily({ value: 'auto-2', label: 'Standard' }).kind
    ).toBe('other')
  })

  it('groups explicitly combined families and keeps same-family aliases together', () => {
    expect(
      resolveApiKeyGroupFamily({ value: 'combo', label: 'GPT＋Claude' })
    ).toMatchObject({
      kind: 'mixed',
      aliases: expect.arrayContaining(['OpenAI', 'Claude', 'Anthropic']),
    })
    expect(
      resolveApiKeyGroupFamily({ value: 'route', label: 'OpenAI / GPT' })
    ).toMatchObject({ kind: 'provider', name: 'OpenAI' })
  })

  it.each([
    ['enterprise-fast', '企业专线'],
    ['legacy', 'preclaude gateway'],
    ['enterprise', 'OpenAI compatible Claude'],
    ['claude-premium', 'OpenAI compatible Claude'],
    ['glm-vip', 'GPT + Claude'],
    ['gpt-official', 'Claude–企业专线'],
  ])('keeps ambiguous names in Other (%s, %s)', (value, label) => {
    expect(resolveApiKeyGroupFamily({ value, label }).kind).toBe('other')
  })
})

it('normalizes full-width characters, spaces and dashes for group search', () => {
  expect(normalizeApiKeyGroupSearch(' ＧＰＴ — 企业   ＦＡＳＴ ')).toBe(
    'gpt-企业 fast'
  )
})
