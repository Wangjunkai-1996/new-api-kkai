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
import { useState, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { ComboboxInput } from '@/components/ui/combobox-input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { privateUserQueryKey } from '@/lib/private-query-cache'
import { useAuthStore } from '@/stores/auth-store'

import { getTokenModels } from '../../api'

const APP_CONFIGS = {
  claude: {
    label: 'Claude',
    defaultName: 'KKAI',
    modelFields: [
      { key: 'model', labelKey: 'Primary Model', required: true },
      { key: 'haikuModel', labelKey: 'Haiku Model', required: false },
      { key: 'sonnetModel', labelKey: 'Sonnet Model', required: false },
      { key: 'opusModel', labelKey: 'Opus Model', required: false },
    ],
  },
  codex: {
    label: 'Codex',
    defaultName: 'KKAI',
    modelFields: [{ key: 'model', labelKey: 'Primary Model', required: true }],
  },
  gemini: {
    label: 'Gemini',
    defaultName: 'KKAI',
    modelFields: [{ key: 'model', labelKey: 'Primary Model', required: true }],
  },
} as const

type AppType = keyof typeof APP_CONFIGS

function getServerAddress(): string {
  try {
    const raw = localStorage.getItem('status')
    if (raw) {
      const status = JSON.parse(raw)
      if (status.server_address) return status.server_address
    }
  } catch {
    /* empty */
  }
  return window.location.origin
}

function buildCCSwitchURL(
  app: string,
  name: string,
  models: Record<string, string>,
  apiKey: string
): string {
  const serverAddress = getServerAddress()
  const endpoint = app === 'codex' ? `${serverAddress}/v1` : serverAddress
  const params = new URLSearchParams()
  params.set('resource', 'provider')
  params.set('app', app)
  params.set('name', name)
  params.set('endpoint', endpoint)
  params.set('apiKey', apiKey)
  for (const [k, v] of Object.entries(models)) {
    if (v) params.set(k, v)
  }
  params.set('homepage', serverAddress)
  params.set('enabled', 'true')
  return `ccswitch://v1/import?${params.toString()}`
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  tokenKey: string
  tokenId: number
}

export function CCSwitchDialog(props: Props) {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const [app, setApp] = useState<AppType>('claude')
  const [name, setName] = useState<string>(APP_CONFIGS.claude.defaultName)
  const [models, setModels] = useState<Record<string, string>>({})

  const modelsQuery = useQuery({
    queryKey: privateUserQueryKey(
      userId,
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
    enabled: props.open && props.tokenId > 0 && userId > 0,
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

  useEffect(() => {
    if (props.open) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setModels({})

      setApp('claude')

      setName(APP_CONFIGS.claude.defaultName)
    }
  }, [props.open, props.tokenId])

  const currentConfig = APP_CONFIGS[app]

  const handleAppChange = (val: string) => {
    const appVal = val as AppType
    setApp(appVal)
    setName(APP_CONFIGS[appVal].defaultName)
    setModels({})
  }

  const handleSubmit = () => {
    if (!models.model) {
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
      bodyClassName={
        currentConfig.modelFields.length === 1 ? 'space-y-4 pb-52' : 'space-y-4'
      }
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button disabled={!hasAvailableModels} onClick={handleSubmit}>
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
                AppType,
                (typeof APP_CONFIGS)[AppType],
              ][]
            ).map(([key, cfg]) => (
              <div key={key} className='flex items-center gap-2'>
                <RadioGroupItem value={key} id={`app-${key}`} />
                <Label htmlFor={`app-${key}`} className='cursor-pointer'>
                  {cfg.label}
                </Label>
              </div>
            ))}
          </RadioGroup>
        </div>

        <div className='space-y-2'>
          <Label>{t('Name')}</Label>
          <ComboboxInput
            options={[]}
            value={name}
            onValueChange={setName}
            placeholder={currentConfig.defaultName}
            emptyText=''
            allowCustomValue
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

        {currentConfig.modelFields.map((field) => (
          <div key={field.key} className='space-y-2'>
            <Label>
              {t(field.labelKey)}
              {field.required && (
                <span className='text-destructive ml-0.5'>*</span>
              )}
            </Label>
            <ComboboxInput
              options={modelOptions}
              value={models[field.key] || ''}
              onValueChange={(v) =>
                setModels((prev) => ({ ...prev, [field.key]: v }))
              }
              placeholder={t('Select or enter model name')}
              emptyText={t('No models found')}
            />
          </div>
        ))}
      </div>
    </Dialog>
  )
}
