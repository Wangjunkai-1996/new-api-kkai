import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'

import type { RequestStatus } from './api'
import { requestStatusLabel } from './status'

export function RequestStatusBadge(props: { status: RequestStatus }) {
  const { t } = useTranslation()
  let variant: 'default' | 'secondary' | 'outline' = 'secondary'
  if (props.status === 'developing' || props.status === 'completed') {
    variant = 'default'
  }
  if (props.status === 'rejected') variant = 'outline'
  return (
    <Badge
      variant={variant}
      className='h-auto max-w-full text-left whitespace-normal'
    >
      {requestStatusLabel(props.status, t)}
    </Badge>
  )
}
