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
import { resolveModelProvider } from '@/lib/model-provider'

export type CCSwitchApp = 'claude' | 'codex' | 'gemini'

export const CC_SWITCH_RECOMMENDED_MODELS: Record<CCSwitchApp, string> = {
  claude: 'claude-opus-5-5',
  codex: 'gpt-6-astra',
  gemini: '',
}

const NON_TEXT_MODEL =
  /(?:^|[/_.:-])(?:images?|video|audio|embeddings?|moderation|tts|transcribe|transcription|realtime|sora|dall-e|whisper|imagen|veo|nano-banana)(?:$|[/_.:-])/
const CODEX_TEXT_MODEL =
  /(?:^|[/.:])(?:gpt-\d|chatgpt-\d|codex(?:-|$)|o[134](?:$|[-.:]))/
const CLAUDE_TEXT_MODEL = /(?:^|[/.:])claude-/

// ponytail: the token API exposes names only. Unknown aliases stay manual;
// use endpoint metadata instead if that API later exposes model capabilities.
export function inferCCSwitchApp(
  models: readonly string[]
): CCSwitchApp | null {
  let app: CCSwitchApp | null = null

  for (const model of models) {
    const name = model.trim().toLowerCase()
    if (NON_TEXT_MODEL.test(name)) continue

    const provider = resolveModelProvider(name)?.name
    let modelApp: CCSwitchApp
    if (provider === 'OpenAI' && CODEX_TEXT_MODEL.test(name)) {
      modelApp = 'codex'
    } else if (provider === 'Anthropic' && CLAUDE_TEXT_MODEL.test(name)) {
      modelApp = 'claude'
    } else {
      return null
    }

    if (app && app !== modelApp) return null
    app = modelApp
  }

  return app
}
