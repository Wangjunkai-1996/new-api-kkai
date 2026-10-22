import type { TFunction } from 'i18next'

import type { RequestStatus } from './api'

export const REQUEST_STATUSES: RequestStatus[] = [
  'pending',
  'accepted',
  'scheduling',
  'developing',
  'completed',
  'rejected',
]

export function requestStatusLabel(
  status: RequestStatus,
  t: TFunction
): string {
  switch (status) {
    case 'pending':
      return t('Awaiting review')
    case 'accepted':
      return t('Accepted · awaiting scheduling')
    case 'scheduling':
      return t('Scheduling')
    case 'developing':
      return t('In development')
    case 'completed':
      return t('Completed')
    case 'rejected':
      return t('Rejected')
  }
}

export function requestStatusDescription(
  status: RequestStatus,
  t: TFunction
): string {
  switch (status) {
    case 'pending':
      return t('Your request has arrived and is awaiting review.')
    case 'accepted':
      return t('Your idea has been accepted. Scheduling has not started yet.')
    case 'scheduling':
      return t(
        'We are assessing the work and arranging an implementation plan.'
      )
    case 'developing':
      return t('Your request is being developed. Updates will appear here.')
    case 'completed':
      return t('This request is complete. See the feedback for details.')
    case 'rejected':
      return t(
        'This request was not accepted. See the feedback for the reason.'
      )
  }
}

export function requestStatusActions(status: RequestStatus): RequestStatus[] {
  switch (status) {
    case 'pending':
      return ['accepted', 'rejected']
    case 'accepted':
      return ['scheduling']
    case 'scheduling':
      return ['developing']
    case 'developing':
      return ['completed']
    default:
      return []
  }
}
