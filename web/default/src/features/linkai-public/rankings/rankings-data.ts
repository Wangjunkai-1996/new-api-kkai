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
export type ArenaCategory = {
  id: string
  label: string
  updatedAt: string
  total: number
  metricLabel?: string
  rows: Array<{ model: string; score: string }>
}

export const ARENA_CATEGORIES: ArenaCategory[] = [
  {
    id: 'text',
    label: 'Text Models',
    updatedAt: 'Dec 31, 11:38',
    total: 291,
    rows: [
      { model: 'gemini-3-pro', score: '1490' },
      { model: 'gemini-3-flash', score: '1478 Preliminary' },
      { model: 'grok-4.1-thinking', score: '1477' },
      { model: 'claude-opus-4-5-thinking-32k', score: '1476' },
      { model: 'claude-opus-4-5', score: '1472' },
    ],
  },
  {
    id: 'web',
    label: 'Web Development',
    updatedAt: 'Dec 31, 11:38',
    total: 22,
    rows: [
      { model: 'claude-opus-4-5-thinking-32k', score: '1493' },
      { model: 'claude-sonnet-4-5', score: '1479' },
      { model: 'gemini-3-pro', score: '1473' },
      { model: 'gpt-5.2-codex', score: '1458' },
      { model: 'grok-code-fast-1', score: '1441' },
    ],
  },
  {
    id: 'vision',
    label: 'Vision Models',
    updatedAt: 'Dec 31, 11:38',
    total: 98,
    rows: [
      { model: 'gemini-3-pro', score: '1448' },
      { model: 'gpt-5.2', score: '1439' },
      { model: 'claude-opus-4-5', score: '1421' },
      { model: 'qwen3-vl-plus', score: '1408' },
      { model: 'grok-4.1-vision', score: '1394' },
    ],
  },
  {
    id: 'text-to-image',
    label: 'Text to Image',
    updatedAt: 'Dec 31, 11:38',
    total: 46,
    rows: [
      { model: 'imagen-4-ultra', score: '1358' },
      { model: 'seedream-4.5', score: '1346' },
      { model: 'flux-2-pro', score: '1339' },
      { model: 'gpt-image-1.5', score: '1328' },
      { model: 'midjourney-v7', score: '1314' },
    ],
  },
  {
    id: 'image-editing',
    label: 'Image Editing',
    updatedAt: 'Dec 31, 11:38',
    total: 38,
    rows: [
      { model: 'gemini-3-pro-image', score: '1402' },
      { model: 'gpt-image-1.5', score: '1396' },
      { model: 'seededit-3.0', score: '1381' },
      { model: 'flux-kontext-pro', score: '1368' },
      { model: 'qwen-image-edit', score: '1355' },
    ],
  },
  {
    id: 'search',
    label: 'Search Models',
    updatedAt: 'Dec 31, 11:38',
    total: 31,
    rows: [
      { model: 'perplexity-sonar-pro', score: '1462' },
      { model: 'gemini-3-pro-search', score: '1451' },
      { model: 'grok-4.1-search', score: '1437' },
      { model: 'gpt-5.2-search', score: '1429' },
      { model: 'qwen3-search', score: '1410' },
    ],
  },
  {
    id: 'text-to-video',
    label: 'Text to Video',
    updatedAt: 'Dec 31, 11:38',
    total: 29,
    rows: [
      { model: 'veo-3.1-fast-audio', score: '1490' },
      { model: 'veo-3.1-audio', score: '1478 Preliminary' },
      { model: 'veo-3-fast-audio', score: '1477' },
      { model: 'sora-2-pro', score: '1461' },
      { model: 'veo-3-audio', score: '1454' },
    ],
  },
  {
    id: 'image-to-video',
    label: 'Image to Video',
    updatedAt: 'Dec 31, 11:38',
    total: 24,
    rows: [
      { model: 'veo-3.1-audio', score: '1493' },
      { model: 'veo-3.1-fast-audio', score: '1479' },
      { model: 'wan2.5-i2v-preview', score: '1473' },
      { model: 'veo-3-audio', score: '1399' },
      { model: 'veo-3-fast-audio', score: '1397' },
    ],
  },
  {
    id: 'code',
    label: 'Coding Assistant',
    updatedAt: 'Nov 9, 21:28',
    total: 291,
    rows: [
      { model: 'claude-sonnet-4-5', score: '1490' },
      { model: 'gpt-5.2-codex', score: '1478 Preliminary' },
      { model: 'gemini-3-pro', score: '1477' },
      { model: 'deepseek-v3.2', score: '1464' },
      { model: 'qwen3-coder-plus', score: '1456' },
    ],
  },
]
