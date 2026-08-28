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
export type DocsArticle = {
  id: string
  title: string
  subtitle: string
  sections: Array<{
    id: string
    title: string
    paragraphs: string[]
    numbered?: string[]
  }>
}

export const DOCS_NAVIGATION = [
  {
    id: 'model-compatibility',
    label: 'Preface - Model compatibility (required)',
  },
  { id: 'openai-sdk', label: 'OpenAI official SDK tutorial' },
  { id: 'batch-requests', label: 'High-concurrency batch request examples' },
  { id: 'claude-code', label: 'Claude Code API configuration guide' },
  { id: 'codex', label: 'OpenAI Codex API configuration guide' },
  { id: 'gemini-cli', label: 'Gemini CLI API configuration guide' },
  { id: 'openclaw', label: 'OpenClaw API tutorial' },
  { id: 'opencode', label: 'OpenCode API tutorial' },
  { id: 'trae', label: 'Trae API tutorial' },
  { id: 'cc-switch', label: 'CC Switch API tutorial' },
] as const

const COMPATIBILITY_ARTICLE: DocsArticle = {
  id: 'model-compatibility',
  title: 'Preface - Model compatibility (required)',
  subtitle: 'Model compatibility',
  sections: [
    {
      id: 'compatibility',
      title: 'Model compatibility',
      paragraphs: [
        'Welcome to the official Omnitoken & LinkAI website. Before using this website and its services, please read and fully understand this disclaimer. By accessing, browsing, or using any part of this website, you confirm that you have read, understood, and accepted this statement.',
      ],
    },
    {
      id: 'information',
      title: '1. About information content',
      paragraphs: [],
      numbered: [
        'All information published on this website, including product introductions, technical parameters, pricing descriptions, and industry cases, is for reference only and does not constitute an offer, commitment, or commercial guarantee.',
        'We make every effort to keep information accurate, complete, and timely, but cannot guarantee that every item is error-free. Decisions made in reliance on this website are at your own risk.',
        'Model prices and token rates may be adjusted according to market conditions. The quotation at the time of the actual transaction shall prevail.',
      ],
    },
    {
      id: 'external-links',
      title: '2. About external links',
      paragraphs: [
        'This website may contain links to third-party websites. They are provided for convenience and do not represent an endorsement or guarantee of third-party content, products, or services.',
        'We are not responsible for losses or disputes resulting from access to third-party websites. Please evaluate and follow the terms and privacy policies of those websites.',
      ],
    },
  ],
}

function createGuide(id: string, title: string): DocsArticle {
  return {
    id,
    title,
    subtitle: 'Quick start',
    sections: [
      {
        id: 'overview',
        title: 'Overview',
        paragraphs: [
          'Use an API key from the console and point your client base URL to the LinkAI gateway. Existing OpenAI-compatible request formats can be retained.',
        ],
      },
      {
        id: 'authentication',
        title: 'Authentication',
        paragraphs: [
          'Send the API key in the Authorization header as a Bearer token. Keep keys on the server and never expose them in public client code.',
        ],
      },
      {
        id: 'api-address',
        title: 'API address',
        paragraphs: [
          'Select the endpoint shown in your console. Replace the model name as needed without changing the surrounding request structure.',
        ],
      },
    ],
  }
}

export const DOCS_ARTICLES: DocsArticle[] = [
  COMPATIBILITY_ARTICLE,
  ...DOCS_NAVIGATION.slice(1).map((item) => createGuide(item.id, item.label)),
]
