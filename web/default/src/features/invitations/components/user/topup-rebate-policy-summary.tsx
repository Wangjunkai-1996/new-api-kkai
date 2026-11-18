import type { UseQueryResult } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { formatRebateRate } from '../../format'
import type { InvitationStats } from '../../types'

export const TopupRebatePolicySummary = (props: {
  query: UseQueryResult<InvitationStats, Error>
  orderRebateEnabled: boolean | undefined
  signupRewardEnabled: boolean
}) => {
  const { t, i18n } = useTranslation()
  const query = props.query
  const loading =
    !query.isError && (query.isPending || query.fetchStatus !== 'idle')
  const policy =
    query.isSuccess && !loading ? query.data?.topupRebatePolicy : undefined
  const disabled =
    props.orderRebateEnabled === false || policy?.state === 'disabled'
  const showOrderRules = props.orderRebateEnabled === true && !disabled
  const rate =
    showOrderRules && policy?.state === 'available'
      ? formatRebateRate(policy.ratio, i18n.resolvedLanguage ?? i18n.language)
      : null

  let message: string
  let showRetry = false
  if (disabled) {
    message = t('Order rebates are not enabled.')
  } else if (loading) {
    message = t('Loading...')
  } else if (rate !== null) {
    message = t("Rebate on a friend's top-up: {{rate}}", { rate })
  } else if (showOrderRules && policy?.state === 'not_configured') {
    message = t('No top-up rebate rule currently applies.')
  } else {
    message = t('The top-up rebate rate is temporarily unavailable.')
    showRetry = true
  }
  showRetry =
    showRetry ||
    query.isError ||
    query.data?.topupRebatePolicy?.state === 'unavailable'

  return (
    <div className='mb-4 min-w-0 space-y-2 break-words'>
      <div role='status'>
        <p className='font-semibold'>{message}</p>
        {rate !== null && (
          <p className='text-muted-foreground text-sm'>
            {t("Based on the credits actually added to your friend's account.")}
          </p>
        )}
      </div>
      {showRetry && (
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          {t('Retry')}
        </Button>
      )}
      {(showOrderRules || props.signupRewardEnabled) && (
        <details className='text-muted-foreground text-sm'>
          <summary className='focus-visible:ring-ring w-fit cursor-pointer rounded-sm focus-visible:ring-2'>
            {t('Other reward rules')}
          </summary>
          {showOrderRules && (
            <p className='mt-1'>
              {t(
                'For subscriptions and redemptions, see each record for any rebate and its rate.'
              )}
            </p>
          )}
          {props.signupRewardEnabled && (
            <p className='mt-1'>
              {t(
                'Sign-up rewards are fixed credits, not a percentage of a top-up.'
              )}
            </p>
          )}
        </details>
      )}
    </div>
  )
}
