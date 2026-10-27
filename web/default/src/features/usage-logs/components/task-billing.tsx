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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { DynamicPricingBreakdown } from '@/features/pricing/components/dynamic-pricing-breakdown'
import { usePricingData } from '@/features/pricing/hooks/use-pricing-data'
import { pluginUsageSchema } from '@/features/pricing/lib/plugin-pricing'
import { formatLogQuota } from '@/lib/format'

import { decodeBillingExprB64 } from '../lib/format'
import { getTaskBillingStateLabel } from '../lib/task-billing'
import type { TaskLog } from '../types'
import { BillingBreakdown } from './dialogs/details-dialog'
import { DetailRow, DetailSection } from './dialogs/log-detail-layout'
import { LogCostDisplay } from './log-cost-display'

function TaskDynamicPricing(props: { log: TaskLog }) {
  const { t } = useTranslation()
  const pricing = usePricingData()
  const modelName =
    props.log.properties?.origin_model_name ||
    props.log.properties?.upstream_model_name
  const schema = pluginUsageSchema(
    pricing.models.find((model) => model.model_name === modelName),
    props.log.admin_info?.task_plugin?.key
  )

  return (
    <DetailSection label={t('Dynamic Pricing')}>
      <DynamicPricingBreakdown
        compact
        billingExpr={decodeBillingExprB64(props.log.billing?.expr_b64)}
        usageSchema={schema}
      />
    </DetailSection>
  )
}

export function TaskBillingDetails(props: {
  log: TaskLog
  presentation?: 'video'
}) {
  const { t } = useTranslation()
  const billing = props.log.billing
  const amountAvailable = billing?.amount_available === true
  const amountLabel =
    billing?.state === 'reserved'
      ? t('Current pre-charge')
      : t('Current charge')
  const hasPricing =
    billing?.pricing_epoch === 'usd_credit_v1' &&
    (billing.model_price != null ||
      billing.model_ratio != null ||
      Boolean(billing.expr_b64))

  return (
    <div
      className={
        props.presentation === 'video'
          ? 'video-work-billing-details'
          : 'flex flex-col gap-3'
      }
    >
      {props.presentation === 'video' ? (
        <div className='video-work-billing-total'>
          <p className='text-muted-foreground text-xs'>{amountLabel}</p>
          <div className='flex flex-wrap items-center justify-between gap-3'>
            {amountAvailable ? (
              <LogCostDisplay
                quota={props.log.quota}
                other={billing ?? null}
                logType={2}
                showBillingSource
                prominent
              />
            ) : (
              <strong
                className='text-[38px] font-semibold'
                aria-label={t('Amount unavailable')}
              >
                —
              </strong>
            )}
            <span
              className='video-work-billing-state'
              data-state={billing?.state}
            >
              {getTaskBillingStateLabel(billing?.state, t)}
            </span>
          </div>
        </div>
      ) : (
        <DetailSection label={amountLabel}>
          {amountAvailable ? (
            <LogCostDisplay
              quota={props.log.quota}
              other={billing ?? null}
              logType={2}
              showBillingSource
            />
          ) : (
            <span aria-label={t('Amount unavailable')}>—</span>
          )}
          <DetailRow
            label={t('Billing status')}
            value={getTaskBillingStateLabel(billing?.state, t)}
          />
        </DetailSection>
      )}
      {hasPricing ? (
        <>
          <BillingBreakdown
            log={props.log}
            other={billing}
            isAdmin={false}
            totalLabel={amountLabel}
            totalAvailable={amountAvailable}
            tierLabel={t('Estimated tier')}
          />
          {billing.seconds != null && (
            <DetailRow
              label={t('Billable duration')}
              value={`${billing.seconds} ${t('second')}`}
            />
          )}
          {billing.duration != null && (
            <DetailRow
              label={t('Duration multiplier')}
              value={`${billing.duration}×`}
            />
          )}
          {billing.size != null && (
            <DetailRow
              label={t('Size multiplier')}
              value={`${billing.size}×`}
            />
          )}
          {billing.resolution != null && (
            <DetailRow
              label={t('Resolution multiplier')}
              value={`${billing.resolution}×`}
            />
          )}
          {billing.image_count != null && (
            <DetailRow
              label={t('Billable image count')}
              value={billing.image_count}
            />
          )}
          {billing.billing_mode === 'tiered_expr' && billing.expr_b64 && (
            <TaskDynamicPricing log={props.log} />
          )}
        </>
      ) : (
        <p className='text-muted-foreground text-xs'>
          {t('Billing details unavailable for this record')}
        </p>
      )}
      <p className='text-muted-foreground text-xs'>
        {t(
          'The original pre-charge and refund history were not recorded here.'
        )}
      </p>
    </div>
  )
}

export function TaskBillingCell(props: { log: TaskLog }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  return (
    <>
      <div className='flex flex-col items-start gap-1'>
        <span className='font-mono text-sm font-medium tabular-nums'>
          {props.log.billing?.amount_available === true
            ? formatLogQuota(props.log.quota)
            : '—'}
        </span>
        <Button variant='ghost' size='xs' onClick={() => setOpen(true)}>
          {t('Billing Details')}
        </Button>
      </div>
      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={t('Billing Details')}
        description={
          props.log.properties?.origin_model_name || props.log.task_id
        }
        contentClassName='sm:max-w-lg'
        contentHeight='auto'
      >
        {open && <TaskBillingDetails log={props.log} />}
      </Dialog>
    </>
  )
}
