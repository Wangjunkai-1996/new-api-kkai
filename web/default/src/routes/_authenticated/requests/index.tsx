import { createFileRoute } from '@tanstack/react-router'

import { RequestsPage } from '@/features/requests'

export const Route = createFileRoute('/_authenticated/requests/')({
  component: RequestsPage,
})
