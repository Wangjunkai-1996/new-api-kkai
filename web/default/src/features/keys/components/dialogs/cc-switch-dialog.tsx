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
import { useQuery } from '@tanstack/react-query'
import { useState, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { useSystemConfig } from '@/hooks/use-system-config'
import { privateUserQueryKey } from '@/lib/private-query-cache'
import { useAuthStore } from '@/stores/auth-store'

import { getTokenModels } from '../../api'
import {
  CC_SWITCH_RECOMMENDED_MODELS,
  inferCCSwitchApp,
  type CCSwitchApp,
} from '../../lib/cc-switch-defaults'
import { buildCCSwitchURL } from '../../lib/cc-switch-url'

// Kept for existing importers and focused deep-link contract tests.
// oxlint-disable-next-line react/only-export-components
export { buildCCSwitchURL } from '../../lib/cc-switch-url'

const APP_CONFIGS = {
  claude: {
    label: 'Claude',
    modelFields: [
      { key: 'model', labelKey: 'Primary Model', required: true },
      { key: 'haikuModel', labelKey: 'Haiku Model', required: false },
      { key: 'sonnetModel', labelKey: 'Sonnet Model', required: false },
      { key: 'opusModel', labelKey: 'Opus Model', required: false },
    ],
  },
  codex: {
    label: 'Codex',
    modelFields: [{ key: 'model', labelKey: 'Primary Model', required: true }],
  },
  gemini: {
    label: 'Gemini',
    modelFields: [{ key: 'model', labelKey: 'Primary Model', required: true }],
  },
} as const

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  tokenKey: string
  tokenId: number
}

export function CCSwitchDialog(props: Props) {
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)

  return (
    <CCSwitchForm
      key={`${userId}:${props.tokenId}:${props.open}`}
      {...props}
      userId={userId}
    />
  )
}

function CCSwitchForm(props: Props & { userId: number }) {
  const { t } = useTranslation()
  const { systemName } = useSystemConfig()
  const [selectedApp, setSelectedApp] = useState<CCSwitchApp | null>(null)
  const [customName, setCustomName] = useState<string | null>(null)
  const name = customName ?? systemName
  const [modelOverrides, setModelOverrides] = useState<Record<string, string>>(
    {}
  )

  const modelsQuery = useQuery({
    queryKey: privateUserQueryKey(
      props.userId,
      'api-keys',
      'cc-switch-models',
      props.tokenId
    ),
    queryFn: async () => {
      const response = await getTokenModels(props.tokenId)
      if (!response.success) {
        throw new Error(response.message || t('Failed to fetch models'))
      }
      return response.data ?? []
    },
    enabled: props.open && props.tokenId > 0 && props.userId > 0,
    staleTime: 0,
    retry: false,
  })
  const hasAvailableModels =
    !modelsQuery.isFetching &&
    !modelsQuery.isError &&
    (modelsQuery.data?.length ?? 0) > 0

  const modelOptions = useMemo(() => {
    const items = hasAvailableModels ? (modelsQuery.data ?? []) : []
    return items.map((m) => ({ value: m, label: m }))
  }, [hasAvailableModels, modelsQuery.data])

  const inferredApp = hasAvailableModels
    ? inferCCSwitchApp(modelsQuery.data ?? [])
    : null
  const app = selectedApp ?? inferredApp ?? 'claude'
  const recommendedModel =
    selectedApp || inferredApp ? CC_SWITCH_RECOMMENDED_MODELS[app] : ''
  const recommendationAvailable =
    hasAvailableModels && modelsQuery.data?.includes(recommendedModel)
  // Undefined means untouched; an explicit empty value must stay empty.
  const models: Record<string, string> = {
    model: recommendationAvailable ? recommendedModel : '',
    ...modelOverrides,
  }
  const hasInvalidModel = Object.values(models).some(
    (value) => value && !modelsQuery.data?.includes(value)
  )
  const canSubmit =
    hasAvailableModels && Boolean(models.model) && !hasInvalidModel
  const currentConfig = APP_CONFIGS[app]

  const handleAppChange = (val: string) => {
    setSelectedApp(val as CCSwitchApp)
    setModelOverrides({})
  }

  const handleSubmit = () => {
    if (!canSubmit) {
      toast.warning(t('Please select a primary model'))
      return
    }
    const key = props.tokenKey.startsWith('sk-')
      ? props.tokenKey
      : `sk-${props.tokenKey}`
    const url = buildCCSwitchURL(app, name, models, key)
    window.open(url, '_blank')
    props.onOpenChange(false)
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Import to CC Switch')}
      contentClassName='sm:max-w-md'
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button disabled={!canSubmit} onClick={handleSubmit}>
            {t('Open CC Switch')}
          </Button>
        </>
      }
    >
      <div className='space-y-4'>
        <div className='space-y-2'>
          <Label>{t('Application')}</Label>
          <RadioGroup
            value={app}
            onValueChange={handleAppChange}
            className='flex gap-4'
          >
            {(
              Object.entries(APP_CONFIGS) as [
                CCSwitchApp,
                (typeof APP_CONFIGS)[CCSwitchApp],
              ][]
            ).map(([key, cfg]) => (
              <div
                key={key}
                className='flex items-center gap-2'
                onClickCapture={() => {
                  // Re-selecting the inferred radio is still an explicit choice.
                  // Capture also covers its label and the native input click.
                  if (key === app) setSelectedApp(key)
                }}
              >
                <RadioGroupItem value={key} id={`app-${key}`} />
                <Label htmlFor={`app-${key}`} className='cursor-pointer'>
                  {cfg.label}
                </Label>
              </div>
            ))}
          </RadioGroup>
        </div>

        <div className='space-y-2'>
          <Label htmlFor='cc-switch-name'>{t('Name')}</Label>
          <Input
            id='cc-switch-name'
            value={name}
            onChange={(event) => setCustomName(event.target.value)}
            placeholder={systemName}
          />
        </div>

        {modelsQuery.isFetching && (
          <p className='text-muted-foreground text-sm' aria-live='polite'>
            {t('Loading...')}
          </p>
        )}
        {modelsQuery.isError && (
          <p className='text-destructive text-sm' role='alert'>
            {modelsQuery.error instanceof Error
              ? modelsQuery.error.message
              : t('Failed to fetch models')}
          </p>
        )}
        {!modelsQuery.isFetching &&
          !modelsQuery.isError &&
          modelsQuery.data?.length === 0 && (
            <p className='text-muted-foreground text-sm'>
              {t('No models found')}
            </p>
          )}

        {hasAvailableModels && !models.model && (
          <p className='text-muted-foreground text-sm' aria-live='polite'>
            {recommendedModel && !recommendationAvailable
              ? t(
                  'The recommended model {{model}} is unavailable for this API key. Please select another model.',
                  { model: recommendedModel }
                )
              : t('Please select a primary model')}
          </p>
        )}
        {hasAvailableModels && hasInvalidModel && (
          <p className='text-destructive text-sm' role='alert'>
            {t('A selected model no longer exists. Reload the model list.')}
          </p>
        )}

        {currentConfig.modelFields.map((field) => (
          <div key={field.key} className='space-y-2'>
            <Label htmlFor={`cc-switch-${field.key}`} required={field.required}>
              {t(field.labelKey)}
            </Label>
            <Combobox
              id={`cc-switch-${field.key}`}
              aria-label={t(field.labelKey)}
              options={modelOptions}
              value={models[field.key] ?? ''}
              onValueChange={(value) => {
                setSelectedApp(app)
                setModelOverrides({
                  ...models,
                  [field.key]: value ?? '',
                })
              }}
              placeholder={t('Select or enter model name')}
              emptyText={t('No models found')}
            />
          </div>
        ))}
      </div>
    </Dialog>
  )
}
