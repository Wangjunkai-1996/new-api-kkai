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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery } from '@tanstack/react-query'
import { AlertTriangle, Save } from 'lucide-react'
import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { sideDrawerContentClassName } from '@/components/drawer-layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { FieldGroup } from '@/components/ui/field'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  previewModelPricing,
  previewModelPricingConversion,
  type ModelPricingPluginVariant,
  type ModelPricingConversion,
  type ModelPricingEntry,
} from '@/features/model-pricing/api'
import {
  getSitePricingCurrency,
  isValidPricingCurrency,
  USD_PRICING_CURRENCY,
} from '@/features/model-pricing/currency'
import { ImagePolicyPricing } from '@/features/model-pricing/image-policy-pricing'
import { pricingFromDraft, pricingRow } from '@/features/model-pricing/pricing'
import {
  PricingConversionDialog,
  type PricingConversionPreview,
} from '@/features/model-pricing/pricing-conversion-dialog'
import { PricingCurrencySelector } from '@/features/model-pricing/pricing-currency-selector'
import { usePricingData } from '@/features/pricing/hooks/use-pricing-data'
import { combineBillingExpr } from '@/features/pricing/lib/billing-expr'
import { pluginExpressionsEqual } from '@/features/pricing/lib/plugin-pricing'
import {
  createDefaultTaskVisualConfig,
  generateTaskExprFromConfig,
} from '@/features/pricing/lib/task-expr'
import type { BillingUsageSchema } from '@/features/pricing/types'
import { useDebounce } from '@/hooks/use-debounce'
import { handleServerError } from '@/lib/handle-server-error'
import { cn } from '@/lib/utils'
import { usePricingPreferencesStore } from '@/stores/pricing-preferences-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

import {
  buildPreviewRows,
  createInitialLaneState,
  createModelPricingSchema,
  hasValue,
  laneConfigs,
  toNumberOrNull,
  type ModelPricingFormValues,
  type ModelRatioData,
  type PricingMode,
} from './model-pricing-core'
import { formatPricingNumber } from './pricing-format'
import { TaskPluginPricingEditor } from './task-plugin-pricing-editor'
import { TaskUsagePricingEditor } from './task-usage-pricing-editor'
import { TieredPricingEditor } from './tiered-pricing-editor'

export type { ModelRatioData } from './model-pricing-core'

type ModelPricingSheetProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  editData?: ModelRatioData | null
  onSave?: () => void | Promise<void>
  isSaving?: boolean
  usageSchema?: BillingUsageSchema
  imagePricingPolicy?: ModelPricingEntry['image_pricing_policy']
  billingUnit?: 'second' | 'request'
  pluginVariants?: ModelPricingPluginVariant[]
  onDirtyChange?: (dirty: boolean) => void
}

type ModelPricingEditorPanelProps = Omit<
  ModelPricingSheetProps,
  'open' | 'onOpenChange'
> & {
  className?: string
  embedded?: boolean
  scrollHeader?: ReactNode
}

export type ModelPricingEditorPanelHandle = {
  commitDraft: () => Promise<ModelRatioData | null>
}

const DEFAULT_TOKEN_BILLING_EXPR = 'tier("base", p * 0 + c * 0)'

export const ModelPricingSheet = forwardRef<
  ModelPricingEditorPanelHandle,
  ModelPricingSheetProps
>(function ModelPricingSheet(
  {
    open,
    onOpenChange,
    editData,
    onSave,
    isSaving,
    usageSchema,
    billingUnit,
    imagePricingPolicy,
    pluginVariants,
    onDirtyChange,
  },
  ref
) {
  const { t } = useTranslation()
  const title = editData ? t('Edit model pricing') : t('Add model pricing')
  const description = editData?.name || t('New model')

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side='right'
        className={sideDrawerContentClassName('sm:max-w-2xl')}
      >
        <SheetHeader className='sr-only'>
          <SheetTitle>{title}</SheetTitle>
          <SheetDescription>{description}</SheetDescription>
        </SheetHeader>
        <ModelPricingEditorPanel
          ref={ref}
          editData={editData}
          usageSchema={usageSchema}
          billingUnit={billingUnit}
          imagePricingPolicy={imagePricingPolicy}
          pluginVariants={pluginVariants}
          onDirtyChange={onDirtyChange}
          onSave={onSave}
          isSaving={isSaving}
          className='h-full rounded-none border-0'
        />
      </SheetContent>
    </Sheet>
  )
})

export const ModelPricingEditorPanel = forwardRef<
  ModelPricingEditorPanelHandle,
  ModelPricingEditorPanelProps
>(function ModelPricingEditorPanel(
  {
    editData,
    className,
    onSave,
    isSaving,
    usageSchema,
    billingUnit,
    imagePricingPolicy,
    pluginVariants,
    onDirtyChange,
    embedded = false,
    scrollHeader,
  },
  ref
) {
  const { t } = useTranslation()
  const formElementRef = useRef<HTMLFormElement>(null)
  const currencyConfig = useSystemConfigStore((state) => state.config.currency)
  const preference = usePricingPreferencesStore((state) => state.currency)
  const siteCurrency = useMemo(
    () => getSitePricingCurrency(currencyConfig),
    [currencyConfig]
  )
  const currency =
    preference === 'site' && isValidPricingCurrency(siteCurrency)
      ? siteCurrency
      : USD_PRICING_CURRENCY
  const [pricingMode, setPricingMode] = useState<PricingMode>('tiered_expr')
  const {
    promptPrice,
    prices: lanePrices,
    enabled: laneEnabled,
  } = createInitialLaneState(editData)
  const [billingExpr, setBillingExpr] = useState(DEFAULT_TOKEN_BILLING_EXPR)
  const [expressionEdited, setExpressionEdited] = useState(false)
  const [displayBillingExpr, setDisplayBillingExpr] = useState('')
  const [conversionReason, setConversionReason] = useState('')
  const [manualConversion, setManualConversion] = useState(false)
  const [wasConverted, setWasConverted] = useState(false)
  const conversionGeneration = useRef(0)
  const [conversionPreview, setConversionPreview] =
    useState<PricingConversionPreview | null>(null)
  const conversion = useMutation({
    mutationFn: previewModelPricingConversion,
    meta: { errorToast: false },
  })
  const [requestRuleExpr, setRequestRuleExpr] = useState('')
  const [pluginExpressions, setPluginExpressions] = useState<
    Record<string, string>
  >({})
  const [editorReloadToken, setEditorReloadToken] = useState(0)
  const autoSwitchedForRef = useRef<string | null>(null)
  const isEditMode = !!editData
  const hasLegacyPricing =
    editData &&
    [
      editData.price,
      editData.ratio,
      editData.completionRatio,
      editData.cacheRatio,
      editData.createCacheRatio,
      editData.imageRatio,
      editData.audioRatio,
      editData.audioCompletionRatio,
    ].some(hasValue)
  let initialPricingMode: PricingMode = 'tiered_expr'
  if (editData?.billingMode !== 'tiered_expr' && hasLegacyPricing) {
    initialPricingMode = hasValue(editData?.price) ? 'per-request' : 'per-token'
  }
  const initialBillingExpr =
    editData?.billingExpr ||
    (initialPricingMode === 'tiered_expr' ? DEFAULT_TOKEN_BILLING_EXPR : '')
  const { models: pricingModels } = usePricingData()

  const form = useForm<ModelPricingFormValues>({
    resolver: zodResolver(createModelPricingSchema(t)),
    defaultValues: {
      name: '',
      price: '',
      ratio: '',
      cacheRatio: '',
      createCacheRatio: '',
      completionRatio: '',
      imageRatio: '',
      audioRatio: '',
      audioCompletionRatio: '',
    },
  })
  const watchedValues = useWatch({
    control: form.control,
    compute: (values) => values,
  })
  let previewRequest = ''
  if (pricingMode === 'per-token' && watchedValues.name.trim()) {
    try {
      previewRequest = JSON.stringify({
        model_name: watchedValues.name.trim(),
        pricing: pricingFromDraft({
          ...watchedValues,
          billingMode: 'per-token',
        }),
      })
    } catch {
      // Incomplete numeric input is validated by the editor before saving.
    }
  }
  const debouncedPreviewRequest = useDebounce(previewRequest, 250)
  const pricePreview = useQuery({
    queryKey: ['model-pricing-preview', debouncedPreviewRequest],
    queryFn: () => previewModelPricing(JSON.parse(debouncedPreviewRequest)),
    enabled:
      Boolean(debouncedPreviewRequest) &&
      debouncedPreviewRequest === previewRequest,
    retry: false,
    refetchOnWindowFocus: false,
    meta: { errorToast: false },
  })
  const effectivePreview =
    previewRequest === debouncedPreviewRequest ? pricePreview.data : undefined
  const usageSchemaByModel = useMemo(
    () =>
      new Map(
        pricingModels.map((model) => [
          model.model_name,
          model.billing_usage_schema,
        ])
      ),
    [pricingModels]
  )
  const usageExamplesByModel = useMemo(
    () =>
      new Map(
        pricingModels.map((model) => [
          model.model_name,
          model.billing_usage_examples,
        ])
      ),
    [pricingModels]
  )
  const taskUsageSchema =
    usageSchema ??
    pluginVariants?.find((variant) => !variant.stale)?.usage_schema ??
    usageSchemaByModel.get(watchedValues.name.trim())
  const taskUsageExamples =
    usageExamplesByModel.get(watchedValues.name.trim()) ??
    pluginVariants?.find((variant) => !variant.stale)?.usage_examples
  let taskSecondsField: 'seconds' | 'duration' | null = null
  if (
    taskUsageSchema?.seconds?.type === 'number' &&
    taskUsageSchema.seconds.unit === 'second'
  ) {
    taskSecondsField = 'seconds'
  } else if (
    taskUsageSchema?.duration?.type === 'number' &&
    taskUsageSchema.duration.unit === 'second'
  ) {
    taskSecondsField = 'duration'
  }
  // Legacy reference-video pricing carries a provider-specific modifier
  // (video_input_ratio) outside the usage facts. A one-line expression cannot
  // preserve that modifier, so leave those models for manual expression setup.
  const hasExternalVideoModifier = Boolean(
    taskUsageSchema?.video_input || taskUsageSchema?.video_input_ratio
  )
  const canConvertPerRequestToSeconds =
    pricingMode === 'per-request' &&
    billingUnit === 'second' &&
    taskSecondsField !== null &&
    !hasExternalVideoModifier &&
    hasValue(watchedValues.price)
  const defaultTaskBillingExpr = useMemo(
    () =>
      taskUsageSchema
        ? generateTaskExprFromConfig(
            createDefaultTaskVisualConfig(taskUsageSchema),
            taskUsageSchema
          )
        : '',
    [taskUsageSchema]
  )
  const resolvedBillingExpr =
    taskUsageSchema &&
    (!billingExpr || billingExpr === DEFAULT_TOKEN_BILLING_EXPR)
      ? defaultTaskBillingExpr
      : billingExpr

  useEffect(() => {
    conversionGeneration.current += 1
    // Reset the pricing draft together with React Hook Form's external form state.
    // eslint-disable-next-line react/set-state-in-effect
    setConversionReason('')
    setManualConversion(false)
    setExpressionEdited(false)
    setPluginExpressions(editData?.pluginBillingExpr ?? {})
    setDisplayBillingExpr(editData?.displayBillingExpr ?? '')
    setWasConverted(false)
    setConversionPreview(null)

    if (editData) {
      form.reset({
        name: editData.name,
        price: editData.price || '',
        ratio: editData.ratio || '',
        cacheRatio: editData.cacheRatio || '',
        createCacheRatio: editData.createCacheRatio || '',
        completionRatio: editData.completionRatio || '',
        imageRatio: editData.imageRatio || '',
        audioRatio: editData.audioRatio || '',
        audioCompletionRatio: editData.audioCompletionRatio || '',
      })
      setPricingMode(initialPricingMode)
      setBillingExpr(initialBillingExpr)
      setRequestRuleExpr(editData.requestRuleExpr || '')
    } else {
      form.reset({
        name: '',
        price: '',
        ratio: '',
        cacheRatio: '',
        createCacheRatio: '',
        completionRatio: '',
        imageRatio: '',
        audioRatio: '',
        audioCompletionRatio: '',
      })
      setPricingMode('tiered_expr')
      setBillingExpr(DEFAULT_TOKEN_BILLING_EXPR)
      setRequestRuleExpr('')
    }

    setEditorReloadToken((token) => token + 1)
    autoSwitchedForRef.current = null
  }, [editData, form, initialPricingMode, initialBillingExpr])

  useEffect(() => {
    if (!editData) return
    if (editData.billingMode === 'tiered_expr') return
    if (editData.price || editData.ratio) return

    const schema = taskUsageSchema
    if (!schema || Object.keys(schema).length === 0) return
    if (autoSwitchedForRef.current === editData.name) return

    setPricingMode('tiered_expr')
    autoSwitchedForRef.current = editData.name
  }, [editData, taskUsageSchema])

  useEffect(() => {
    onDirtyChange?.(
      form.formState.isDirty ||
        pricingMode !== initialPricingMode ||
        billingExpr !== initialBillingExpr ||
        displayBillingExpr !== (editData?.displayBillingExpr ?? '') ||
        requestRuleExpr !== (editData?.requestRuleExpr ?? '') ||
        !pluginExpressionsEqual(pluginExpressions, editData?.pluginBillingExpr)
    )
  }, [
    onDirtyChange,
    form.formState.isDirty,
    pricingMode,
    billingExpr,
    displayBillingExpr,
    requestRuleExpr,
    editData,
    initialPricingMode,
    initialBillingExpr,
    pluginExpressions,
  ])

  const previewRows = useMemo(() => {
    let previewLanes = {
      promptPrice,
      prices: lanePrices,
      enabled: laneEnabled,
    }
    if (pricingMode === 'per-token' && effectivePreview) {
      previewLanes = createInitialLaneState(
        pricingRow(watchedValues.name, effectivePreview.effective)
      )
    }
    return buildPreviewRows(
      watchedValues,
      pricingMode,
      resolvedBillingExpr,
      requestRuleExpr,
      previewLanes.promptPrice,
      previewLanes.prices,
      previewLanes.enabled,
      t,
      currency,
      effectivePreview?.cacheWriteMode,
      effectivePreview?.billingDetails
    )
  }, [
    resolvedBillingExpr,
    laneEnabled,
    lanePrices,
    pricingMode,
    promptPrice,
    requestRuleExpr,
    t,
    watchedValues,
    currency,
    effectivePreview,
  ])

  const warnings = useMemo(() => {
    const nextWarnings: string[] = []
    const hasConflict =
      !!editData?.price &&
      [
        editData.ratio,
        editData.completionRatio,
        editData.cacheRatio,
        editData.createCacheRatio,
        editData.imageRatio,
        editData.audioRatio,
        editData.audioCompletionRatio,
      ].some(hasValue)

    if (hasConflict) {
      nextWarnings.push(
        t(
          'This model has both fixed-price and token-price settings. Saving the current mode will rewrite the conflicting fields.'
        )
      )
    }

    if (
      pricingMode === 'per-token' &&
      toNumberOrNull(promptPrice) === null &&
      laneConfigs.some(
        ({ key }) => laneEnabled[key] && hasValue(lanePrices[key])
      )
    ) {
      nextWarnings.push(
        t('Input price is required before saving dependent prices.')
      )
    }

    if (
      pricingMode === 'per-token' &&
      laneEnabled.audioOutput &&
      !hasValue(lanePrices.audioInput)
    ) {
      nextWarnings.push(t('Audio output price requires an audio input price.'))
    }

    return nextWarnings
  }, [editData, laneEnabled, lanePrices, pricingMode, promptPrice, t])

  const buildSubmitData = useCallback(
    (values: ModelPricingFormValues) => {
      const data: ModelRatioData = {
        name: values.name.trim(),
        ...(editData?.pluginBillingExpr ||
        pluginVariants?.length ||
        Object.keys(pluginExpressions).length
          ? { pluginBillingExpr: pluginExpressions }
          : {}),
        billingMode: pricingMode,
        price: values.price || '',
        ratio: values.ratio || '',
        cacheRatio: values.cacheRatio || '',
        createCacheRatio: values.createCacheRatio || '',
        completionRatio: values.completionRatio || '',
        imageRatio: values.imageRatio || '',
        audioRatio: values.audioRatio || '',
        audioCompletionRatio: values.audioCompletionRatio || '',
      }

      if (pricingMode === 'tiered_expr') {
        data.billingExpr = resolvedBillingExpr
        data.displayBillingExpr = displayBillingExpr
        data.requestRuleExpr = requestRuleExpr
      }

      return data
    },
    [
      pricingMode,
      requestRuleExpr,
      resolvedBillingExpr,
      displayBillingExpr,
      pluginExpressions,
      editData,
      pluginVariants,
    ]
  )

  const convertPricing = async () => {
    if (
      conversion.isPending ||
      conversionPreview ||
      pricingMode === 'tiered_expr'
    ) {
      return
    }
    if (!(await form.trigger())) return
    const draft = buildSubmitData(form.getValues())
    const generation = ++conversionGeneration.current
    const manualExpression = manualConversion ? billingExpr.trim() : ''
    setConversionReason('')
    try {
      const pricing = pricingFromDraft(draft)
      let result: ModelPricingConversion
      if (manualExpression || canConvertPerRequestToSeconds) {
        const expression = manualExpression
          ? combineBillingExpr(resolvedBillingExpr, requestRuleExpr)
          : `tier("base", u("${taskSecondsField}") * ${formatPricingNumber(watchedValues.price ?? '')})`
        const [before] = await Promise.all([
          previewModelPricing({ model_name: draft.name, pricing }),
          previewModelPricing({
            model_name: draft.name,
            pricing: {
              'billing_setting.billing_mode': 'tiered_expr',
              'billing_setting.billing_expr': expression,
            },
          }),
        ])
        result = {
          expression,
          effective: before.effective,
          cache_write_mode: before.cacheWriteMode,
          billing_details: before.billingDetails,
        }
      } else {
        result = await conversion.mutateAsync({
          model_name: draft.name,
          pricing,
        })
      }
      if (generation !== conversionGeneration.current) return
      if (
        JSON.stringify(buildSubmitData(form.getValues())) !==
        JSON.stringify(draft)
      ) {
        setConversionReason(
          'Prices changed while preparing the conversion. Try again.'
        )
        return
      }
      if (result.unsupported_reason) {
        setConversionReason(result.unsupported_reason)
        setManualConversion(true)
        return
      }
      if (!result.expression || !result.effective) {
        throw new Error(t('Failed to prepare pricing conversion'))
      }
      setConversionPreview({
        modelName: draft.name,
        effective: result.effective,
        expression: result.expression,
        draftFingerprint: JSON.stringify(draft),
        cacheWriteMode: result.cache_write_mode,
        billingDetails: result.billing_details,
        billingUnit,
        usageSchema: taskUsageSchema,
      })
    } catch (error) {
      handleServerError(error, t('Failed to prepare pricing conversion'))
    }
  }

  const applyConversion = () => {
    if (!conversionPreview) return
    setConversionPreview(null)
    if (
      pricingMode === 'tiered_expr' ||
      JSON.stringify(buildSubmitData(form.getValues())) !==
        conversionPreview.draftFingerprint
    ) {
      setConversionReason(
        'Prices changed while preparing the conversion. Try again.'
      )
      return
    }
    setBillingExpr(conversionPreview.expression)
    setRequestRuleExpr('')
    setWasConverted(true)
    setManualConversion(false)
    setPricingMode('tiered_expr')
    setEditorReloadToken((token) => token + 1)
  }

  useImperativeHandle(
    ref,
    () => ({
      commitDraft: async () => {
        if (pricingMode !== 'tiered_expr' || imagePricingPolicy) return null
        if (
          !hasLegacyPricing &&
          !editData?.billingExpr &&
          !expressionEdited &&
          pluginExpressionsEqual(pluginExpressions, editData?.pluginBillingExpr)
        ) {
          return null
        }
        if (
          formElementRef.current?.querySelector('[data-billing-invalid="true"]')
        ) {
          return null
        }
        const amounts =
          formElementRef.current?.querySelectorAll<HTMLInputElement>(
            'input[data-pricing-amount]'
          )
        if (amounts && [...amounts].some((input) => !input.reportValidity())) {
          return null
        }
        const isValid = await form.trigger()
        if (!isValid) return null
        return buildSubmitData(form.getValues())
      },
    }),
    [
      form,
      pricingMode,
      buildSubmitData,
      imagePricingPolicy,
      hasLegacyPricing,
      editData,
      expressionEdited,
      pluginExpressions,
    ]
  )

  const expressionEditor = taskUsageSchema ? (
    <TaskUsagePricingEditor
      currency={currency}
      key={`${editorReloadToken}:${watchedValues.name}`}
      billingExpr={resolvedBillingExpr}
      requestRuleExpr={requestRuleExpr}
      usageSchema={taskUsageSchema}
      usageExamples={taskUsageExamples}
      onBillingExprChange={(expression) => {
        conversionGeneration.current += 1
        setBillingExpr(expression)
        setExpressionEdited(true)
      }}
      onRequestRuleExprChange={(expression) => {
        conversionGeneration.current += 1
        setRequestRuleExpr(expression)
      }}
    />
  ) : (
    <TieredPricingEditor
      currency={currency}
      key={editorReloadToken}
      modelName={watchedValues.name}
      billingExpr={billingExpr}
      displayBillingExpr={displayBillingExpr}
      onDisplayBillingExprChange={setDisplayBillingExpr}
      requestRuleExpr={requestRuleExpr}
      onBillingExprChange={(expression) => {
        conversionGeneration.current += 1
        setBillingExpr(expression)
        setExpressionEdited(true)
      }}
      onRequestRuleExprChange={(expression) => {
        conversionGeneration.current += 1
        setRequestRuleExpr(expression)
      }}
    />
  )

  let conversionButtonLabel = t('Convert to expression')
  if (conversion.isPending) {
    conversionButtonLabel = t('Preparing conversion...')
  } else if (manualConversion && billingExpr.trim()) {
    conversionButtonLabel = t('Preview pricing conversion')
  }

  const showActions = Boolean(onSave)
  if (imagePricingPolicy) {
    return <ImagePolicyPricing model={imagePricingPolicy} currency={currency} />
  }

  return (
    <div
      className={cn(
        'bg-background flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-xl border',
        className
      )}
    >
      {!embedded && (
        <div className='border-b p-4'>
          <div className='flex flex-wrap items-start justify-between gap-3'>
            <div className='min-w-0'>
              <h3 className='truncate text-base font-medium'>
                {isEditMode ? t('Edit model pricing') : t('Add model pricing')}
              </h3>
            </div>
          </div>
        </div>
      )}

      <Form {...form}>
        <form
          ref={formElementRef}
          onSubmit={(event) => event.preventDefault()}
          className='flex min-h-0 flex-1 flex-col'
          autoComplete='off'
        >
          <div
            role='region'
            aria-label={
              isEditMode ? t('Edit model pricing') : t('Add model pricing')
            }
            className='@container/pricing-editor min-h-0 flex-1 overflow-y-auto overscroll-contain p-4 pb-6'
          >
            {scrollHeader && (
              <div className='mb-4 space-y-3'>{scrollHeader}</div>
            )}
            <div className='grid min-w-0 items-start gap-4 @min-[960px]/pricing-editor:grid-cols-[minmax(0,1fr)_260px]'>
              <FieldGroup className='min-w-0'>
                {warnings.length > 0 && (
                  <Alert variant='destructive'>
                    <AlertTriangle data-icon='inline-start' />
                    <AlertDescription>
                      <div className='flex flex-col gap-1'>
                        {warnings.map((warning) => (
                          <span key={warning}>{warning}</span>
                        ))}
                      </div>
                    </AlertDescription>
                  </Alert>
                )}

                {!embedded && (
                  <FormField
                    control={form.control}
                    name='name'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Model name')}</FormLabel>
                        <FormControl>
                          <Input
                            placeholder={t('gpt-4')}
                            {...field}
                            disabled={isEditMode}
                          />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'The exact model identifier as used in API requests.'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                )}

                <PricingCurrencySelector siteCurrency={siteCurrency} />

                <TaskPluginPricingEditor
                  key={`${editorReloadToken}:${watchedValues.name}`}
                  variants={pluginVariants ?? []}
                  expressions={pluginExpressions}
                  onChange={setPluginExpressions}
                  modelExpression={combineBillingExpr(
                    resolvedBillingExpr,
                    requestRuleExpr
                  )}
                  modelBillingMode={
                    pricingMode === 'tiered_expr' ? 'tiered_expr' : 'ratio'
                  }
                  currency={currency}
                >
                  {pricingMode === 'tiered_expr' ? (
                    <FieldGroup className='gap-5'>
                      {expressionEditor}
                    </FieldGroup>
                  ) : (
                    <div className='space-y-4'>
                      <Alert className='border-amber-500/40 bg-amber-500/10 p-4 text-amber-900 dark:text-amber-100'>
                        <AlertTriangle aria-hidden='true' className='size-5' />
                        <AlertDescription className='space-y-3 text-sm text-inherit'>
                          <p className='font-medium'>
                            {t(
                              'Legacy prices are read-only. Preview their conversion to an expression before editing or saving.'
                            )}
                          </p>
                          <Button
                            type='button'
                            disabled={conversion.isPending}
                            onClick={() => void convertPricing()}
                          >
                            {conversionButtonLabel}
                          </Button>
                          {conversionReason && (
                            <p role='status'>{t(conversionReason)}</p>
                          )}
                        </AlertDescription>
                      </Alert>
                      {manualConversion && expressionEditor}
                    </div>
                  )}
                  {wasConverted && (
                    <p className='text-muted-foreground text-xs'>
                      {t(
                        'After conversion, expression reservation and rounding rules apply. Effective unit prices are preserved; individual rounded charges may differ.'
                      )}
                    </p>
                  )}
                </TaskPluginPricingEditor>
              </FieldGroup>

              <aside
                aria-label={t('Preview')}
                className='bg-muted/20 min-w-0 rounded-lg border @min-[960px]/pricing-editor:sticky @min-[960px]/pricing-editor:top-0'
              >
                <div className='border-b px-3 py-2'>
                  <div className='text-sm font-medium'>{t('Preview')}</div>
                </div>
                <div className='divide-y'>
                  {pricingMode === 'per-token' && !effectivePreview && (
                    <p
                      className='text-muted-foreground px-3 py-2 text-xs'
                      role='status'
                    >
                      {pricePreview.isError
                        ? t('Failed to load model pricing')
                        : t('Loading...')}
                    </p>
                  )}
                  {(pricingMode !== 'per-token' || effectivePreview) &&
                    previewRows.map((row) => (
                      <div key={row.key} className='grid gap-1 px-3 py-2.5'>
                        <span className='text-muted-foreground text-xs'>
                          {row.label}
                        </span>
                        <span
                          className={cn(
                            'min-w-0 text-sm',
                            row.multiline
                              ? 'font-mono text-xs leading-5 break-words whitespace-pre-wrap'
                              : 'truncate'
                          )}
                        >
                          {row.value}
                        </span>
                      </div>
                    ))}
                </div>
              </aside>
            </div>
          </div>
          {showActions && (
            <div className='bg-background/95 supports-[backdrop-filter]:bg-background/80 shrink-0 border-t p-3 backdrop-blur'>
              <div className='flex flex-col-reverse gap-2 sm:flex-row sm:justify-end'>
                {onSave && (
                  <Button
                    type='button'
                    onClick={onSave}
                    disabled={isSaving || pricingMode !== 'tiered_expr'}
                    className='w-full sm:w-auto'
                  >
                    <Save data-icon='inline-start' />
                    {isSaving ? t('Saving...') : t('Save model prices')}
                  </Button>
                )}
              </div>
            </div>
          )}
        </form>
      </Form>
      {conversionPreview && (
        <PricingConversionDialog
          preview={conversionPreview}
          currency={currency}
          onCancel={() => {
            conversionGeneration.current += 1
            setConversionPreview(null)
            setConversionReason('')
          }}
          onConfirm={applyConversion}
        />
      )}
    </div>
  )
})
