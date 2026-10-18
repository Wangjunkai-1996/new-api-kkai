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

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'

import type { MembershipModelLimit } from '../types'

export function MembershipModelLimitEditor(props: {
  value: Record<string, MembershipModelLimit>
  onChange: (value: Record<string, MembershipModelLimit>) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  return (
    <section className='space-y-4'>
      <h3 className='font-semibold'>{t('Request authorization limits')}</h3>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Register verified cumulative limits for each model, including server-side history and repeated tool calls. Unknown limits must not be guessed.'
        )}
      </p>
      {Object.entries(props.value).map(([model, limit], index) => (
        <div key={model} className='space-y-3 rounded-xl border p-4'>
          <div className='flex items-center justify-between gap-3'>
            <p className='font-medium break-all'>{model}</p>
            <Button
              type='button'
              variant='ghost'
              size='sm'
              disabled={props.disabled}
              onClick={() => {
                const next = { ...props.value }
                delete next[model]
                props.onChange(next)
              }}
            >
              {t('Remove')}
            </Button>
          </div>
          <div className='grid gap-3 sm:grid-cols-3'>
            {(
              [
                {
                  key: 'max_input_tokens',
                  label: t('Maximum cumulative input tokens'),
                },
                { key: 'max_output_tokens', label: t('Maximum output tokens') },
                { key: 'max_tool_calls', label: t('Maximum tool calls') },
              ] as const
            ).map(({ key, label }) => (
              <div key={key} className='space-y-1.5'>
                <label
                  className='text-sm'
                  htmlFor={`model-limit-${index}-${key}`}
                >
                  {label}
                </label>
                <Input
                  id={`model-limit-${index}-${key}`}
                  type='number'
                  min={0}
                  max={2147483647}
                  step={1}
                  value={Number.isFinite(limit[key]) ? limit[key] : ''}
                  disabled={props.disabled}
                  onChange={(event) =>
                    props.onChange({
                      ...props.value,
                      [model]: {
                        ...limit,
                        [key]:
                          event.target.value === ''
                            ? Number.NaN
                            : Number(event.target.value),
                      },
                    })
                  }
                />
              </div>
            ))}
          </div>
          <TaskUsageLimitEditor
            modelIndex={index}
            value={limit.task_usage_limits ?? {}}
            disabled={props.disabled}
            onChange={(value) =>
              props.onChange({
                ...props.value,
                [model]: { ...limit, task_usage_limits: value },
              })
            }
          />
          <div className='space-y-1.5'>
            <label
              className='text-sm'
              htmlFor={`model-limit-evidence-${index}`}
            >
              {t('Verified limit source')}
            </label>
            <Textarea
              id={`model-limit-evidence-${index}`}
              value={limit.evidence}
              maxLength={2048}
              disabled={props.disabled}
              onChange={(event) =>
                props.onChange({
                  ...props.value,
                  [model]: { ...limit, evidence: event.target.value },
                })
              }
            />
          </div>
        </div>
      ))}
      <div className='flex flex-wrap gap-3'>
        <Input
          className='max-w-sm'
          value={name}
          onChange={(event) => setName(event.target.value)}
          aria-label={t('Model name')}
          placeholder={t('Model name')}
          disabled={props.disabled}
        />
        <Button
          variant='outline'
          type='button'
          disabled={
            props.disabled ||
            !name.trim() ||
            Object.hasOwn(props.value, name.trim())
          }
          onClick={() => {
            props.onChange({
              ...props.value,
              [name.trim()]: {
                max_input_tokens: 0,
                max_output_tokens: 0,
                max_tool_calls: 0,
                evidence: '',
              },
            })
            setName('')
          }}
        >
          {t('Add model limit')}
        </Button>
      </div>
    </section>
  )
}

function TaskUsageLimitEditor(props: {
  modelIndex: number
  value: Record<string, number>
  onChange: (value: Record<string, number>) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const [field, setField] = useState('')
  return (
    <details className='rounded-lg border p-3'>
      <summary className='cursor-pointer text-sm'>
        {t('Task usage bounds')}
      </summary>
      <div className='mt-3 space-y-3'>
        {Object.entries(props.value).map(([key, value], index) => (
          <div key={key} className='flex flex-wrap items-end gap-2'>
            <div className='min-w-0 flex-1 space-y-1'>
              <label
                htmlFor={`task-bound-${props.modelIndex}-${index}`}
                className='block text-sm break-all'
              >
                {key}
              </label>
              <Input
                id={`task-bound-${props.modelIndex}-${index}`}
                type='number'
                min={0}
                max={2147483647}
                step='any'
                value={Number.isFinite(value) ? value : ''}
                disabled={props.disabled}
                onChange={(event) =>
                  props.onChange({
                    ...props.value,
                    [key]:
                      event.target.value === ''
                        ? Number.NaN
                        : Number(event.target.value),
                  })
                }
              />
            </div>
            <Button
              type='button'
              variant='ghost'
              disabled={props.disabled}
              onClick={() => {
                const next = { ...props.value }
                delete next[key]
                props.onChange(next)
              }}
            >
              {t('Remove')}
            </Button>
          </div>
        ))}
        <div className='flex flex-wrap gap-2'>
          <Input
            className='max-w-xs'
            aria-label={t('Usage field')}
            placeholder={t('Usage field')}
            value={field}
            onChange={(event) => setField(event.target.value)}
            disabled={props.disabled}
          />
          <Button
            variant='outline'
            type='button'
            disabled={
              props.disabled ||
              !field.trim() ||
              Object.hasOwn(props.value, field.trim())
            }
            onClick={() => {
              props.onChange({ ...props.value, [field.trim()]: 0 })
              setField('')
            }}
          >
            {t('Add usage limit')}
          </Button>
        </div>
      </div>
    </details>
  )
}
