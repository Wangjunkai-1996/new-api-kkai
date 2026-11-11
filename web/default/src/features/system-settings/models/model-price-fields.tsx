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
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import type { PricingCurrency } from '@/features/model-pricing/currency'
import {
  pricingRow,
  type CacheWriteMode,
  type LegacyBillingDetails,
  type PricingValues,
} from '@/features/model-pricing/pricing'
import { PricingAmountInput } from '@/features/model-pricing/pricing-amount-input'
import {
  CLAUDE_CACHE_CREATION_1H_MULTIPLIER,
  OPENAI_CACHE_CREATION_12H_MULTIPLIER,
} from '@/features/pricing/lib/billing-expr'

import {
  createInitialLaneState,
  hasValue,
  laneConfigs,
  ratioFieldByLane,
  type ModelPricingFormValues,
} from './model-pricing-core'
import { PriceInput } from './model-pricing-inputs'
import { formatPricingNumber } from './pricing-format'

export function ModelPriceFields(props: {
  values: ModelPricingFormValues
  effective?: {
    effective: PricingValues
    cacheWriteMode?: CacheWriteMode
    billingDetails?: LegacyBillingDetails
  }
  mode: 'per-token' | 'per-request'
  currency: PricingCurrency
  billingUnit?: 'second' | 'request'
  onChange: (field: keyof ModelPricingFormValues, value: string) => void
}) {
  const { t } = useTranslation()
  const id = useId()
  const [emptyField, setEmptyField] = useState<
    keyof ModelPricingFormValues | null
  >(null)
  const resolved = pricingRow(
    props.values.name,
    props.effective?.effective ?? {}
  )
  for (const field of ['ratio', ...Object.values(ratioFieldByLane)] as const) {
    const value = props.values[field]
    if (value !== undefined && hasValue(value)) resolved[field] = value
  }
  const lanes = createInitialLaneState(resolved)
  const basePrice = Number(lanes.promptPrice)
  const details = props.effective?.billingDetails
  const cacheMode = props.effective?.cacheWriteMode
  let cacheTTLLabel = ''
  let cacheTTLMultiplier = 1
  if (cacheMode === 'claude_ttl') {
    cacheTTLLabel = t('Cache create (1h) price')
    cacheTTLMultiplier = CLAUDE_CACHE_CREATION_1H_MULTIPLIER
  } else if (cacheMode === 'openai_ttl') {
    cacheTTLLabel = t('Cache create (12h) price')
    cacheTTLMultiplier = OPENAI_CACHE_CREATION_12H_MULTIPLIER
  }

  if (props.mode === 'per-request') {
    const label = details?.image_count ? t('Price per image') : t('Fixed price')
    let unit = t('request')
    if (props.billingUnit === 'second') unit = t('second')
    if (details?.image_count) unit = t('image')
    return (
      <Field>
        <FieldLabel htmlFor={`${id}-price`}>{label}</FieldLabel>
        <PricingAmountInput
          id={`${id}-price`}
          currency={props.currency}
          value={
            emptyField === 'price'
              ? ''
              : props.values.price || resolved.price || ''
          }
          onChange={(value) => {
            setEmptyField(value === '' ? 'price' : null)
            props.onChange('price', value)
          }}
        />
        <FieldDescription>
          {props.currency.symbol} / {unit}
        </FieldDescription>
      </Field>
    )
  }

  return (
    <FieldGroup className='grid min-w-0 gap-4 @min-[540px]/pricing-editor:grid-cols-2'>
      <Field>
        <FieldLabel htmlFor={`${id}-input`}>{t('Input price')}</FieldLabel>
        <PriceInput
          id={`${id}-input`}
          currency={props.currency}
          value={emptyField === 'ratio' ? '' : lanes.promptPrice}
          onChange={(value) => {
            setEmptyField(value === '' ? 'ratio' : null)
            props.onChange(
              'ratio',
              value === '' ? '' : String(Number(value) / 2)
            )
          }}
        />
      </Field>
      {laneConfigs.map((lane) => {
        const field = ratioFieldByLane[lane.key]
        const denominator =
          lane.key === 'audioOutput'
            ? Number(lanes.prices.audioInput)
            : basePrice
        const missingBase = !Number.isFinite(denominator) || denominator <= 0
        let label = t(lane.titleKey)
        if (lane.key === 'createCache' && cacheMode === 'claude_ttl') {
          label = t('Cache Creation (5m)')
        }
        if (lane.key === 'createCache' && cacheMode === 'openai_ttl') {
          label = t('Cache Creation (30m)')
        }
        let value = lanes.prices[lane.key]
        let providerPrice: number | undefined
        if (lane.key === 'audioInput' && !details?.audio_text_branches) {
          providerPrice = details?.audio_input_price
        }
        const providerControlled = providerPrice !== undefined
        if (providerPrice !== undefined) {
          value = formatPricingNumber(providerPrice)
        }
        let description = t(lane.descriptionKey)
        if (providerControlled) {
          description = t(
            'Audio and text-only requests keep their respective pricing.'
          )
        } else if (missingBase) {
          description = t(
            'Input price is required before saving dependent prices.'
          )
        }
        return (
          <Field
            key={lane.key}
            data-disabled={missingBase || providerControlled || undefined}
          >
            <div className='flex min-h-6 items-center justify-between gap-2'>
              <FieldLabel htmlFor={`${id}-${lane.key}`}>{label}</FieldLabel>
              {hasValue(props.values[field]) && !providerControlled ? (
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  className='h-6 px-2 text-xs'
                  onClick={() => {
                    setEmptyField(null)
                    props.onChange(field, '')
                  }}
                >
                  {t('Use default')}
                </Button>
              ) : (
                <span className='text-muted-foreground text-xs'>
                  {t('Inherited')}
                </span>
              )}
            </div>
            <PriceInput
              id={`${id}-${lane.key}`}
              aria-describedby={`${id}-${lane.key}-description`}
              currency={props.currency}
              value={emptyField === field ? '' : value}
              disabled={missingBase || providerControlled}
              onChange={(amount) => {
                setEmptyField(amount === '' ? field : null)
                props.onChange(
                  field,
                  amount === '' ? '' : String(Number(amount) / denominator)
                )
              }}
            />
            <FieldDescription id={`${id}-${lane.key}-description`}>
              {description}
            </FieldDescription>
          </Field>
        )
      })}
      {cacheTTLLabel && hasValue(lanes.prices.createCache) && (
        <Field data-disabled>
          <FieldLabel htmlFor={`${id}-cache-ttl`}>{cacheTTLLabel}</FieldLabel>
          <PriceInput
            id={`${id}-cache-ttl`}
            currency={props.currency}
            value={formatPricingNumber(
              Number(lanes.prices.createCache) * cacheTTLMultiplier
            )}
            disabled
            onChange={() => {}}
          />
        </Field>
      )}
    </FieldGroup>
  )
}
