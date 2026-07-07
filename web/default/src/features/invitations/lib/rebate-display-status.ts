import type { TFunction } from 'i18next'

import type { RebateDisplayStatus, RebateStatus } from '../types'

const REBATE_DISPLAY_STATUS_CLASSES: Record<RebateDisplayStatus, string> = {
  estimated: 'bg-amber-500/10 text-amber-700 dark:text-amber-400',
  claimable: 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
  requested: 'bg-sky-500/10 text-sky-700 dark:text-sky-400',
  approved: 'bg-indigo-500/10 text-indigo-700 dark:text-indigo-400',
  paid: 'bg-gray-500/10 text-gray-700 dark:text-gray-400',
  waiting_unlock: 'bg-blue-500/10 text-blue-700 dark:text-blue-400',
}

export function deriveRebateDisplayStatus(
  status: RebateStatus
): RebateDisplayStatus {
  switch (status) {
    case 'pending':
      return 'claimable'
    case 'requested':
      return 'requested'
    case 'approved':
      return 'approved'
    case 'completed':
      return 'paid'
    case 'rejected':
      return 'estimated'
  }
}

export function getRebateDisplayStatusClass(
  status: RebateDisplayStatus
): string {
  return REBATE_DISPLAY_STATUS_CLASSES[status]
}

export function getRebateDisplayStatusLabel(
  t: TFunction,
  status: RebateDisplayStatus
): string {
  const labels: Record<RebateDisplayStatus, string> = {
    estimated: t('Estimated Rebate'),
    claimable: t('Claimable Rebate'),
    requested: t('Requested Rebate'),
    approved: t('Approved Rebate'),
    paid: t('Paid Rebate'),
    waiting_unlock: t(
      'Pending rebate (waiting for first top-up/subscription to unlock)'
    ),
  }

  return labels[status]
}
