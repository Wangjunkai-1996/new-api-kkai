import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'

import type { RequestStatus } from './api'
import { requestStatusLabel } from './status'

import '@/styles/requests.css'

export function RequestStatusBadge(props: { status: RequestStatus }) {
  const { t } = useTranslation()
  return (
    <Badge
      variant='secondary'
      data-status={props.status}
      className='request-status-tone request-status-badge h-auto max-w-full rounded-[5px] border-0 px-2 py-1 text-left text-[11px] leading-tight whitespace-normal'
    >
      <span
        aria-hidden='true'
        className='size-[5px] shrink-0 rounded-full bg-current'
      />
      {requestStatusLabel(props.status, t)}
    </Badge>
  )
}
