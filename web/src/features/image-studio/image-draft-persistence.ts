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
import { imageComposerSchema } from './schemas'
import type { ImageComposerValues, ImageStudioComposerMode } from './types'

export type ImageStudioDraftPersistence =
  | { action: 'clear' }
  | { action: 'save'; draft: ImageComposerValues }
  | { action: 'skip' }

type ImageStudioDraftCandidate = {
  prompt?: string
  [key: string]: unknown
}

export const resolveImageStudioDraftPersistence = (
  mode: ImageStudioComposerMode,
  initialized: boolean,
  userId: number,
  values: ImageStudioDraftCandidate
): ImageStudioDraftPersistence => {
  if (mode !== 'generation' || !initialized || userId <= 0) {
    return { action: 'skip' }
  }
  if ((values.prompt ?? '').trim() === '') return { action: 'clear' }
  const parsed = imageComposerSchema.safeParse(values)
  if (!parsed.success) return { action: 'skip' }
  return { action: 'save', draft: parsed.data }
}
