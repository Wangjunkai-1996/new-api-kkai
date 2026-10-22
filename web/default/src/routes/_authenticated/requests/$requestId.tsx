import { createFileRoute, notFound } from '@tanstack/react-router'
import { z } from 'zod'

import { RequestDetailPage } from '@/features/requests/detail'

const searchSchema = z.object({
  event: z.coerce.number().int().positive().optional().catch(undefined),
  submitted: z.boolean().optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/requests/$requestId')({
  validateSearch: searchSchema,
  beforeLoad: ({ params }) => {
    if (
      !/^\d+$/.test(params.requestId) ||
      !Number.isSafeInteger(Number(params.requestId)) ||
      Number(params.requestId) <= 0
    ) {
      throw notFound()
    }
  },
  component: RequestDetailRoute,
})

function RequestDetailRoute() {
  const params = Route.useParams()
  const search = Route.useSearch()
  return (
    <RequestDetailPage
      key={params.requestId}
      requestId={Number(params.requestId)}
      eventId={search.event}
      submitted={search.submitted}
    />
  )
}
