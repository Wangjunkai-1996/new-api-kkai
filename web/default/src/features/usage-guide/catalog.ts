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

export type UsagePlatform = 'windows' | 'macos' | 'linux'

export type UsageTool = {
  id: string
  nameKey: string
  kindKey: string
  descriptionKey: string
  platforms: UsagePlatform[]
  downloads: Array<{
    platform: UsagePlatform
    architecture?: 'x64' | 'arm64' | 'universal'
    labelKey: string
    url: string
    action: 'download' | 'official-page' | 'install-guide'
  }>
  officialUrl: string
  checkedAt: string
  notesKey?: string
}

const CC_SWITCH_URL = 'https://github.com/farion1231/cc-switch/releases/latest'

export const USAGE_TOOLS: UsageTool[] = [
  {
    id: 'cc-switch',
    nameKey: 'CC Switch',
    kindKey: 'Configuration manager',
    descriptionKey:
      'Manage API endpoints, keys, and models in one place, then switch between client configurations.',
    platforms: ['windows', 'macos', 'linux'],
    downloads: [
      {
        platform: 'windows',
        labelKey: 'Windows official page',
        url: CC_SWITCH_URL,
        action: 'official-page',
      },
      {
        platform: 'macos',
        labelKey: 'macOS official page',
        url: CC_SWITCH_URL,
        action: 'official-page',
      },
      {
        platform: 'linux',
        labelKey: 'Linux official page',
        url: CC_SWITCH_URL,
        action: 'official-page',
      },
    ],
    officialUrl: CC_SWITCH_URL,
    checkedAt: '2026-11-03',
    notesKey: 'Install and open CC Switch before using a quick setup.',
  },
  {
    id: 'cherry-studio',
    nameKey: 'Cherry Studio',
    kindKey: 'Chat and writing',
    descriptionKey:
      'A desktop workspace for chat, writing, and knowledge-base workflows. It can use an API key directly.',
    platforms: ['windows', 'macos', 'linux'],
    downloads: [
      {
        platform: 'windows',
        labelKey: 'Windows download page',
        url: 'https://www.cherry-ai.com/download',
        action: 'official-page',
      },
      {
        platform: 'macos',
        labelKey: 'macOS download page',
        url: 'https://www.cherry-ai.com/download',
        action: 'official-page',
      },
      {
        platform: 'linux',
        labelKey: 'Linux download page',
        url: 'https://www.cherry-ai.com/download',
        action: 'official-page',
      },
    ],
    officialUrl: 'https://www.cherry-ai.com/download',
    checkedAt: '2026-11-03',
    notesKey:
      'No CC Switch import is required for the manual Cherry Studio flow.',
  },
  {
    id: 'codex-cli',
    nameKey: 'Codex CLI',
    kindKey: 'AI coding in the terminal',
    descriptionKey:
      'Use Codex from a terminal to read, edit, and reason about a codebase. The GPT quick setup targets this client.',
    platforms: ['windows', 'macos', 'linux'],
    downloads: [
      {
        platform: 'windows',
        labelKey: 'Windows install guide',
        url: 'https://learn.chatgpt.com/docs/codex/cli',
        action: 'install-guide',
      },
      {
        platform: 'macos',
        labelKey: 'macOS install guide',
        url: 'https://learn.chatgpt.com/docs/codex/cli',
        action: 'install-guide',
      },
      {
        platform: 'linux',
        labelKey: 'Linux install guide',
        url: 'https://learn.chatgpt.com/docs/codex/cli',
        action: 'install-guide',
      },
    ],
    officialUrl: 'https://learn.chatgpt.com/docs/codex/cli',
    checkedAt: '2026-11-03',
  },
  {
    id: 'codex-app',
    nameKey: 'Codex desktop entry',
    kindKey: 'Desktop coding workspace',
    descriptionKey:
      'A separate desktop coding workspace. Keep its install and connection instructions separate from Codex CLI.',
    platforms: ['windows', 'macos', 'linux'],
    downloads: [
      {
        platform: 'windows',
        labelKey: 'Windows official page',
        url: 'https://developers.openai.com/codex/app/',
        action: 'official-page',
      },
      {
        platform: 'macos',
        labelKey: 'macOS official page',
        url: 'https://developers.openai.com/codex/app/',
        action: 'official-page',
      },
      {
        platform: 'linux',
        labelKey: 'Linux official page',
        url: 'https://developers.openai.com/codex/app/',
        action: 'official-page',
      },
    ],
    officialUrl: 'https://developers.openai.com/codex/app/',
    checkedAt: '2026-11-03',
    notesKey:
      'Use the official page to confirm current desktop availability and supported systems.',
  },
  {
    id: 'claude-code',
    nameKey: 'Claude Code',
    kindKey: 'AI coding in the terminal',
    descriptionKey:
      'A project-aware coding client for working with repositories. The Claude quick setup targets this client.',
    platforms: ['windows', 'macos', 'linux'],
    downloads: [
      {
        platform: 'windows',
        labelKey: 'Windows install guide',
        url: 'https://code.claude.com/docs/en/setup',
        action: 'install-guide',
      },
      {
        platform: 'macos',
        labelKey: 'macOS install guide',
        url: 'https://code.claude.com/docs/en/setup',
        action: 'install-guide',
      },
      {
        platform: 'linux',
        labelKey: 'Linux install guide',
        url: 'https://code.claude.com/docs/en/setup',
        action: 'install-guide',
      },
    ],
    officialUrl: 'https://code.claude.com/docs/en/setup',
    checkedAt: '2026-11-03',
  },
  {
    id: 'claude-desktop',
    nameKey: 'Claude Desktop',
    kindKey: 'Desktop assistant',
    descriptionKey:
      'A separate desktop assistant. It does not support directly configuring this site API key.',
    platforms: ['windows', 'macos'],
    downloads: [
      {
        platform: 'windows',
        labelKey: 'Windows download page',
        url: 'https://claude.com/download',
        action: 'official-page',
      },
      {
        platform: 'macos',
        labelKey: 'macOS download page',
        url: 'https://claude.com/download',
        action: 'official-page',
      },
    ],
    officialUrl: 'https://claude.com/download',
    checkedAt: '2026-11-03',
    notesKey:
      'Download only. Use Claude Code or another supported client for this site API key.',
  },
]
