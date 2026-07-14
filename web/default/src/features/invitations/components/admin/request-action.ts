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
import {
  approveAndPayRebateRequest,
  approveRebateRequest,
  batchApproveAndPayRebateRequests,
  completeRebateRequest,
  rejectRebateRequest,
  resetRebateRequestReview,
  undoCompleteRebateRequest,
} from '../../api'
import { requireInvitationData } from '../../api/result'
import type { RebateRequestAction } from './request-action-dialog'

export const runRequestAction = async (action: RebateRequestAction) => {
  if (action.kind === 'approve') {
    return requireInvitationData(
      await approveRebateRequest(action.requestId),
      'Failed to approve request'
    )
  }
  if (action.kind === 'approve-pay') {
    return requireInvitationData(
      await approveAndPayRebateRequest(action.requestId, action.command),
      'Failed to approve and pay request'
    )
  }
  if (action.kind === 'batch-approve-pay') {
    return requireInvitationData(
      await batchApproveAndPayRebateRequests(action.requestIds, action.command),
      'Failed to approve and pay requests'
    )
  }
  if (action.kind === 'reject') {
    return requireInvitationData(
      await rejectRebateRequest(action.requestId, action.reason),
      'Failed to reject request'
    )
  }
  if (action.kind === 'reset') {
    return requireInvitationData(
      await resetRebateRequestReview(action.requestId),
      'Failed to reset request'
    )
  }
  if (action.kind === 'complete') {
    return requireInvitationData(
      await completeRebateRequest(action.requestId),
      'Failed to complete request'
    )
  }
  return requireInvitationData(
    await undoCompleteRebateRequest(action.requestId),
    'Failed to undo completed request'
  )
}
