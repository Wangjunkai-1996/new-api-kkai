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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Combobox } from '@/components/ui/combobox'
import { Label } from '@/components/ui/label'
import { useSiteConfig } from '@/features/site-config'
import { useConsoleContract } from '@/hooks/use-console-contract'
import { useMediaQuery } from '@/hooks/use-media-query'
import { getUserGroups } from '@/lib/api'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { handleServerError } from '@/lib/handle-server-error'
import { getCurrentHost } from '@/lib/host-scope'
import { useAuthStore } from '@/stores/auth-store'

import {
  createApiKey,
  fetchTokenKey,
  getApiKey,
  getTokenModels,
} from '../../keys/api'
import {
  CC_SWITCH_RECOMMENDED_MODELS,
  type CCSwitchApp,
} from '../../keys/lib/cc-switch-defaults'
import {
  buildCCSwitchURL,
  getCCSwitchEndpoint,
} from '../../keys/lib/cc-switch-url'

export type QuickSetupPreset = 'gpt' | 'claude-code'

export interface QuickSetupDialogProps {
  preset: QuickSetupPreset | null
  onClose: () => void
}

type FlowStage =
  | 'prepare'
  | 'manual-before-create'
  | 'creating'
  | 'loading-models'
  | 'select-model'
  | 'created-no-model'
  | 'created-model-error'
  | 'created-key-error'
  | 'created-unknown'
  | 'ready-to-launch'
  | 'launch-requested'

type Flow = {
  stage: FlowStage
  tokenId?: number
  models: string[]
  selectedModel: string
  modelOverrides: Record<string, string>
  readyConfirmed: boolean
  manual: boolean
  message?: string
}

function renderActionButton(args: {
  flow: Flow
  canCreate: boolean
  canOpen: boolean
  isCreating: boolean
  onCreate: (manual: boolean) => void
  onOpen: (autoOpen: boolean) => void
  onClose: () => void
  t: (key: string) => string
}) {
  const { flow, canCreate, canOpen, isCreating, onCreate, onOpen, onClose, t } =
    args
  if (flow.stage === 'launch-requested') {
    return (
      <Button onClick={() => void onOpen(true)}>
        {t('Open CC Switch again')}
      </Button>
    )
  }
  if (flow.manual && flow.tokenId) {
    return (
      <Button variant='outline' render={<Link to='/keys' onClick={onClose} />}>
        {t('Open API Keys')}
      </Button>
    )
  }
  if (flow.stage === 'ready-to-launch' || flow.stage === 'select-model') {
    return (
      <Button
        disabled={!canOpen || isCreating}
        onClick={() => void onOpen(true)}
      >
        {t('Open CC Switch')}
      </Button>
    )
  }
  if (flow.stage === 'manual-before-create') {
    return (
      <Button
        disabled={isCreating || !canCreate}
        onClick={() => void onCreate(true)}
      >
        {t('Create API key')}
      </Button>
    )
  }
  if (flow.stage === 'created-unknown') {
    return (
      <Button variant='outline' render={<Link to='/keys' onClick={onClose} />}>
        {t('Open API Keys')}
      </Button>
    )
  }
  if (flow.tokenId) {
    return (
      <Button variant='outline' render={<Link to='/keys' onClick={onClose} />}>
        {t('Open API Keys')}
      </Button>
    )
  }
  return (
    <Button
      disabled={isCreating || !canCreate || !flow.readyConfirmed}
      onClick={() => void onCreate(false)}
    >
      {t('Create key and open CC Switch')}
    </Button>
  )
}

const PRESETS: Record<
  QuickSetupPreset,
  { group: string; app: CCSwitchApp; tool: string }
> = {
  gpt: { group: 'GPT-稳定', app: 'codex', tool: 'Codex' },
  'claude-code': {
    group: 'claudecode-满血企业',
    app: 'claude',
    tool: 'Claude Code',
  },
}

const CLAUDE_MODEL_FIELDS = [
  { key: 'haikuModel', label: 'Haiku Model', required: false },
  { key: 'sonnetModel', label: 'Sonnet Model', required: false },
  { key: 'opusModel', label: 'Opus Model', required: false },
] as const

function initialFlow(): Flow {
  return {
    stage: 'prepare',
    models: [],
    selectedModel: '',
    modelOverrides: {},
    readyConfirmed: false,
    manual: false,
  }
}

function normalizeKey(value: string): string {
  return value.startsWith('sk-') ? value : `sk-${value}`
}

function chooseModel(
  current: string,
  models: string[],
  recommended: string
): string {
  if (models.includes(current)) return current
  if (models.includes(recommended)) return recommended
  return ''
}

function isActiveToken(
  token: {
    id: number
    group?: string | null
    status: number
    expired_time: number
  },
  tokenId: number,
  group: string
): boolean {
  return (
    token.id === tokenId &&
    token.group === group &&
    token.status === 1 &&
    (token.expired_time === -1 || token.expired_time > Date.now() / 1000)
  )
}

export function QuickSetupDialog(props: QuickSetupDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const contract = useConsoleContract()
  const siteConfig = useSiteConfig()
  const tenantId = siteConfig.data?.tenant_id ?? 0
  const host = getCurrentHost()
  const flowsRef = useRef<Record<QuickSetupPreset, Flow>>({
    gpt: initialFlow(),
    'claude-code': initialFlow(),
  })
  const [flows, setFlows] = useState<Record<QuickSetupPreset, Flow>>(() => ({
    gpt: initialFlow(),
    'claude-code': initialFlow(),
  }))
  const requestRef = useRef(0)
  const cancelledRef = useRef(false)
  const isMobile = useMediaQuery('(max-width: 640px)')
  const identityRef = useRef(`${userId}:${tenantId}:${host}`)
  const activePreset = props.preset
  const flow = activePreset ? flows[activePreset] : initialFlow()
  const config = activePreset ? PRESETS[activePreset] : PRESETS.gpt
  const groupsQuery = useQuery({
    queryKey: ['usage-guide-groups', userId, tenantId, host],
    queryFn: getUserGroups,
    enabled: Boolean(activePreset && userId > 0),
    staleTime: 60_000,
    retry: false,
  })
  const groupAvailable = Boolean(
    groupsQuery.data?.success && groupsQuery.data.data?.[config.group]
  )
  const canCreate = Boolean(
    contract.contract?.capabilities.includes('token_create_result_id') &&
    groupAvailable
  )

  useEffect(() => {
    const identity = `${userId}:${tenantId}:${host}`
    if (identityRef.current === identity) return
    identityRef.current = identity
    requestRef.current += 1
    cancelledRef.current = true
    const reset = { gpt: initialFlow(), 'claude-code': initialFlow() }
    flowsRef.current = reset
    setFlows(reset)
  }, [host, tenantId, userId])

  const updateFlow = useCallback(
    (preset: QuickSetupPreset, update: (current: Flow) => Flow) => {
      flowsRef.current[preset] = update(flowsRef.current[preset])
      setFlows({ ...flowsRef.current })
    },
    []
  )

  const close = useCallback(() => {
    cancelledRef.current = true
    requestRef.current += 1
    props.onClose()
  }, [props])

  useEffect(() => {
    if (activePreset) cancelledRef.current = false
  }, [activePreset])

  const isCurrent = (preset: QuickSetupPreset, requestId: number) =>
    !cancelledRef.current &&
    requestRef.current === requestId &&
    props.preset === preset &&
    useAuthStore.getState().auth.user?.id === userId

  const isSameIdentity = () =>
    identityRef.current === `${userId}:${tenantId}:${host}` &&
    useAuthStore.getState().auth.user?.id === userId

  const loadModels = async (
    preset: QuickSetupPreset,
    tokenId: number,
    requestId: number
  ): Promise<string[] | null> => {
    updateFlow(preset, (current) => ({
      ...current,
      stage: 'loading-models',
      message: undefined,
    }))
    const result = await getTokenModels(tokenId)
    if (!isCurrent(preset, requestId)) return null
    if (!result.success) {
      updateFlow(preset, (current) => ({
        ...current,
        stage: 'created-model-error',
        message: result.message,
      }))
      return null
    }
    const models = result.data ?? []
    const recommended = CC_SWITCH_RECOMMENDED_MODELS[PRESETS[preset].app]
    updateFlow(preset, (current) => ({
      ...current,
      models,
      selectedModel: chooseModel(current.selectedModel, models, recommended),
      modelOverrides: Object.fromEntries(
        Object.entries(current.modelOverrides).filter(([, value]) =>
          models.includes(value)
        )
      ),
      stage: models.length === 0 ? 'created-no-model' : 'ready-to-launch',
    }))
    return models
  }

  const readSecret = async (
    preset: QuickSetupPreset,
    requestId: number
  ): Promise<string | null> => {
    const current = flowsRef.current[preset]
    if (
      !current.tokenId ||
      !current.selectedModel ||
      !isCurrent(preset, requestId)
    ) {
      return null
    }
    let tokenResult
    try {
      tokenResult = await getApiKey(current.tokenId)
    } catch (error) {
      if (isCurrent(preset, requestId)) {
        updateFlow(preset, (flowState) => ({
          ...flowState,
          stage: 'created-key-error',
          message: t('The API key could not be checked. Please try again.'),
        }))
        handleServerError(
          error,
          t('The API key could not be checked. Please try again.')
        )
      }
      return null
    }
    if (!isCurrent(preset, requestId)) return null
    if (
      !tokenResult.success ||
      !tokenResult.data ||
      !isActiveToken(tokenResult.data, current.tokenId, PRESETS[preset].group)
    ) {
      updateFlow(preset, (flowState) => ({
        ...flowState,
        stage: 'created-key-error',
        message: t(
          'The API key is no longer available. Open API Keys to manage it.'
        ),
      }))
      return null
    }
    let secretResult
    try {
      secretResult = await fetchTokenKey(current.tokenId)
    } catch (error) {
      if (isCurrent(preset, requestId)) {
        updateFlow(preset, (flowState) => ({
          ...flowState,
          stage: 'created-key-error',
          message: t('The API key could not be read. Please try again.'),
        }))
        handleServerError(
          error,
          t('The API key could not be read. Please try again.')
        )
      }
      return null
    }
    if (!isCurrent(preset, requestId)) return null
    if (!secretResult.success || !secretResult.data?.key) {
      updateFlow(preset, (flowState) => ({
        ...flowState,
        stage: 'created-key-error',
        message: secretResult.message,
      }))
      return null
    }
    const secret = normalizeKey(secretResult.data.key)
    return secret
  }

  const fetchSecretAndOpen = async (
    preset: QuickSetupPreset,
    requestId: number
  ) => {
    const current = flowsRef.current[preset]
    const secret = await readSecret(preset, requestId)
    if (!secret || !isCurrent(preset, requestId)) return
    const models = { model: current.selectedModel, ...current.modelOverrides }
    const url = buildCCSwitchURL(
      PRESETS[preset].app,
      PRESETS[preset].group,
      models,
      secret
    )
    if (!isCurrent(preset, requestId)) return
    window.open(url, '_blank', 'noopener,noreferrer')
    updateFlow(preset, (flowState) => ({
      ...flowState,
      stage: 'launch-requested',
    }))
  }

  const create = async (manual: boolean) => {
    if (!activePreset || !canCreate || (!manual && !flow.readyConfirmed)) return
    const preset = activePreset
    const requestId = ++requestRef.current
    cancelledRef.current = false
    updateFlow(preset, (current) => ({
      ...current,
      stage: 'creating',
      message: undefined,
    }))
    try {
      const result = await createApiKey({
        name: PRESETS[preset].group,
        group: PRESETS[preset].group,
        expired_time: -1,
        remain_quota: 0,
        unlimited_quota: true,
        model_limits_enabled: false,
        model_limits: '',
        allow_ips: '',
        cross_group_retry: false,
      })
      if (!isSameIdentity()) return
      if (!result.success) {
        updateFlow(preset, (current) => ({
          ...current,
          stage: 'prepare',
          message: result.message,
        }))
        return
      }
      const tokenId = result.data?.id
      if (!Number.isInteger(tokenId) || tokenId <= 0) {
        updateFlow(preset, (current) => ({
          ...current,
          stage: 'created-unknown',
          message: t(
            'The key may have been created. Check API Keys before retrying.'
          ),
        }))
        return
      }
      updateFlow(preset, (current) => ({ ...current, tokenId, manual }))
      void queryClient.invalidateQueries({ queryKey: ['keys'] })
      if (!isCurrent(preset, requestId)) return
      const models = await loadModels(preset, tokenId, requestId)
      if (!models || !isCurrent(preset, requestId)) return
      const recommended = CC_SWITCH_RECOMMENDED_MODELS[PRESETS[preset].app]
      if (!manual && !isMobile && models.includes(recommended)) {
        await fetchSecretAndOpen(preset, requestId)
      }
    } catch (error) {
      if (!isCurrent(preset, requestId)) return
      updateFlow(preset, (current) => ({
        ...current,
        stage: 'created-unknown',
        message: t('The result is unknown. Check API Keys before retrying.'),
      }))
      handleServerError(
        error,
        t('The result is unknown. Check API Keys before retrying.')
      )
    }
  }

  const openExisting = async (autoOpen: boolean) => {
    if (!activePreset || !flow.tokenId) return
    const requestId = ++requestRef.current
    cancelledRef.current = false
    let models: string[] | null
    try {
      models = await loadModels(activePreset, flow.tokenId, requestId)
    } catch (error) {
      if (isCurrent(activePreset, requestId)) {
        updateFlow(activePreset, (state) => ({
          ...state,
          stage: 'created-model-error',
          message: t('Model loading failed. Please try again.'),
        }))
        handleServerError(error, t('Model loading failed. Please try again.'))
      }
      return
    }
    if (!models || !isCurrent(activePreset, requestId)) return
    if (models.length === 0) {
      updateFlow(activePreset, (state) => ({
        ...state,
        stage: 'created-no-model',
      }))
      return
    }
    const current = flowsRef.current[activePreset]
    if (!current.selectedModel || !models.includes(current.selectedModel)) {
      updateFlow(activePreset, (state) => ({ ...state, stage: 'select-model' }))
      return
    }
    if (autoOpen) await fetchSecretAndOpen(activePreset, requestId)
  }

  const setModel = (key: string, value: string | null) => {
    if (!activePreset) return
    updateFlow(activePreset, (current) => ({
      ...current,
      selectedModel: key === 'model' ? (value ?? '') : current.selectedModel,
      modelOverrides: { ...current.modelOverrides, [key]: value ?? '' },
    }))
  }

  const copyKey = async () => {
    if (!activePreset || !flow.tokenId || !isSameIdentity()) return
    const requestId = ++requestRef.current
    cancelledRef.current = false
    const secret = await readSecret(activePreset, requestId)
    if (secret && isCurrent(activePreset, requestId)) {
      const copied = await copyToClipboard(secret)
      if (copied) toast.success(t('Copied'))
      else toast.error(t('Failed to copy'))
    }
  }

  const modelOptions = flow.models.map((model) => ({
    value: model,
    label: model,
  }))
  const modelFields = [{ key: 'model', label: 'Primary Model', required: true }]
  if (config.app === 'claude') modelFields.push(...CLAUDE_MODEL_FIELDS)
  const showModelFields = flow.models.length > 0 && flow.stage !== 'prepare'
  const canOpen = Boolean(flow.tokenId && flow.selectedModel && !flow.message)
  const isCreating =
    flow.stage === 'creating' || flow.stage === 'loading-models'
  const title = activePreset
    ? `${t('Configure')} ${config.tool}`
    : t('Configure')
  let createBlockedMessage: string | null = null
  if (!canCreate) {
    if (contract.loading) {
      createBlockedMessage = t('Checking compatibility...')
    } else if (
      !contract.contract?.capabilities.includes('token_create_result_id')
    ) {
      createBlockedMessage = t(
        'This server cannot create a guided API key yet. Please retry later.'
      )
    } else if (groupsQuery.isPending) {
      createBlockedMessage = t('Loading...')
    } else if (groupsQuery.isError) {
      createBlockedMessage = t('Failed to load groups')
    } else {
      createBlockedMessage = t('No groups available')
    }
  }

  return (
    <Dialog
      open={Boolean(activePreset)}
      onOpenChange={(open) => {
        if (!open) close()
      }}
      title={title}
      description={t('Create a dedicated API key and configure your client.')}
      contentClassName='sm:max-w-[474px]'
      footer={
        activePreset ? (
          <>
            <Button variant='outline' onClick={close}>
              {t('Cancel')}
            </Button>
            {/* oxlint-disable-next-line react/refs -- callbacks are event handlers; the helper does not invoke them during render. */}
            {renderActionButton({
              flow,
              canCreate,
              canOpen,
              isCreating,
              onCreate: create,
              onOpen: openExisting,
              onClose: close,
              t,
            })}
          </>
        ) : null
      }
    >
      {activePreset ? (
        <div className='space-y-4'>
          {createBlockedMessage && (
            <Alert variant='destructive'>
              <AlertDescription>{createBlockedMessage}</AlertDescription>
            </Alert>
          )}
          <div className='bg-muted rounded-lg p-3 text-sm'>
            <div className='grid grid-cols-[auto_1fr] gap-x-3 gap-y-1'>
              <span className='text-muted-foreground'>{t('Key name')}</span>
              <span>{config.group}</span>
              <span className='text-muted-foreground'>{t('Group')}</span>
              <span>{config.group}</span>
              <span className='text-muted-foreground'>{t('Quota')}</span>
              <span>{t('No separate key quota limit')}</span>
              <span className='text-muted-foreground'>{t('Validity')}</span>
              <span>{t('Long-term, can be disabled anytime')}</span>
            </div>
          </div>
          {flow.stage === 'prepare' && (
            <>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Install and open CC Switch and {{tool}} before continuing.',
                  {
                    tool: config.tool,
                  }
                )}
              </p>
              <label className='flex items-start gap-2 text-sm'>
                <Checkbox
                  checked={flow.readyConfirmed}
                  onCheckedChange={(checked) =>
                    updateFlow(activePreset, (current) => ({
                      ...current,
                      readyConfirmed: checked === true,
                    }))
                  }
                />
                <span>{t('I have installed and opened CC Switch')}</span>
              </label>
              <Button
                variant='link'
                className='h-auto p-0'
                onClick={() =>
                  updateFlow(activePreset, (current) => ({
                    ...current,
                    stage: 'manual-before-create',
                  }))
                }
              >
                {t('I want to configure manually')}
              </Button>
            </>
          )}
          {isCreating && <p aria-live='polite'>{t('Creating API key...')}</p>}
          {flow.message && (
            <Alert variant='destructive'>
              <AlertDescription>{flow.message}</AlertDescription>
            </Alert>
          )}
          {flow.stage === 'created-unknown' && (
            <p className='text-muted-foreground text-sm'>
              {t('Do not create another key until you check API Keys.')}
            </p>
          )}
          {showModelFields && (
            <div className='space-y-3'>
              {modelFields.map((field) => (
                <div key={field.key} className='space-y-1'>
                  <Label
                    htmlFor={`quick-setup-${field.key}`}
                    required={field.required}
                  >
                    {t(field.label)}
                  </Label>
                  <Combobox
                    id={`quick-setup-${field.key}`}
                    aria-label={t(field.label)}
                    options={modelOptions}
                    value={
                      field.key === 'model'
                        ? flow.selectedModel
                        : (flow.modelOverrides[field.key] ?? '')
                    }
                    onValueChange={(value) => setModel(field.key, value)}
                    placeholder={t('Select a model')}
                    emptyText={t('No models found')}
                  />
                </div>
              ))}
            </div>
          )}
          {flow.stage === 'created-no-model' && (
            <div className='space-y-2'>
              <p className='text-muted-foreground text-sm'>
                {t('No models are available for this API key yet.')}
              </p>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => void openExisting(false)}
              >
                {t('Retry')}
              </Button>
            </div>
          )}
          {flow.stage === 'created-model-error' && (
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => void openExisting(false)}
            >
              {t('Retry')}
            </Button>
          )}
          {flow.stage === 'created-key-error' && (
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => void openExisting(true)}
            >
              {t('Retry')}
            </Button>
          )}
          {flow.stage === 'launch-requested' && (
            <p className='text-muted-foreground text-sm' aria-live='polite'>
              {t(
                'The key was created and CC Switch was opened. Confirm the import in CC Switch.'
              )}
            </p>
          )}
          {flow.tokenId && flow.stage !== 'launch-requested' && (
            <div className='space-y-2'>
              <Label>{t('API Key')}</Label>
              <div className='flex gap-2'>
                <code className='bg-muted min-w-0 flex-1 truncate rounded p-2 text-xs'>
                  {t('Copy only when needed')}
                </code>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => void copyKey()}
                >
                  {t('Copy')}
                </Button>
              </div>
            </div>
          )}
          {flow.tokenId && (
            <div className='bg-muted/40 space-y-2 rounded-lg p-3 text-xs'>
              <div className='space-y-1'>
                <span className='text-muted-foreground block'>
                  {t('API endpoint')}
                </span>
                <code className='bg-background block overflow-x-auto rounded px-2 py-1 font-mono break-all'>
                  {getCCSwitchEndpoint(config.app)}
                </code>
              </div>
              <p className='text-muted-foreground'>
                {t(
                  'Choose a model from the list above. The endpoint and model must match the client you are configuring.'
                )}
              </p>
              <p className='text-muted-foreground'>
                {t(
                  'Keep your API key private. Do not share it or include it in screenshots.'
                )}
              </p>
              <Button
                variant='link'
                size='sm'
                className='h-auto px-0'
                render={<Link to='/keys' onClick={close} />}
              >
                {t('Manage API keys')}
                <span aria-hidden='true'>↗</span>
              </Button>
            </div>
          )}
        </div>
      ) : null}
    </Dialog>
  )
}
