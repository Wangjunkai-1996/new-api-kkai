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
import type { TFunction } from 'i18next'

import type { ResellerWithdrawal } from './api'

export function withdrawalStatusLabel(
  status: ResellerWithdrawal['status'],
  t: TFunction
): string {
  switch (status) {
    case 'requested':
      return t('Requested')
    case 'approved':
      return t('Approved')
    case 'paid':
      return t('Paid')
    case 'rejected':
      return t('Rejected')
    case 'failed':
      return t('Failed')
  }
}

export function applicationStatusLabel(
  status: 'pending' | 'approved' | 'rejected',
  t: TFunction
): string {
  switch (status) {
    case 'pending':
      return t('Pending')
    case 'approved':
      return t('Approved')
    case 'rejected':
      return t('Rejected')
  }
}
