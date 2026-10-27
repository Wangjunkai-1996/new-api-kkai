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

import type { TaskLog } from '../types'

export function videoWorkFailure(log: TaskLog, t: TFunction) {
  let code = log.failure?.code
  // Older servers have no safe public snapshot. Classify only known text;
  // never display the potentially unredacted legacy diagnostic verbatim.
  if (!code && /real person|privacy/i.test(log.fail_reason ?? '')) {
    code = 'privacy_restriction'
  }
  switch (code) {
    case 'privacy_restriction':
      return {
        title: t('Input material triggered a privacy restriction'),
        explanation: t(
          'The provider detected a possible real person or restricted private content in the input material.'
        ),
        guidance: t(
          'Check the referenced input material and replace it with material allowed by this model before submitting again.'
        ),
      }
    case 'copyright_restriction':
      return {
        title: t('Possible copyright restriction'),
        guidance: t('videoStudio.failure.copyrightRestriction'),
      }
    case 'content_policy_violation':
      return {
        title: t('Content did not pass the safety review'),
        guidance: t('videoStudio.failure.contentPolicy'),
      }
    case 'input_invalid':
      return {
        title: t('Input parameters do not meet the requirements'),
        guidance: t('videoStudio.failure.inputInvalid'),
      }
    case 'unsupported_media':
      return {
        title: t('Unsupported input media format'),
        guidance: t('videoStudio.failure.unsupportedMedia'),
      }
    case 'media_unavailable':
      return {
        title: t('Input media could not be read'),
        guidance: t('videoStudio.failure.mediaUnavailable'),
      }
    case 'billing_failure':
      return {
        title: t('Billing could not be completed'),
        guidance: t('videoStudio.failure.billingFailure'),
      }
    case 'quota_exceeded':
      return {
        title: t('Generation quota is insufficient'),
        guidance: t('videoStudio.failure.quotaExceeded'),
      }
    case 'temporary_failure':
      return {
        title: t('The generation service is temporarily unavailable'),
        guidance: t('videoStudio.failure.temporaryFailure'),
      }
    case 'task_expired':
      return {
        title: t('The generation task has expired'),
        guidance: t('videoStudio.failure.taskExpired'),
      }
    case 'submission_failure':
      return {
        title: t('The generation request was not accepted'),
        guidance: t('videoStudio.failure.submissionFailure'),
      }
    case 'upstream_authentication':
      return {
        title: t('The generation service could not authenticate'),
        guidance: t(
          'Contact support with the task ID so the service configuration can be checked.'
        ),
      }
    case 'rate_limited':
      return {
        title: t('The generation service is busy'),
        guidance: t(
          'Wait a moment before submitting again. Avoid submitting the same request repeatedly.'
        ),
      }
    case 'generation_failure':
      return {
        title: t('The provider could not complete this generation'),
        guidance: t(
          'Review the error details and contact support with the task ID if the problem continues.'
        ),
      }
    default:
      return {
        title: t('Detailed failure information is unavailable'),
        guidance: t(
          'This historical record did not retain a detailed failure reason. Contact support with the task ID for further investigation.'
        ),
      }
  }
}
