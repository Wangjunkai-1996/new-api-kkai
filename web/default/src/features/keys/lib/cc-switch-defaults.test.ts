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
import { describe, expect, test } from 'vitest'

import { inferCCSwitchApp } from './cc-switch-defaults'

describe('CC Switch application defaults', () => {
  test.each([
    {
      name: 'OpenAI pool and expanded automatic groups',
      models: ['gpt-6-astra', 'gpt-6-sol', 'gpt-5.6-luna', 'codex-auto-review'],
      app: 'codex',
    },
    {
      name: 'Claude pools',
      models: ['claude-opus-5-5', 'claude-fable-5-1', 'claude-haiku-4-5'],
      app: 'claude',
    },
    {
      name: 'OpenAI pool without the recommended model',
      models: ['gpt-5.6-sol', 'gpt-6.1-sol', 'gpt-5.6-terra'],
      app: 'codex',
    },
    {
      name: 'Claude pool without the recommended model',
      models: ['claude-opus-4-6', 'claude-opus-4-7', 'claude-fable-5'],
      app: 'claude',
    },
    {
      name: 'provider-qualified and case-insensitive model names',
      models: [' OpenAI/GPT-6-ASTRA ', 'o3', 'openai.o4-mini'],
      app: 'codex',
    },
    {
      name: 'qualified Claude names',
      models: ['anthropic.claude-opus-5-5', 'anthropic/claude-sonnet-4-6'],
      app: 'claude',
    },
    {
      name: 'text models alongside explicit non-text models',
      models: [
        'gpt-6-astra',
        'gpt-image-2',
        'gpt-4o-audio-preview',
        'gpt-4o-mini-transcribe',
        'gpt-realtime',
        'text-embedding-3-large',
        'omni-moderation-latest',
        'tts-1',
        'sora-2',
        'dall-e-3',
        'whisper-1',
        'gemini-3.1-flash-image-preview',
        'veo-3',
      ],
      app: 'codex',
    },
  ])('infers $name', ({ models, app }) => {
    expect(inferCCSwitchApp(models)).toBe(app)
  })

  test.each([
    { name: 'empty model list', models: [] },
    {
      name: 'OpenAI images only',
      models: ['gpt-image-2', 'gpt-image-2.5-flare'],
    },
    { name: 'video only', models: ['sora-2', 'veo-3'] },
    {
      name: 'audio only',
      models: ['gpt-4o-audio-preview', 'whisper-1', 'tts-1'],
    },
    { name: 'embeddings only', models: ['text-embedding-3-small'] },
    {
      name: 'mixed OpenAI and Claude groups',
      models: ['gpt-6-astra', 'gpt-6-sol', 'claude-opus-5-5'],
    },
    {
      name: 'another text family alongside OpenAI',
      models: ['gpt-6-astra', 'deepseek-v4.1-flash'],
    },
    {
      name: 'unknown alias alongside Claude',
      models: ['claude-opus-5-5', 'custom-model'],
    },
    { name: 'unknown OpenAI alias', models: ['openai/custom-model'] },
    { name: 'provider keyword inside unrelated names', models: ['360gpt-pro'] },
    { name: 'Gemini stays manual', models: ['gemini-4-argon'] },
  ])('leaves $name manual', ({ models }) => {
    expect(inferCCSwitchApp(models)).toBeNull()
  })
})
