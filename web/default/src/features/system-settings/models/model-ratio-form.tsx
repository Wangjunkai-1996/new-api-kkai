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
import { Code2, Eye, RotateCcw, Save } from 'lucide-react'
import { memo, useCallback, useRef, useState } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { JsonCodeEditor } from '@/components/json-code-editor'
import { LearnMore } from '@/components/learn-more'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'
import type { ModelPricingEntry } from '@/features/model-pricing/api'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageActionsPortal } from '../components/settings-page-context'
import {
  ModelRatioVisualEditor,
  type ModelRatioVisualEditorHandle,
} from './model-ratio-visual-editor'

type ModelFormValues = {
  ModelPrice: string
  ModelRatio: string
  CacheRatio: string
  CreateCacheRatio: string
  CompletionRatio: string
  ImageRatio: string
  AudioRatio: string
  AudioCompletionRatio: string
  ExposeRatioEnabled: boolean
  BillingMode: string
  BillingExpr: string
  DisplayBillingExpr: string
  PluginBillingExpr: string
}

type ModelRatioFormProps = {
  form: UseFormReturn<ModelFormValues>
  savedValues: ModelFormValues
  onSave: (values: ModelFormValues) => Promise<void>
  onReset: () => void
  isSaving: boolean
  isResetting: boolean
  variant?: 'default' | 'unset'
  channelModelNames?: string[]
  pricingEntries?: ModelPricingEntry[]
}

type ModelJsonFieldName =
  | 'BillingExpr'
  | 'DisplayBillingExpr'
  | 'PluginBillingExpr'

const modelJsonFields: Array<{
  name: ModelJsonFieldName
  labelKey: string
}> = [
  { name: 'BillingExpr', labelKey: 'Billing expression' },
  { name: 'DisplayBillingExpr', labelKey: 'Display billing expression' },
  { name: 'PluginBillingExpr', labelKey: 'Provider pricing' },
]

function ModelJsonTextareaField(props: {
  form: UseFormReturn<ModelFormValues>
  name: ModelJsonFieldName
  label: string
}) {
  return (
    <FormField
      control={props.form.control}
      name={props.name}
      render={({ field }) => (
        <FormItem className='flex min-w-0 flex-col gap-2'>
          <FormLabel>{props.label}</FormLabel>
          <FormControl>
            <JsonCodeEditor
              value={field.value}
              onChange={(value) => field.onChange(value)}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

export const ModelRatioForm = memo(function ModelRatioForm({
  form,
  savedValues,
  onSave,
  onReset,
  isSaving,
  isResetting,
  variant = 'default',
  channelModelNames,
  pricingEntries,
}: ModelRatioFormProps) {
  const { t } = useTranslation()
  const isUnsetVariant = variant === 'unset'
  const [editMode, setEditMode] = useState<'visual' | 'json'>('visual')
  const visualEditorRef = useRef<ModelRatioVisualEditorHandle>(null)

  const handleFieldChange = useCallback(
    (field: keyof ModelFormValues, value: string) => {
      form.setValue(field, value, {
        shouldValidate: true,
        shouldDirty: true,
      })
    },
    [form]
  )

  const toggleEditMode = useCallback(() => {
    setEditMode((prev) => (prev === 'visual' ? 'json' : 'visual'))
  }, [])

  const handleSave = useCallback(async () => {
    if (editMode === 'visual') {
      const committed = await visualEditorRef.current?.commitOpenEditor()
      if (committed === false) return
    }

    await form.handleSubmit(async (values) => {
      if (editMode !== 'json') return onSave(values)
      const previous = JSON.parse(savedValues.BillingExpr) as Record<
        string,
        string
      >
      const expressions = JSON.parse(values.BillingExpr) as Record<
        string,
        string
      >
      const modes = JSON.parse(values.BillingMode) as Record<string, string>
      for (const [name, expression] of Object.entries(expressions)) {
        if (expression !== previous[name]) modes[name] = 'tiered_expr'
      }
      await onSave({ ...values, BillingMode: JSON.stringify(modes) })
    })()
  }, [editMode, form, onSave, savedValues.BillingExpr])

  return (
    <Form {...form}>
      <div className='flex min-h-0 flex-1 flex-col gap-6'>
        {!isUnsetVariant && (
          <div className='flex shrink-0 flex-wrap items-center justify-end gap-2'>
            <SettingsPageActionsPortal>
              <FormField
                control={form.control}
                name='ExposeRatioEnabled'
                render={({ field }) => (
                  <SettingsSwitchItem className='gap-2 py-0'>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Expose ratio API')}</FormLabel>
                      <FormDescription className='sr-only'>
                        {t(
                          'Allow clients to query configured prices via `/api/ratio`.'
                        )}
                      </FormDescription>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                    <LearnMore contentProps={{ side: 'bottom', align: 'end' }}>
                      {t(
                        'Allow clients to query configured prices via `/api/ratio`.'
                      )}
                    </LearnMore>
                  </SettingsSwitchItem>
                )}
              />
            </SettingsPageActionsPortal>
            <Button
              type='button'
              variant='destructive'
              size='sm'
              onClick={onReset}
              disabled={isResetting}
            >
              <RotateCcw data-icon='inline-start' />
              {t('Reset prices')}
            </Button>
            {editMode === 'json' && (
              <Button
                type='button'
                size='sm'
                onClick={handleSave}
                disabled={isSaving}
              >
                <Save data-icon='inline-start' />
                {isSaving ? t('Saving...') : t('Save model prices')}
              </Button>
            )}
            <Button variant='outline' size='sm' onClick={toggleEditMode}>
              {editMode === 'visual' ? (
                <>
                  <Code2 className='mr-2 h-4 w-4' />
                  {t('Switch to JSON')}
                </>
              ) : (
                <>
                  <Eye className='mr-2 h-4 w-4' />
                  {t('Switch to Visual')}
                </>
              )}
            </Button>
          </div>
        )}

        {editMode === 'visual' ? (
          <div className='flex min-h-0 flex-1 flex-col gap-6'>
            <ModelRatioVisualEditor
              ref={visualEditorRef}
              savedModelPrice={savedValues.ModelPrice}
              savedModelRatio={savedValues.ModelRatio}
              savedCacheRatio={savedValues.CacheRatio}
              savedCreateCacheRatio={savedValues.CreateCacheRatio}
              savedCompletionRatio={savedValues.CompletionRatio}
              savedImageRatio={savedValues.ImageRatio}
              savedAudioRatio={savedValues.AudioRatio}
              savedAudioCompletionRatio={savedValues.AudioCompletionRatio}
              savedBillingMode={savedValues.BillingMode}
              savedBillingExpr={savedValues.BillingExpr}
              savedDisplayBillingExpr={savedValues.DisplayBillingExpr}
              savedPluginBillingExpr={savedValues.PluginBillingExpr}
              modelPrice={form.watch('ModelPrice')}
              modelRatio={form.watch('ModelRatio')}
              cacheRatio={form.watch('CacheRatio')}
              createCacheRatio={form.watch('CreateCacheRatio')}
              completionRatio={form.watch('CompletionRatio')}
              imageRatio={form.watch('ImageRatio')}
              audioRatio={form.watch('AudioRatio')}
              audioCompletionRatio={form.watch('AudioCompletionRatio')}
              billingMode={form.watch('BillingMode')}
              billingExpr={form.watch('BillingExpr')}
              displayBillingExpr={form.watch('DisplayBillingExpr')}
              pluginBillingExpr={form.watch('PluginBillingExpr')}
              candidateModelNames={channelModelNames}
              pricingEntries={pricingEntries}
              filterMode={isUnsetVariant ? 'unset' : 'all'}
              onSave={handleSave}
              isSaving={isSaving}
              onChange={(field, value) => {
                const fieldMap: Record<string, keyof ModelFormValues> = {
                  'billing_setting.billing_mode': 'BillingMode',
                  'billing_setting.billing_expr': 'BillingExpr',
                  'billing_setting.display_billing_expr': 'DisplayBillingExpr',
                  'billing_setting.plugin_billing_expr': 'PluginBillingExpr',
                }
                const formField =
                  fieldMap[field] || (field as keyof ModelFormValues)
                handleFieldChange(formField, value)
              }}
            />
          </div>
        ) : (
          <SettingsForm
            onSubmit={(event) => {
              event.preventDefault()
              void handleSave()
            }}
          >
            <div className='grid min-w-0 gap-x-5 gap-y-8 lg:grid-cols-2 2xl:grid-cols-3'>
              {modelJsonFields.map((config) => (
                <ModelJsonTextareaField
                  key={config.name}
                  form={form}
                  name={config.name}
                  label={t(config.labelKey)}
                />
              ))}
            </div>
          </SettingsForm>
        )}
      </div>
    </Form>
  )
})
