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
import Claude from '@lobehub/icons/es/Claude/components/Mono'
import DeepSeek from '@lobehub/icons/es/DeepSeek/components/Mono'
import OpenAI from '@lobehub/icons/es/OpenAI/components/Mono'
import Zhipu from '@lobehub/icons/es/Zhipu/components/Mono'
import { Check, ChevronDown, Layers, Loader2, Route } from 'lucide-react'
import type { ComponentProps, ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'

import type { ApiKeyGroupFamily } from '../lib/api-key-group-family'
import type { ApiKeyGroupOption } from './api-key-group-combobox'

import './api-key-group-picker.css'

export function ApiKeyGroupBrand(props: {
  family?: ApiKeyGroupFamily
  size?: number
}) {
  const { family, size = 16 } = props
  let icon: ReactNode
  switch (family?.name) {
    case 'OpenAI':
      icon = <OpenAI size={size} />
      break
    case 'Claude':
      icon = <Claude size={size} />
      break
    case 'DeepSeek':
      icon = <DeepSeek size={size} />
      break
    case 'GLM':
      icon = <Zhipu size={size} />
      break
    default:
      if (family?.icon) {
        icon = getLobeIcon(family.icon.split('.')[0], size)
      } else {
        const Icon = family?.kind === 'auto' ? Route : Layers
        icon = <Icon size={size} />
      }
  }

  return (
    <span className='kgrp-brand' data-brand={family?.name} aria-hidden='true'>
      {icon}
    </span>
  )
}

export function ApiKeyGroupRatio(props: { ratio?: number | string }) {
  const { t } = useTranslation()
  if (props.ratio == null || props.ratio === '') return null
  return (
    <span className='kgrp-ratio'>
      {typeof props.ratio === 'number' ? `${props.ratio}×` : t('Auto')}
    </span>
  )
}

type ApiKeyGroupTriggerProps = ComponentProps<'button'> & {
  option?: ApiKeyGroupOption
  family?: ApiKeyGroupFamily
  familyLabel?: string
  placeholder?: string
  compact?: boolean
  pending?: boolean
  confirmed?: boolean
  crossGroupRetry?: boolean
  health?: ReactNode
}

export function ApiKeyGroupTrigger({
  option,
  family,
  familyLabel,
  placeholder,
  health,
  compact,
  pending,
  confirmed,
  crossGroupRetry,
  className,
  disabled,
  ...buttonProps
}: ApiKeyGroupTriggerProps) {
  const { t } = useTranslation()
  let HandleIcon = ChevronDown
  if (confirmed) HandleIcon = Check
  if (pending) HandleIcon = Loader2

  return (
    <button
      type='button'
      {...buttonProps}
      className={cn('kgrp-trigger', className)}
      data-compact={compact || undefined}
      data-empty={!option || undefined}
      data-confirmed={confirmed || undefined}
      aria-busy={pending || undefined}
      disabled={disabled || pending}
    >
      <span className='kgrp-card'>
        <span className='kgrp-stamp'>
          <ApiKeyGroupBrand family={family} size={compact ? 21 : 28} />
        </span>
        <span className='kgrp-identity'>
          {option && (
            <span className='kgrp-eyebrow'>
              {t('Current group')}
              {familyLabel && (
                <>
                  <span className='kgrp-slash' aria-hidden='true'>
                    /
                  </span>
                  <b>{familyLabel}</b>
                </>
              )}
            </span>
          )}
          <span className='kgrp-headline'>
            <strong>
              {option?.label || placeholder || t('Select a group')}
            </strong>
            <ApiKeyGroupRatio ratio={option?.ratio} />
            {option?.disabled && (
              <span className='kgrp-unavailable'>{t('Unavailable')}</span>
            )}
          </span>
          {crossGroupRetry && (
            <span className='kgrp-cross'>{t('Cross-group')}</span>
          )}
        </span>
        {option && health}
        <span className='kgrp-handle' aria-hidden='true'>
          <HandleIcon
            size={15}
            className={pending ? 'kgrp-pending' : undefined}
          />
        </span>
      </span>
    </button>
  )
}
