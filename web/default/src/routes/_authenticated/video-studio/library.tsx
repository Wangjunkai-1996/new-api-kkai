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
import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'

import { VideoLibraryPage } from '@/features/video-studio'
import { videoTaskIdSchema } from '@/features/video-studio/schemas'

export const videoLibrarySearchSchema = z.object({
  task: videoTaskIdSchema.optional().catch(undefined),
  generation: z
    .union([z.string(), z.number()])
    .pipe(z.coerce.number<string | number>().int().positive().safe())
    .optional()
    .catch(undefined),
})

export const Route = createFileRoute('/_authenticated/video-studio/library')({
  validateSearch: videoLibrarySearchSchema,
  component: VideoLibraryRoute,
})

function VideoLibraryRoute() {
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  return (
    <VideoLibraryPage
      targetTaskId={search.task}
      targetGenerationId={search.generation}
      onTargetChange={(target) => {
        void navigate({
          search: {
            ...search,
            task: target.task,
            generation: target.generation,
          },
          replace: true,
        })
      }}
    />
  )
}
