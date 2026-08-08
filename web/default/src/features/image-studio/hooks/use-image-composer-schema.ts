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
import { useMemo } from 'react'

import {
  parseImageParameters,
  type ImageParameterValidationError,
} from '../image-parameters'
import { imageComposerSchema } from '../schemas'
import type { ImageModelProfile } from '../types'

const VALIDATION_KEYS: Record<ImageParameterValidationError['code'], string> = {
  required: 'imageStudio.validation.parameterRequired',
  invalid_option: 'imageStudio.validation.parameterInvalid',
  invalid_integer: 'imageStudio.validation.parameterInteger',
  out_of_range: 'imageStudio.validation.parameterRange',
  invalid_boolean: 'imageStudio.validation.parameterInvalid',
}

export function useImageComposerSchema(
  profiles: ImageModelProfile[] | undefined
) {
  return useMemo(
    () =>
      imageComposerSchema.superRefine((values, context) => {
        const profile = profiles?.find(
          (candidate) => candidate.id === values.model_profile_id
        )
        if (!profile) return
        const parsed = parseImageParameters(profile, values.parameters)
        if (parsed.success) return
        for (const error of parsed.errors) {
          context.addIssue({
            code: 'custom',
            path: ['parameters', error.parameterKey],
            message: VALIDATION_KEYS[error.code],
          })
        }
      }),
    [profiles]
  )
}
