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
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { toast } from 'sonner'
import { beforeAll, beforeEach, describe, expect, test, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import { useAuthStore } from '@/stores/auth-store'
import { clearImageStudioSubmissionKey } from '@/stores/image-studio-draft-store'

import type { ImageTokenGateState } from '../hooks/use-image-token-gate'
import type {
  CreateImageRequest,
  ImageAsyncQuoteRequest,
  ImageComposerValues,
  ImageEditQuoteRequest,
  ImageGeneration,
  ImageGenerationInput,
  ImageModelProfile,
  ImageQuote,
  ImageQuoteRequest,
  ImageReferenceMetadata,
  ImageSample,
} from '../types'
import { ImageComposer, type ImageComposerLoadRequest } from './image-composer'

type GenerationVariables = {
  request: CreateImageRequest
  idempotencyKey: string
}

const mocks = vi.hoisted(() => ({
  models: [] as ImageModelProfile[],
  generationQuoteRequest: null as ImageQuoteRequest | null,
  asyncQuoteRequest: null as ImageAsyncQuoteRequest | null,
  editQuoteRequest: null as ImageEditQuoteRequest | null,
  quoteExpiresIn: 3600,
  refreshedQuote: null as Partial<ImageQuote> | null,
  generationQuoteRefetch: vi.fn(),
  asyncQuoteRefetch: vi.fn(),
  generationMutation: {
    isPending: false,
    variables: undefined as GenerationVariables | undefined,
    mutateAsync: vi.fn(),
  },
  asyncMutation: { isPending: false, mutateAsync: vi.fn() },
  asyncQuoteFailed: false,
  directReferences: {
    assetIds: [31],
    ready: true,
    processing: false,
    error: null,
    progress: { phase: 'uploading', progress: 0 },
    retry: vi.fn(),
  },
  pendingSubmissions: [] as Array<{
    idempotencyKey: string
    requestFingerprint: string
    expiresAt: number
    submittedAt: number
  }>,
  lookup: vi.fn(),
  editMutation: {
    isPending: false,
    mutateAsync: vi.fn(),
  },
  draftState: {
    userId: 1,
    draft: null as ImageComposerValues | null,
    hydrate: vi.fn(),
    save: vi.fn(),
    clear: vi.fn(),
  },
  reference: {
    metadata: [] as ImageReferenceMetadata[],
    files: [] as File[],
    processing: false,
    clear: vi.fn(),
    replace: vi.fn(),
  },
}))

vi.mock('../queries', () => ({
  useCreateAsyncImageGeneration: () => mocks.asyncMutation,
  useAsyncImageQuote: (request: ImageAsyncQuoteRequest | null) => {
    mocks.asyncQuoteRequest = request
    const quote =
      request && !mocks.asyncQuoteFailed
        ? {
            quota: 1,
            display_amount: '$0.01',
            quote_token: 'async-quote',
            expires_at: Math.floor(Date.now() / 1000) + 3600,
          }
        : undefined
    return {
      data: quote,
      isFetching: false,
      isError: Boolean(request) && mocks.asyncQuoteFailed,
      refetch: () => {
        mocks.asyncQuoteRefetch()
        return Promise.resolve({
          data: quote && { ...quote, ...mocks.refreshedQuote },
        })
      },
    }
  },
  useImageModels: () => ({ data: mocks.models, isLoading: false }),
  useCreateImageGeneration: () => mocks.generationMutation,
  useCreateImageEdit: () => mocks.editMutation,
  useImageQuote: (request: ImageQuoteRequest | null) => {
    mocks.generationQuoteRequest = request
    const count = Number(request?.parameters.variants ?? 1)
    const quote = request
      ? {
          quota: count,
          display_amount: `$0.0${String(count)}`,
          quote_token: `quote-${String(count)}`,
          expires_at: Math.floor(Date.now() / 1000) + mocks.quoteExpiresIn,
        }
      : undefined
    return {
      data: quote,
      isFetching: false,
      isError: false,
      refetch: () => {
        mocks.generationQuoteRefetch()
        return Promise.resolve({
          data: quote && { ...quote, ...mocks.refreshedQuote },
        })
      },
    }
  },
  useImageEditQuote: (request: ImageEditQuoteRequest | null) => {
    mocks.editQuoteRequest = request
    const quote = request
      ? {
          quota: 1,
          display_amount: '$0.01',
          quote_token: 'edit-quote-1',
          expires_at: Math.floor(Date.now() / 1000) + 3600,
        }
      : undefined
    return {
      data: quote,
      isFetching: false,
      isError: false,
      refetch: vi.fn().mockResolvedValue({ data: quote }),
    }
  },
}))

vi.mock('../api', () => ({
  lookupImageSubmission: (...args: unknown[]) => mocks.lookup(...args),
}))
vi.mock('../hooks/use-image-direct-references', () => ({
  useImageDirectReferences: () => mocks.directReferences,
}))
vi.mock('./image-submission-recovery', () => ({
  ImageSubmissionRecovery: () => null,
}))

vi.mock('../hooks/use-image-references', () => ({
  useImageReferences: () => mocks.reference,
}))

vi.mock('@/stores/image-studio-draft-store', () => ({
  clearImageStudioSubmissionKey: vi.fn(),
  listPendingImageStudioSubmissions: () => mocks.pendingSubmissions,
  markImageStudioSubmissionStarted: vi.fn(() => true),
  getOrCreateImageStudioSubmissionKey: vi.fn(() => 'submission-key'),
  useImageStudioDraftStore: (
    selector: (state: typeof mocks.draftState) => unknown
  ) => selector(mocks.draftState),
}))

vi.mock('./image-token-setup-dialog', () => ({
  ImageTokenSetupDialog: () => null,
}))

vi.mock('./image-reference-field', () => ({
  ImageReferenceField: (props: { maxReferenceImages: number }) => (
    <span aria-label='Reference limit'>{props.maxReferenceImages}</span>
  ),
}))

const profile = (): ImageModelProfile => ({
  id: 7,
  model: 'gpt-image-2',
  display_name: 'GPT Image',
  description: '',
  provider_label: 'OpenAI',
  specification_version: 2,
  specification: {
    version: 2,
    parameters: [
      {
        key: 'variants',
        label: 'Candidates',
        request_key: 'n',
        control: 'integer',
        min: 1,
        max: 4,
      },
    ],
  },
  default_parameters: { variants: 1 },
  effective_max_outputs: 4,
  enabled: true,
  sort_order: 0,
  created_at: 1,
  updated_at: 1,
})

const sizedProfile = (sizeKey = 'dimensions'): ImageModelProfile => {
  const result = profile()
  result.specification.custom_size = {
    max_edge: 3840,
    step: 16,
    min_pixels: 655360,
    max_pixels: 8294400,
    max_aspect_ratio: 3,
  }
  result.specification.parameters.push({
    key: sizeKey,
    label: 'Output size',
    request_key: 'size',
    control: 'select',
    required: true,
    options: [{ label: 'Square', value: '1024x1024' }],
  })
  result.default_parameters[sizeKey] = '1024x1024'
  return result
}

const advancedProfile = (count: number): ImageModelProfile => {
  const result = profile()
  for (let index = 0; index < count; index += 1) {
    const key = `extra_${index}`
    result.specification.parameters.push({
      key,
      label: `Extra parameter ${index + 1}`,
      request_key: key,
      control: 'integer',
      min: 0,
      max: 100,
    })
    result.default_parameters[key] = 10
  }
  return result
}

const publishedSample = (overrides: Partial<ImageSample>): ImageSample => ({
  id: 1,
  model_profile_id: 7,
  image_asset_id: 1,
  model: 'gpt-image-2',
  title: 'A sample composition',
  prompt: 'A sample composition',
  model_version: 2,
  parameters: { variants: 1 },
  category: '',
  status: 'published',
  sort_order: 0,
  asset: {
    id: 1,
    position: 0,
    state: 'ready',
    thumbnail_state: 'ready',
    mime_type: 'image/png',
    size_bytes: 9,
    width: 1024,
    height: 1024,
  },
  created_at: 1,
  updated_at: 1,
  ...overrides,
})

const tokenGate = {
  capability: {
    required_group: 'default',
    has_usable_token: true,
    can_create: true,
    effective_models: ['gpt-image-2'],
    max_reference_bytes: 10_000_000,
    max_reference_total_bytes: 10_000_000,
    status: 'ready',
    token: { id: 11, name: 'Image Studio', group: 'default' },
  },
  tokenId: 11,
  checking: false,
  checkFailed: false,
  refetch: vi.fn(),
  dialogOpen: false,
  setDialogOpen: vi.fn(),
  createAndContinue: vi.fn(),
  creating: false,
  createError: null,
} as unknown as ImageTokenGateState

const previousGeneration = (): ImageGeneration => ({
  id: 91,
  model_profile_id: 7,
  specification_version: 2,
  model: 'gpt-image-2',
  prompt: 'A previous composition',
  parameters: { variants: 4 },
  request_id: 'previous-request',
  status: 'succeeded',
  requested_count: 4,
  succeeded_count: 4,
  final_quota: 4,
  started_at: 1,
  finished_at: 1,
  created_at: 1,
  assets: [],
})

const previousInput = (
  mode: ImageGenerationInput['mode'] = 'generation'
): ImageGenerationInput => ({
  generation_id: 91,
  mode,
  references_available: true,
  references: [],
})

const previousEditInput = (): ImageGenerationInput => ({
  ...previousInput('edit'),
  references: [81, 82].map((id) => ({
    id,
    sha256: String(id).repeat(32),
    size_bytes: 9,
    mime_type: 'image/png',
    width: 1024,
    height: 1024,
  })),
})

const asyncGate = (): ImageTokenGateState => {
  if (!tokenGate.capability) throw new Error('Missing test capability')
  return {
    ...tokenGate,
    capability: {
      ...tokenGate.capability,
      async_submission_enabled: true,
      async_models: ['gpt-image-2'],
    },
  }
}

describe('image composer', () => {
  beforeAll(async () => {
    i18next.addResourceBundle('en', 'translation', en.translation, true, true)
    await i18next.changeLanguage('en')
  })

  beforeEach(() => {
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'image-studio-test',
      role: 1,
    })
    mocks.pendingSubmissions = []
    mocks.quoteExpiresIn = 3600
    mocks.refreshedQuote = null
    mocks.generationQuoteRefetch.mockClear()
    mocks.asyncQuoteRefetch.mockClear()
    mocks.asyncMutation.mutateAsync.mockReset()
    mocks.generationMutation.mutateAsync.mockReset()
    mocks.editMutation.mutateAsync.mockReset()
    mocks.asyncQuoteFailed = false
    mocks.lookup.mockReset()
    mocks.directReferences.ready = true
    mocks.directReferences.processing = false
    mocks.models = [profile()]
    mocks.draftState.draft = null
    mocks.generationQuoteRequest = null
    mocks.asyncQuoteRequest = null
    mocks.editQuoteRequest = null
    mocks.generationMutation.isPending = false
    mocks.generationMutation.variables = undefined
    mocks.editMutation.isPending = false
    mocks.reference.metadata = []
    mocks.reference.files = []
    mocks.reference.processing = false
    mocks.reference.clear.mockClear()
    mocks.reference.replace.mockReset()
    mocks.reference.replace.mockImplementation(
      async (_files: File[], options?: { onCommit?: () => void }) => {
        options?.onCommit?.()
        return true
      }
    )
    mocks.draftState.hydrate.mockClear()
    mocks.draftState.save.mockClear()
    mocks.draftState.clear.mockClear()
  })

  test('reuses inputs with one output and preserves the existing creation draft after success', async () => {
    const user = userEvent.setup()
    mocks.models = [sizedProfile()]
    mocks.draftState.draft = {
      model_profile_id: 7,
      prompt: 'An unrelated unsaved idea',
      parameters: { variants: 3, dimensions: '1024x1024' },
    }
    const generation = previousGeneration()
    generation.sample_id = 404
    generation.parameters.dimensions = '1536x2048'
    mocks.generationMutation.mutateAsync.mockResolvedValue({
      id: 92,
      status: 'succeeded',
    })
    const onSubmitted = vi.fn()
    render(
      <ImageComposer
        reuseGeneration={generation}
        reuseInput={previousInput()}
        tokenGate={tokenGate}
        onSubmitted={onSubmitted}
      />
    )

    expect(await screen.findByLabelText('Prompt')).toHaveValue(
      generation.prompt
    )
    expect(screen.getByLabelText('Custom size')).toHaveValue('1536x2048')
    const generate = screen.getByRole('button', { name: 'Generate 1 image' })
    await waitFor(() => expect(generate).toBeEnabled())
    expect(mocks.generationQuoteRequest?.parameters).toEqual({
      variants: 1,
      dimensions: '1536x2048',
    })
    expect(mocks.generationQuoteRequest?.sample_id).toBeUndefined()
    await user.click(generate)
    await waitFor(() =>
      expect(onSubmitted).toHaveBeenCalledWith({ id: 92, status: 'succeeded' })
    )
    expect(mocks.draftState.hydrate).not.toHaveBeenCalled()
    expect(mocks.draftState.save).not.toHaveBeenCalled()
    expect(mocks.draftState.clear).not.toHaveBeenCalled()
  })

  test('requires an explicit mode choice for legacy tasks before obtaining a quote', async () => {
    const user = userEvent.setup()
    render(
      <ImageComposer
        reuseGeneration={previousGeneration()}
        reuseInput={previousInput('unknown')}
        tokenGate={tokenGate}
        onSubmitted={vi.fn()}
      />
    )
    expect(await screen.findByLabelText('Prompt')).toHaveValue(
      'A previous composition'
    )
    expect(
      screen.getByRole('button', { name: 'Generate 1 image' })
    ).toBeDisabled()
    expect(mocks.generationQuoteRequest).toBeNull()
    await user.click(screen.getByRole('button', { name: 'Generate' }))
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Generate 1 image' })
      ).toBeEnabled()
    )
  })

  test('requires a new confirmation when refreshing an expired quote changes the price', async () => {
    const user = userEvent.setup()
    mocks.quoteExpiresIn = 1
    mocks.refreshedQuote = {
      quota: 2,
      display_amount: '$0.02',
      quote_token: 'new-price',
    }
    render(
      <ImageComposer
        reuseGeneration={previousGeneration()}
        reuseInput={previousInput()}
        tokenGate={tokenGate}
        onSubmitted={vi.fn()}
      />
    )
    const generate = await screen.findByRole('button', {
      name: 'Generate 1 image',
    })
    await waitFor(() => expect(generate).toBeEnabled())
    await user.click(generate)
    await waitFor(() =>
      expect(mocks.generationQuoteRefetch).toHaveBeenCalledOnce()
    )
    expect(mocks.generationMutation.mutateAsync).not.toHaveBeenCalled()
  })

  test('reuses available reference IDs without reuploading files, and keeps edit mode after removing the last reference', async () => {
    const user = userEvent.setup()
    const editProfile = profile()
    editProfile.specification.max_reference_images = 2
    mocks.models = [editProfile]
    mocks.asyncMutation.mutateAsync.mockResolvedValue({
      id: 92,
      status: 'submitting',
    })
    render(
      <ImageComposer
        reuseGeneration={previousGeneration()}
        reuseInput={previousEditInput()}
        tokenGate={asyncGate()}
        onSubmitted={vi.fn()}
      />
    )
    const edit = await screen.findByRole('button', { name: 'Edit image' })
    await waitFor(() => expect(edit).toBeEnabled())
    expect(mocks.asyncQuoteRequest).toMatchObject({
      mode: 'edit',
      asset_ids: [81, 82],
    })
    await user.click(edit)
    await waitFor(() =>
      expect(mocks.asyncMutation.mutateAsync).toHaveBeenCalledWith({
        request: expect.objectContaining({ mode: 'edit', asset_ids: [81, 82] }),
        idempotencyKey: 'submission-key',
      })
    )
    expect(mocks.reference.files).toHaveLength(0)
    expect(mocks.editMutation.mutateAsync).not.toHaveBeenCalled()

    await user.click(
      screen.getByRole('button', { name: 'Remove reference image 1' })
    )
    await waitFor(() =>
      expect(mocks.asyncQuoteRequest?.asset_ids).toEqual([82])
    )
    await user.click(
      screen.getByRole('button', { name: 'Remove reference image 1' })
    )
    expect(screen.getByRole('button', { name: 'Edit image' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Edit' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    await waitFor(() => expect(mocks.asyncQuoteRequest).toBeNull())
  })

  test.each(['missing', 'sync'] as const)(
    'requires replacing all saved references when inputs are %s',
    async (reason) => {
      const user = userEvent.setup()
      const input = previousEditInput()
      input.references_available = reason !== 'missing'
      render(
        <ImageComposer
          reuseGeneration={previousGeneration()}
          reuseInput={input}
          tokenGate={reason === 'sync' ? tokenGate : asyncGate()}
          onSubmitted={vi.fn()}
        />
      )
      const edit = await screen.findByRole('button', { name: 'Edit image' })
      expect(edit).toBeDisabled()
      expect(mocks.asyncQuoteRequest).toBeNull()
      expect(mocks.editQuoteRequest).toBeNull()
      expect(
        screen.getByText(
          'Original references are unavailable or incompatible with the current model. Upload the complete reference set to continue.'
        )
      ).toBeInTheDocument()
      await user.click(
        screen.getByRole('button', { name: 'Use new reference images' })
      )
      expect(screen.getByLabelText('Reference limit')).toBeInTheDocument()
      expect(edit).toBeDisabled()
      expect(screen.getByRole('button', { name: 'Edit' })).toHaveAttribute(
        'aria-pressed',
        'true'
      )
    }
  )

  test('does not silently substitute an unavailable original model', async () => {
    const user = userEvent.setup()
    const anotherModel = {
      ...profile(),
      id: 8,
      model: 'another-model',
      display_name: 'Another model',
    }
    mocks.models = [anotherModel]
    render(
      <ImageComposer
        reuseGeneration={previousGeneration()}
        reuseInput={previousInput()}
        tokenGate={tokenGate}
        onSubmitted={vi.fn()}
      />
    )
    expect(await screen.findByLabelText('Prompt')).toHaveValue(
      'A previous composition'
    )
    expect(
      screen.getByText(
        'The original model is unavailable. Choose a model to continue.'
      )
    ).toBeInTheDocument()
    expect(mocks.generationQuoteRequest).toBeNull()
    await user.selectOptions(
      screen.getByRole('combobox', { name: 'Model' }),
      '8'
    )
    await waitFor(() =>
      expect(mocks.generationQuoteRequest?.model).toBe('another-model')
    )
  })

  test.each(['version', 'unsupported'] as const)(
    'blocks changed %s settings until explicitly reset',
    async (change) => {
      const user = userEvent.setup()
      const generation = previousGeneration()
      if (change === 'version') generation.specification_version = 1
      else generation.parameters.removed_quality = 'high'
      render(
        <ImageComposer
          reuseGeneration={generation}
          reuseInput={previousInput()}
          tokenGate={tokenGate}
          onSubmitted={vi.fn()}
        />
      )
      const generate = await screen.findByRole('button', {
        name: 'Generate 1 image',
      })
      expect(generate).toBeDisabled()
      expect(mocks.generationQuoteRequest).toBeNull()
      await user.click(
        screen.getByRole('button', { name: 'Reset to current model defaults' })
      )
      await waitFor(() => expect(generate).toBeEnabled())
      expect(mocks.generationQuoteRequest?.parameters).toEqual({ variants: 1 })
    }
  )

  test('uses small JSON with ready reference IDs for every enabled user', async () => {
    const user = userEvent.setup()
    mocks.models = [sizedProfile()]
    mocks.reference.metadata = [{ sha256: 'a'.repeat(64), size_bytes: 9 }]
    mocks.reference.files = [
      new File(['reference'], 'reference.png', { type: 'image/png' }),
    ]
    const generation = { id: 31, status: 'submitting' }
    mocks.asyncMutation.mutateAsync.mockResolvedValue(generation)
    const onSubmitted = vi.fn()
    if (!tokenGate.capability) throw new Error('Missing test capability')
    const enabledGate = {
      ...tokenGate,
      capability: {
        ...tokenGate.capability,
        async_submission_enabled: true,
        async_models: ['gpt-image-2'],
      },
    }
    render(<ImageComposer tokenGate={enabledGate} onSubmitted={onSubmitted} />)
    await user.type(
      await screen.findByLabelText('Prompt'),
      'Edit asynchronously; output size: 2048x1536'
    )
    await user.click(screen.getByRole('button', { name: 'Edit' }))
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Edit image' })).toBeEnabled()
    )
    expect(mocks.asyncQuoteRequest?.parameters.dimensions).toBe('2048x1536')
    await user.click(screen.getByRole('button', { name: 'Edit image' }))
    await waitFor(() =>
      expect(mocks.asyncMutation.mutateAsync).toHaveBeenCalledWith({
        request: {
          token_id: 11,
          model: 'gpt-image-2',
          prompt: 'Edit asynchronously; output size: 2048x1536',
          parameters: { variants: 1, dimensions: '2048x1536' },
          mode: 'edit',
          asset_ids: [31],
          quote_token: 'async-quote',
        },
        idempotencyKey: 'submission-key',
      })
    )
    expect(onSubmitted).toHaveBeenCalledWith(generation)
  })

  test.each([2, 3])(
    'defaults advanced parameters disclosure according to its %i controls',
    async (count) => {
      mocks.models = [advancedProfile(count)]
      render(
        <ImageComposer
          presentation='workbench'
          tokenGate={tokenGate}
          onSubmitted={vi.fn()}
        />
      )
      const title = await screen.findByText('More model parameters')
      const disclosure = title.closest('details')
      if (count <= 2) {
        expect(disclosure).toHaveAttribute('open')
      } else {
        expect(disclosure).not.toHaveAttribute('open')
      }
      expect(screen.queryByText('Changed')).not.toBeInTheDocument()
    }
  )

  test('preserves advanced parameters disclosure choice and reports changed or invalid settings', async () => {
    const user = userEvent.setup()
    mocks.models = [advancedProfile(3)]
    render(
      <ImageComposer
        presentation='workbench'
        tokenGate={tokenGate}
        onSubmitted={vi.fn()}
      />
    )
    const title = await screen.findByText('More model parameters')
    const disclosure = title.closest('details')
    await user.click(title)
    await waitFor(() => expect(disclosure).toHaveAttribute('open'))
    fireEvent.change(screen.getByLabelText('Scene description'), {
      target: { value: 'Keep the advanced parameters open' },
    })
    expect(disclosure).toHaveAttribute('open')
    const setting = screen.getByLabelText('Extra parameter 1')
    fireEvent.change(setting, { target: { value: '11' } })
    expect(await screen.findByText('Changed')).toBeInTheDocument()
    fireEvent.change(setting, { target: { value: '10' } })
    await waitFor(() =>
      expect(screen.queryByText('Changed')).not.toBeInTheDocument()
    )
    await user.click(title)
    await waitFor(() => expect(disclosure).not.toHaveAttribute('open'))
    fireEvent.change(screen.getByLabelText('Scene description'), {
      target: { value: 'Keep the advanced parameters closed' },
    })
    expect(disclosure).not.toHaveAttribute('open')
    fireEvent.change(setting, { target: { value: '101' } })
    expect(await screen.findByText('Error')).toBeInTheDocument()
    expect(disclosure).toHaveAttribute('open')
    expect(screen.queryByText('Changed')).not.toBeInTheDocument()
  })

  test('shows one applied prompt dimensions feedback line in the workbench and restores guidance for manual dimensions', async () => {
    mocks.models = [sizedProfile()]
    render(
      <ImageComposer
        presentation='workbench'
        tokenGate={tokenGate}
        onSubmitted={vi.fn()}
      />
    )
    const prompt = await screen.findByLabelText('Scene description')
    const guidance =
      'Include a size such as 2048x1536 in your prompt to apply it automatically.'
    expect(screen.getByText(guidance)).toBeInTheDocument()
    fireEvent.change(prompt, { target: { value: 'A 2048x1536 poster' } })
    const feedback = await screen.findByText(
      'Output size from prompt: 2048 × 1536'
    )
    expect(feedback.parentElement).toHaveClass('is-applied')
    expect(screen.queryByText(guidance)).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Custom width'), {
      target: { value: '1536' },
    })
    expect(await screen.findByText(guidance)).not.toHaveClass('is-applied')
    expect(
      screen.queryByText('Output size from prompt: 2048 × 1536')
    ).not.toBeInTheDocument()
  })

  test('replaces a stale workbench estimate while recalculating and keeps submission disabled', async () => {
    mocks.models = [sizedProfile()]
    render(
      <ImageComposer
        presentation='workbench'
        tokenGate={tokenGate}
        onSubmitted={vi.fn()}
      />
    )
    const prompt = await screen.findByLabelText('Scene description')
    const generate = screen.getByRole('button', { name: 'Generate 1 image' })
    fireEvent.change(prompt, { target: { value: 'A coffee shop poster' } })
    await waitFor(() => expect(generate).toBeEnabled())
    expect(screen.getByText('1 image · $0.01')).toBeInTheDocument()

    fireEvent.change(prompt, {
      target: { value: 'A 2048x1536 coffee shop poster' },
    })
    expect(screen.getByText('Calculating estimated cost')).toBeInTheDocument()
    expect(screen.queryByText('1 image · $0.01')).not.toBeInTheDocument()
    expect(generate).toBeDisabled()
    await waitFor(() => expect(generate).toBeEnabled())
    expect(
      screen.queryByText('Calculating estimated cost')
    ).not.toBeInTheDocument()
    expect(screen.getByText('1 image · $0.01')).toBeInTheDocument()
  })

  test('applies prompt dimensions before quoting and submitting, and invalidates the previous quote immediately', async () => {
    const user = userEvent.setup()
    mocks.models = [sizedProfile()]
    mocks.generationMutation.mutateAsync.mockResolvedValue({
      id: 41,
      status: 'submitting',
    })
    render(<ImageComposer tokenGate={tokenGate} onSubmitted={vi.fn()} />)
    const prompt = await screen.findByLabelText('Prompt')
    const generate = screen.getByRole('button', { name: 'Generate 1 image' })
    fireEvent.change(prompt, { target: { value: 'A coffee shop poster' } })
    await waitFor(() => expect(generate).toBeEnabled())
    expect(mocks.generationQuoteRequest?.parameters.dimensions).toBe(
      '1024x1024'
    )

    fireEvent.change(prompt, {
      target: { value: '一张2048×1536的咖啡海报' },
    })

    expect(screen.getByLabelText('Custom size')).toHaveValue('2048x1536')
    expect(
      screen.getByText('Output size from prompt: 2048 × 1536')
    ).toBeInTheDocument()
    expect(
      screen.getByText('Current output size: 2048 × 1536')
    ).toBeInTheDocument()
    expect(generate).toBeDisabled()
    await waitFor(() => expect(generate).toBeEnabled())
    expect(mocks.generationQuoteRequest).toMatchObject({
      prompt: '一张2048×1536的咖啡海报',
      parameters: { dimensions: '2048x1536' },
    })
    await user.click(generate)
    await waitFor(() => {
      expect(mocks.generationMutation.mutateAsync).toHaveBeenCalledWith({
        request: {
          token_id: 11,
          model: 'gpt-image-2',
          prompt: '一张2048×1536的咖啡海报',
          parameters: { variants: 1, dimensions: '2048x1536' },
          quote_token: 'quote-1',
        },
        idempotencyKey: 'submission-key',
      })
    })
  })

  test('keeps a manual size through unrelated prompt edits and deletion, then applies changed dimensions', async () => {
    mocks.models = [sizedProfile()]
    render(<ImageComposer tokenGate={tokenGate} onSubmitted={vi.fn()} />)
    const prompt = await screen.findByLabelText('Prompt')
    const customSize = screen.getByLabelText('Custom size')
    fireEvent.change(prompt, {
      target: { value: 'A 2048x1536 poster' },
    })
    expect(customSize).toHaveValue('2048x1536')

    fireEvent.change(customSize, { target: { value: '1536x2048' } })
    fireEvent.change(prompt, {
      target: { value: 'A colorful 2048x1536 poster' },
    })
    expect(customSize).toHaveValue('1536x2048')
    await waitFor(() => {
      expect(mocks.generationQuoteRequest?.parameters.dimensions).toBe(
        '1536x2048'
      )
    })

    fireEvent.change(prompt, { target: { value: 'A colorful poster' } })
    expect(customSize).toHaveValue('1536x2048')
    fireEvent.change(prompt, {
      target: { value: 'A colorful 2048x1024 poster' },
    })
    expect(customSize).toHaveValue('2048x1024')
    await waitFor(() => {
      expect(mocks.generationQuoteRequest?.parameters.dimensions).toBe(
        '2048x1024'
      )
    })
  })

  test.each([
    {
      text: 'A 2049x1536 poster',
      displayedSize: '2049x1536',
    },
    { text: 'A poster; output size: 2048x', displayedSize: '1536x2048' },
    {
      text: '一张2048x1536或者1536x2048的海报',
      displayedSize: '1536x2048',
    },
  ])(
    'blocks invalid or conflicting prompt dimensions immediately: $text',
    async ({ text, displayedSize }) => {
      mocks.models = [sizedProfile()]
      mocks.draftState.draft = {
        model_profile_id: 7,
        prompt: 'A poster',
        parameters: { variants: 1, dimensions: '1536x2048' },
      }
      render(<ImageComposer tokenGate={tokenGate} onSubmitted={vi.fn()} />)
      const prompt = await screen.findByLabelText('Prompt')
      const generate = screen.getByRole('button', { name: 'Generate 1 image' })
      await waitFor(() => expect(generate).toBeEnabled())

      fireEvent.change(prompt, { target: { value: text } })

      expect(generate).toBeDisabled()
      expect(screen.getByLabelText('Custom size')).toHaveValue(displayedSize)
      await waitFor(() => expect(mocks.generationQuoteRequest).toBeNull())
      expect(mocks.generationMutation.mutateAsync).not.toHaveBeenCalled()
    }
  )

  test('restores a saved manual size without reapplying prompt dimensions', async () => {
    mocks.models = [sizedProfile()]
    mocks.draftState.draft = {
      model_profile_id: 7,
      prompt: 'A 2048x1536 poster',
      parameters: { variants: 1, dimensions: '1536x2048' },
    }
    render(<ImageComposer tokenGate={tokenGate} onSubmitted={vi.fn()} />)
    const prompt = await screen.findByLabelText('Prompt')
    expect(screen.getByLabelText('Custom size')).toHaveValue('1536x2048')
    fireEvent.change(prompt, {
      target: { value: 'A colorful 2048x1536 poster' },
    })
    await waitFor(() => {
      expect(mocks.generationQuoteRequest?.parameters.dimensions).toBe(
        '1536x2048'
      )
    })
  })

  test('maps the selected size across model keys and preserves an incompatible size across mode changes', async () => {
    const user = userEvent.setup()
    const compatible = sizedProfile('outputSize')
    compatible.id = 8
    compatible.model = 'compatible-image-model'
    compatible.display_name = 'Compatible model'
    const restricted = sizedProfile('resolution')
    restricted.id = 9
    restricted.model = 'restricted-image-model'
    restricted.display_name = 'Restricted model'
    restricted.specification.custom_size = {
      max_edge: 1024,
      step: 16,
      min_pixels: 655360,
      max_pixels: 1048576,
      max_aspect_ratio: 3,
    }
    mocks.models = [sizedProfile(), compatible, restricted]
    mocks.draftState.draft = {
      model_profile_id: 7,
      prompt: 'A poster; output size: 2048x1536',
      parameters: { variants: 1, dimensions: '1536x2048' },
    }
    render(<ImageComposer tokenGate={tokenGate} onSubmitted={vi.fn()} />)
    const model = await screen.findByRole('combobox', { name: 'Model' })

    await user.selectOptions(model, '8')

    expect(screen.getByLabelText('Custom size')).toHaveValue('1536x2048')
    await waitFor(() => {
      expect(mocks.generationQuoteRequest).toMatchObject({
        model: compatible.model,
        parameters: { outputSize: '1536x2048' },
      })
    })
    expect(mocks.generationQuoteRequest?.parameters).not.toHaveProperty(
      'dimensions'
    )

    await user.selectOptions(model, '9')

    expect(screen.getByLabelText('Custom size')).toHaveValue('1536x2048')
    expect(
      screen.getByRole('button', { name: 'Generate 1 image' })
    ).toBeDisabled()
    await waitFor(() => expect(mocks.generationQuoteRequest).toBeNull())
    await user.click(screen.getByRole('button', { name: 'Edit' }))
    expect(screen.getByLabelText('Custom size')).toHaveValue('1536x2048')
    await user.click(screen.getByRole('button', { name: 'Generate' }))
    expect(screen.getByLabelText('Custom size')).toHaveValue('1536x2048')
    expect(
      screen.getByRole('button', { name: 'Generate 1 image' })
    ).toBeDisabled()

    await user.selectOptions(model, '7')

    expect(screen.getByLabelText('Custom size')).toHaveValue('1536x2048')
    await waitFor(() => {
      expect(mocks.generationQuoteRequest?.parameters.dimensions).toBe(
        '1536x2048'
      )
    })
  })

  test.each([true, false])(
    'keeps rejected dimensions visible and enables them after selecting a compatible model (custom sizes: %s)',
    async (customSizes) => {
      const user = userEvent.setup()
      const restricted = sizedProfile()
      restricted.specification.custom_size = {
        max_edge: 1024,
        step: 16,
        min_pixels: 655360,
        max_pixels: 1048576,
        max_aspect_ratio: 3,
      }
      if (!customSizes) delete restricted.specification.custom_size
      const compatible = sizedProfile('outputSize')
      compatible.id = 8
      compatible.model = 'compatible-image-model'
      mocks.models = [restricted, compatible]
      render(<ImageComposer tokenGate={tokenGate} onSubmitted={vi.fn()} />)

      fireEvent.change(await screen.findByLabelText('Prompt'), {
        target: { value: 'A poster; output size: 2048x1536' },
      })

      expect(
        screen.getByLabelText(customSizes ? 'Custom size' : 'Output size')
      ).toHaveValue('2048x1536')
      expect(
        screen.getByRole('button', { name: 'Generate 1 image' })
      ).toBeDisabled()
      await user.selectOptions(
        screen.getByRole('combobox', { name: 'Model' }),
        '8'
      )
      expect(screen.getByLabelText('Custom size')).toHaveValue('2048x1536')
      await waitFor(() => {
        expect(
          screen.getByRole('button', { name: 'Generate 1 image' })
        ).toBeEnabled()
        expect(mocks.generationQuoteRequest).toMatchObject({
          model: compatible.model,
          parameters: { outputSize: '2048x1536' },
        })
      })
    }
  )

  test('blocks a prompt output directive after switching to a model without a size parameter', async () => {
    const user = userEvent.setup()
    const noSize = { ...profile(), id: 8, model: 'no-size-model' }
    mocks.models = [sizedProfile(), noSize]
    render(<ImageComposer tokenGate={tokenGate} onSubmitted={vi.fn()} />)
    fireEvent.change(await screen.findByLabelText('Prompt'), {
      target: { value: 'A poster; output size: 2048x1536' },
    })
    await waitFor(() => {
      expect(
        screen.getByRole('button', { name: 'Generate 1 image' })
      ).toBeEnabled()
    })

    await user.selectOptions(
      screen.getByRole('combobox', { name: 'Model' }),
      '8'
    )

    expect(
      screen.getByRole('button', { name: 'Generate 1 image' })
    ).toBeDisabled()
    expect(
      screen.getByText(
        'This model does not support the output size in your prompt.'
      )
    ).toBeInTheDocument()
    await waitFor(() => expect(mocks.generationQuoteRequest).toBeNull())
  })

  test.each(['pending', 'found'] as const)(
    'looks up an uncertain %s submission without sending another image request',
    async (state) => {
      const user = userEvent.setup()
      mocks.pendingSubmissions = [
        {
          idempotencyKey: 'submission-key',
          requestFingerprint: 'a'.repeat(64),
          expiresAt: 1,
          submittedAt: 1,
        },
      ]
      const generation = previousGeneration()
      mocks.lookup.mockResolvedValue({ state, generation })
      const onSubmitted = vi.fn()
      render(<ImageComposer tokenGate={tokenGate} onSubmitted={onSubmitted} />)
      await user.type(
        await screen.findByLabelText('Prompt'),
        'Find existing request'
      )
      await waitFor(() =>
        expect(
          screen.getByRole('button', { name: 'Generate 1 image' })
        ).toBeEnabled()
      )
      await user.click(screen.getByRole('button', { name: 'Generate 1 image' }))
      await waitFor(() =>
        expect(mocks.lookup).toHaveBeenCalledWith('submission-key')
      )
      await waitFor(() =>
        expect(
          screen.getByRole('button', { name: 'Generate 1 image' })
        ).toBeEnabled()
      )
      expect(mocks.generationMutation.mutateAsync).not.toHaveBeenCalled()
      expect(mocks.asyncMutation.mutateAsync).not.toHaveBeenCalled()
      expect(mocks.generationQuoteRefetch).not.toHaveBeenCalled()
      if (state === 'found') {
        expect(onSubmitted).toHaveBeenCalledWith(generation)
      } else {
        expect(onSubmitted).not.toHaveBeenCalled()
      }
    }
  )

  test('retries a missing submission with its original key and a refreshed quote', async () => {
    const user = userEvent.setup()
    mocks.pendingSubmissions = [
      {
        idempotencyKey: 'submission-key',
        requestFingerprint: 'a'.repeat(64),
        expiresAt: Date.now() + 60_000,
        submittedAt: Date.now(),
      },
    ]
    mocks.lookup.mockResolvedValue({ state: 'not_found' })
    mocks.refreshedQuote = { quote_token: 'retry-quote' }
    const generation = previousGeneration()
    mocks.generationMutation.mutateAsync.mockResolvedValue(generation)
    const onSubmitted = vi.fn()
    render(<ImageComposer tokenGate={tokenGate} onSubmitted={onSubmitted} />)
    fireEvent.change(await screen.findByLabelText('Prompt'), {
      target: { value: 'Find existing request' },
    })
    const generate = screen.getByRole('button', { name: 'Generate 1 image' })
    await waitFor(() => expect(generate).toBeEnabled())
    await user.click(generate)
    await waitFor(() => expect(onSubmitted).toHaveBeenCalledWith(generation))
    expect(mocks.lookup).toHaveBeenCalledWith('submission-key')
    expect(mocks.generationQuoteRefetch).toHaveBeenCalledOnce()
    expect(
      mocks.generationMutation.mutateAsync
    ).toHaveBeenCalledExactlyOnceWith({
      request: {
        token_id: 11,
        model: 'gpt-image-2',
        prompt: 'Find existing request',
        parameters: { variants: 1 },
        quote_token: 'retry-quote',
      },
      idempotencyKey: 'submission-key',
    })
  })

  test('preserves async edit inputs and the original key when retrying a missing submission', async () => {
    const user = userEvent.setup()
    const editProfile = profile()
    editProfile.specification.max_reference_images = 2
    mocks.models = [editProfile]
    mocks.pendingSubmissions = [
      {
        idempotencyKey: 'submission-key',
        requestFingerprint: 'a'.repeat(64),
        expiresAt: Date.now() + 60_000,
        submittedAt: Date.now(),
      },
    ]
    mocks.lookup.mockResolvedValue({ state: 'not_found' })
    mocks.refreshedQuote = { quote_token: 'async-retry-quote' }
    mocks.asyncMutation.mutateAsync.mockResolvedValue({
      id: 92,
      status: 'submitting',
    })
    render(
      <ImageComposer
        reuseGeneration={previousGeneration()}
        reuseInput={previousEditInput()}
        tokenGate={asyncGate()}
        onSubmitted={vi.fn()}
      />
    )
    const edit = await screen.findByRole('button', { name: 'Edit image' })
    await waitFor(() => expect(edit).toBeEnabled())
    await user.click(edit)
    await waitFor(() =>
      expect(mocks.asyncMutation.mutateAsync).toHaveBeenCalledExactlyOnceWith({
        request: {
          token_id: 11,
          model: 'gpt-image-2',
          prompt: 'A previous composition',
          parameters: { variants: 1 },
          mode: 'edit',
          asset_ids: [81, 82],
          quote_token: 'async-retry-quote',
        },
        idempotencyKey: 'submission-key',
      })
    )
    expect(mocks.asyncQuoteRefetch).toHaveBeenCalledOnce()
    expect(mocks.editMutation.mutateAsync).not.toHaveBeenCalled()
    expect(mocks.generationMutation.mutateAsync).not.toHaveBeenCalled()
  })

  test('keeps expired missing receipts protected instead of sending a new request', async () => {
    const user = userEvent.setup()
    const error = vi.spyOn(toast, 'error')
    mocks.pendingSubmissions = [
      {
        idempotencyKey: 'submission-key',
        requestFingerprint: 'a'.repeat(64),
        expiresAt: Date.now() - 1,
        submittedAt: 1,
      },
    ]
    mocks.lookup.mockResolvedValue({ state: 'not_found' })
    render(<ImageComposer tokenGate={tokenGate} onSubmitted={vi.fn()} />)
    fireEvent.change(await screen.findByLabelText('Prompt'), {
      target: { value: 'Find existing request' },
    })
    const generate = screen.getByRole('button', { name: 'Generate 1 image' })
    await waitFor(() => expect(generate).toBeEnabled())
    await user.click(generate)
    await waitFor(() =>
      expect(error).toHaveBeenCalledWith(
        'This submission receipt has expired. Check your image history or contact support before submitting again.'
      )
    )
    expect(clearImageStudioSubmissionKey).not.toHaveBeenCalled()
    expect(mocks.generationQuoteRefetch).not.toHaveBeenCalled()
    expect(mocks.generationMutation.mutateAsync).not.toHaveBeenCalled()
    expect(mocks.asyncMutation.mutateAsync).not.toHaveBeenCalled()
  })

  test.each(['price_changed', 'receipt_expired'] as const)(
    'does not retry when quote refresh reveals %s',
    async (reason) => {
      const user = userEvent.setup()
      mocks.pendingSubmissions = [
        {
          idempotencyKey: 'submission-key',
          requestFingerprint: 'a'.repeat(64),
          expiresAt: Date.now() + 60_000,
          submittedAt: Date.now(),
        },
      ]
      mocks.lookup.mockResolvedValue({ state: 'not_found' })
      if (reason === 'price_changed') {
        mocks.refreshedQuote = { quota: 2, display_amount: '$0.02' }
      } else {
        mocks.generationQuoteRefetch.mockImplementationOnce(() => {
          mocks.pendingSubmissions[0].expiresAt = Date.now() - 1
        })
      }
      render(<ImageComposer tokenGate={tokenGate} onSubmitted={vi.fn()} />)
      fireEvent.change(await screen.findByLabelText('Prompt'), {
        target: { value: 'Find existing request' },
      })
      const generate = screen.getByRole('button', { name: 'Generate 1 image' })
      await waitFor(() => expect(generate).toBeEnabled())
      await user.click(generate)
      await waitFor(() =>
        expect(mocks.generationQuoteRefetch).toHaveBeenCalledOnce()
      )
      await waitFor(() => expect(generate).toBeEnabled())
      expect(mocks.generationMutation.mutateAsync).not.toHaveBeenCalled()
      expect(clearImageStudioSubmissionKey).not.toHaveBeenCalled()
    }
  )

  test.each([
    { status: 400, code: 'invalid_request' },
    { status: 409, code: 'quote_stale' },
  ])(
    'keeps the original receipt after a retry receives $status/$code',
    async ({ status, code }) => {
      const user = userEvent.setup()
      const error = vi.spyOn(toast, 'error')
      mocks.pendingSubmissions = [
        {
          idempotencyKey: 'submission-key',
          requestFingerprint: 'a'.repeat(64),
          expiresAt: Date.now() + 60_000,
          submittedAt: Date.now(),
        },
      ]
      mocks.lookup.mockResolvedValue({ state: 'not_found' })
      mocks.generationMutation.mutateAsync.mockRejectedValue({
        isAxiosError: true,
        response: { status, data: { code } },
      })
      render(<ImageComposer tokenGate={tokenGate} onSubmitted={vi.fn()} />)
      fireEvent.change(await screen.findByLabelText('Prompt'), {
        target: { value: 'Find existing request' },
      })
      const generate = screen.getByRole('button', { name: 'Generate 1 image' })
      await waitFor(() => expect(generate).toBeEnabled())
      await user.click(generate)
      await waitFor(() =>
        expect(mocks.generationMutation.mutateAsync).toHaveBeenCalledOnce()
      )
      await waitFor(() => expect(error).toHaveBeenCalled())
      expect(clearImageStudioSubmissionKey).not.toHaveBeenCalled()
    }
  )

  test('keeps the legacy edit transport when the selected model does not support async', async () => {
    const user = userEvent.setup()
    if (!tokenGate.capability) throw new Error('Missing test capability')
    const enabledGate = {
      ...tokenGate,
      capability: {
        ...tokenGate.capability,
        async_submission_enabled: true,
        async_models: ['another-model'],
      },
    }
    mocks.reference.metadata = [{ sha256: 'a'.repeat(64), size_bytes: 9 }]
    mocks.reference.files = [
      new File(['reference'], 'reference.png', { type: 'image/png' }),
    ]
    mocks.editMutation.mutateAsync.mockResolvedValue({
      id: 9,
      status: 'submitting',
    })
    render(<ImageComposer tokenGate={enabledGate} onSubmitted={vi.fn()} />)
    await user.type(
      await screen.findByLabelText('Prompt'),
      'Keep compatibility'
    )
    await user.click(screen.getByRole('button', { name: 'Edit' }))
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Edit image' })).toBeEnabled()
    )
    await user.click(screen.getByRole('button', { name: 'Edit image' }))
    await waitFor(() =>
      expect(mocks.editMutation.mutateAsync).toHaveBeenCalledOnce()
    )
    expect(mocks.asyncMutation.mutateAsync).not.toHaveBeenCalled()
  })

  test('blocks a failed async quote without silently submitting through legacy transport', async () => {
    const user = userEvent.setup()
    if (!tokenGate.capability) throw new Error('Missing test capability')
    const enabledGate = {
      ...tokenGate,
      capability: {
        ...tokenGate.capability,
        async_submission_enabled: true,
        async_models: ['gpt-image-2'],
      },
    }
    mocks.asyncQuoteFailed = true
    render(<ImageComposer tokenGate={enabledGate} onSubmitted={vi.fn()} />)
    await user.type(await screen.findByLabelText('Prompt'), 'No quote yet')
    await screen.findByText(/Price unavailable/)
    expect(
      screen.getByRole('button', { name: 'Generate 1 image' })
    ).toBeDisabled()
    expect(mocks.generationMutation.mutateAsync).not.toHaveBeenCalled()
    expect(mocks.editMutation.mutateAsync).not.toHaveBeenCalled()
    expect(mocks.asyncMutation.mutateAsync).not.toHaveBeenCalled()
  })

  test('shows the count for one image and updates button and price for four', async () => {
    const user = userEvent.setup()
    render(
      <ImageComposer tokenGate={tokenGate} onSubmitted={() => undefined} />
    )

    await user.type(await screen.findByLabelText('Prompt'), 'Four concepts')

    expect(
      screen.getByRole('button', { name: 'Generate 1 image' })
    ).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByText('1 image · $0.01')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '4' }))

    expect(
      screen.getByRole('button', { name: 'Generate 4 images' })
    ).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByText('4 images · $0.04')).toBeInTheDocument()
    })
  })

  test('uses the submitted request count while generation is pending', async () => {
    const submittedProfile = profile()
    submittedProfile.id = 8
    submittedProfile.model = 'submitted-image-model'
    submittedProfile.specification.parameters[0] = {
      ...submittedProfile.specification.parameters[0],
      key: 'outputs',
    }
    submittedProfile.default_parameters = { outputs: 1 }
    mocks.models = [profile(), submittedProfile]
    mocks.generationMutation.isPending = true
    mocks.generationMutation.variables = {
      request: {
        token_id: 11,
        model: submittedProfile.model,
        prompt: 'Submitted prompt',
        parameters: { outputs: 4 },
        quote_token: 'quote-4',
      },
      idempotencyKey: 'submission-key',
    }

    render(
      <ImageComposer tokenGate={tokenGate} onSubmitted={() => undefined} />
    )

    expect(
      await screen.findByRole('button', { name: 'Generating 4 images...' })
    ).toBeDisabled()
    expect(screen.getByRole('combobox', { name: 'Model' })).toBeDisabled()
    expect(
      screen.queryByRole('button', { name: 'Generating 1 image...' })
    ).not.toBeInTheDocument()
  })

  test('hides and forces the output count to one in edit mode', async () => {
    const user = userEvent.setup()
    const editProfile = profile()
    editProfile.default_parameters = { variants: 4 }
    mocks.models = [editProfile]
    mocks.reference.metadata = [{ sha256: 'a'.repeat(64), size_bytes: 9 }]
    mocks.reference.files = [
      new File(['reference'], 'reference.png', { type: 'image/png' }),
    ]

    render(
      <ImageComposer tokenGate={tokenGate} onSubmitted={() => undefined} />
    )

    await user.type(await screen.findByLabelText('Prompt'), 'Edit this image')
    expect(
      screen.getByRole('group', { name: 'Number of images' })
    ).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Edit' }))

    expect(
      screen.queryByRole('group', { name: 'Number of images' })
    ).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Candidates')).not.toBeInTheDocument()
    await waitFor(() => {
      expect(mocks.editQuoteRequest?.parameters.variants).toBe(1)
    })
  })

  test('restores and selects every key model in both modes and submits the selected edit model', async () => {
    const user = userEvent.setup()
    const flare = {
      ...profile(),
      id: 8,
      model: 'gpt-image-2.5-flare',
      display_name: 'Flare',
    }
    flare.specification.max_reference_images = 2
    const sunburst = {
      ...profile(),
      id: 9,
      model: 'gpt-image-2.5-sunburst',
      display_name: 'Sunburst',
    }
    sunburst.specification.max_reference_images = 4
    mocks.models = [profile(), flare, sunburst]
    mocks.draftState.draft = {
      model_profile_id: flare.id,
      prompt: 'A saved composition',
      parameters: { variants: 4 },
    }
    mocks.reference.metadata = [{ sha256: 'a'.repeat(64), size_bytes: 9 }]
    mocks.reference.files = [
      new File(['reference'], 'reference.png', { type: 'image/png' }),
    ]
    const generation = { id: 19, status: 'succeeded' }
    mocks.editMutation.mutateAsync.mockResolvedValue(generation)
    const onSubmitted = vi.fn()

    render(<ImageComposer tokenGate={tokenGate} onSubmitted={onSubmitted} />)

    const modelSelect = await screen.findByRole('combobox', { name: 'Model' })
    expect(modelSelect).toHaveValue(String(flare.id))
    expect(screen.getByLabelText('Prompt')).toHaveValue('A saved composition')
    expect(screen.getByRole('option', { name: 'Flare' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Sunburst' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Edit' }))

    expect(modelSelect).toBeEnabled()
    expect(modelSelect).toHaveValue(String(flare.id))
    expect(screen.getByLabelText('Reference limit')).toHaveTextContent('2')
    expect(screen.getAllByRole('option')).toHaveLength(4)

    await user.selectOptions(modelSelect, String(sunburst.id))

    expect(screen.getByLabelText('Reference limit')).toHaveTextContent('4')
    await waitFor(() => {
      expect(mocks.editQuoteRequest).toMatchObject({
        token_id: 11,
        model: sunburst.model,
        prompt: 'A saved composition',
        parameters: { variants: 1 },
      })
      expect(screen.getByRole('button', { name: 'Edit image' })).toBeEnabled()
    })
    await user.click(screen.getByRole('button', { name: 'Edit image' }))

    await waitFor(() => {
      expect(mocks.editMutation.mutateAsync).toHaveBeenCalledWith({
        request: {
          ...mocks.editQuoteRequest,
          quote_token: 'edit-quote-1',
        },
        images: mocks.reference.files,
        idempotencyKey: 'submission-key',
      })
      expect(onSubmitted).toHaveBeenCalledWith(generation)
    })

    await user.click(screen.getByRole('button', { name: 'Generate' }))
    expect(modelSelect).toHaveValue(String(sunburst.id))
  })

  test('uses the sample model in edit mode and preserves later manual selection across mode changes', async () => {
    const user = userEvent.setup()
    const sunburst = {
      ...profile(),
      id: 9,
      model: 'gpt-image-2.5-sunburst',
      display_name: 'Sunburst',
    }
    mocks.models = [profile(), sunburst]
    const sample: ImageSample = {
      id: 21,
      model_profile_id: sunburst.id,
      image_asset_id: 1,
      model: sunburst.model,
      title: 'A sample composition',
      prompt: 'A sample composition',
      model_version: sunburst.specification_version,
      parameters: { variants: 4 },
      category: '',
      status: 'published',
      sort_order: 0,
      asset: {
        id: 1,
        position: 0,
        state: 'ready',
        thumbnail_state: 'ready',
        mime_type: 'image/png',
        size_bytes: 9,
        width: 1024,
        height: 1024,
      },
      created_at: 1,
      updated_at: 1,
    }
    mocks.reference.metadata = [{ sha256: 'a'.repeat(64), size_bytes: 9 }]
    const { rerender } = render(
      <ImageComposer tokenGate={tokenGate} onSubmitted={() => undefined} />
    )
    await user.click(screen.getByRole('button', { name: 'Edit' }))

    rerender(
      <ImageComposer
        tokenGate={tokenGate}
        sample={sample}
        onSubmitted={() => undefined}
      />
    )

    const modelSelect = screen.getByRole('combobox', { name: 'Model' })
    expect(modelSelect).toHaveValue(String(sunburst.id))
    await waitFor(() => {
      expect(mocks.editQuoteRequest).toMatchObject({
        model: sunburst.model,
        sample_id: sample.id,
        parameters: { variants: 1 },
      })
    })

    await user.selectOptions(modelSelect, '7')
    await user.click(screen.getByRole('button', { name: 'Generate' }))
    await user.click(screen.getByRole('button', { name: 'Edit' }))

    expect(modelSelect).toHaveValue('7')
    expect(screen.getByLabelText('Prompt')).toHaveValue(sample.prompt)
    await waitFor(() => {
      expect(mocks.editQuoteRequest?.model).toBe('gpt-image-2')
      expect(mocks.editQuoteRequest?.sample_id).toBeUndefined()
    })
  })

  test('keeps a newer draft when a frozen submission succeeds', async () => {
    const user = userEvent.setup()
    let complete: ((generation: ImageGeneration) => void) | undefined
    mocks.generationMutation.mutateAsync.mockImplementation(
      () =>
        new Promise<ImageGeneration>((resolve) => {
          complete = resolve
        })
    )
    const onSubmitted = vi.fn()
    render(
      <ImageComposer
        presentation='workbench'
        tokenGate={tokenGate}
        onSubmitted={onSubmitted}
      />
    )
    const prompt = await screen.findByLabelText('Scene description')
    await user.type(prompt, 'First composition')
    const generate = screen.getByRole('button', { name: 'Generate 1 image' })
    await waitFor(() => expect(generate).toBeEnabled())
    await user.click(generate)
    await waitFor(() =>
      expect(mocks.generationMutation.mutateAsync).toHaveBeenCalledOnce()
    )
    mocks.draftState.clear.mockClear()
    await user.clear(prompt)
    await user.type(prompt, 'A newer idea')
    mocks.draftState.clear.mockClear()
    await act(async () => complete?.(previousGeneration()))
    expect(prompt).toHaveValue('A newer idea')
    expect(mocks.draftState.clear).not.toHaveBeenCalled()
    expect(onSubmitted).toHaveBeenCalledWith(
      expect.objectContaining({ id: 91 }),
      expect.objectContaining({
        mode: 'generation',
        values: expect.objectContaining({ prompt: 'First composition' }),
      })
    )
  })

  test('loads the complete regeneration context and restores an ordinary draft', async () => {
    const user = userEvent.setup()
    const model = profile()
    model.specification.max_reference_images = 2
    mocks.models = [model]
    mocks.draftState.draft = {
      model_profile_id: 7,
      prompt: 'Ordinary draft',
      parameters: { variants: 3 },
    }
    const settled = vi.fn()
    const { rerender } = render(
      <ImageComposer
        presentation='workbench'
        tokenGate={asyncGate()}
        onSubmitted={vi.fn()}
        onLoadRequestSettled={settled}
      />
    )
    expect(await screen.findByLabelText('Scene description')).toHaveValue(
      'Ordinary draft'
    )
    const request: ImageComposerLoadRequest = {
      revision: 1,
      kind: 'regenerate',
      generation: previousGeneration(),
      input: previousEditInput(),
    }
    rerender(
      <ImageComposer
        presentation='workbench'
        tokenGate={asyncGate()}
        onSubmitted={vi.fn()}
        loadRequest={request}
        onLoadRequestSettled={settled}
      />
    )
    await user.click(
      await screen.findByRole('button', { name: 'Load settings' })
    )
    expect(await screen.findByLabelText('Editing instructions')).toHaveValue(
      'A previous composition'
    )
    await waitFor(() =>
      expect(mocks.asyncQuoteRequest).toMatchObject({
        mode: 'edit',
        asset_ids: [81, 82],
        parameters: { variants: 1 },
      })
    )
    expect(settled).toHaveBeenCalledWith(1, true)
    expect(mocks.draftState.save).not.toHaveBeenCalled()
    await user.click(
      screen.getByRole('button', { name: 'Restore previous draft' })
    )
    expect(await screen.findByLabelText('Scene description')).toHaveValue(
      'Ordinary draft'
    )
    await waitFor(() =>
      expect(mocks.asyncQuoteRequest).toMatchObject({
        mode: 'generation',
        asset_ids: [],
        parameters: { variants: 3 },
      })
    )
    expect(
      screen.queryByRole('button', { name: 'Restore previous draft' })
    ).not.toBeInTheDocument()
  })

  test('rejects an output loaded against an older editor revision', async () => {
    const user = userEvent.setup()
    const revisions = vi.fn()
    const settled = vi.fn()
    const { rerender } = render(
      <ImageComposer
        presentation='workbench'
        tokenGate={tokenGate}
        onSubmitted={vi.fn()}
        onEditorRevisionChange={revisions}
      />
    )
    const prompt = await screen.findByLabelText('Scene description')
    const originalRevision = Number(revisions.mock.lastCall?.[0])
    await user.type(prompt, 'Do not overwrite this')
    const request: ImageComposerLoadRequest = {
      revision: 5,
      kind: 'edit-output',
      file: new File(['image'], 'finished.png', { type: 'image/png' }),
      expectedEditorRevision: originalRevision,
    }
    rerender(
      <ImageComposer
        presentation='workbench'
        tokenGate={tokenGate}
        onSubmitted={vi.fn()}
        loadRequest={request}
        onEditorRevisionChange={revisions}
        onLoadRequestSettled={settled}
      />
    )
    await waitFor(() => expect(settled).toHaveBeenCalledWith(5, false))
    expect(prompt).toHaveValue('Do not overwrite this')
    expect(mocks.reference.replace).not.toHaveBeenCalled()
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })

  test('opens an output directly for editing without replacing its prompt or size or submitting it', async () => {
    const user = userEvent.setup()
    const model = sizedProfile()
    model.specification.max_reference_images = 2
    mocks.models = [model]
    mocks.draftState.draft = {
      model_profile_id: model.id,
      prompt: 'Keep this description and size',
      parameters: { variants: 4, dimensions: '1536x1024' },
    }
    const output = new File(['finished image'], 'finished.png', {
      type: 'image/png',
    })
    mocks.reference.replace.mockImplementation(
      async (files: File[], options?: { onCommit?: () => void }) => {
        mocks.reference.files = [...files]
        mocks.reference.metadata = [
          { sha256: 'b'.repeat(64), size_bytes: output.size },
        ]
        options?.onCommit?.()
        return true
      }
    )
    const generation = { ...previousGeneration(), id: 92, status: 'submitting' }
    mocks.asyncMutation.mutateAsync.mockResolvedValue(generation)
    const onSubmitted = vi.fn()
    const settled = vi.fn()
    const common = {
      presentation: 'workbench' as const,
      tokenGate: asyncGate(),
      onSubmitted,
      onLoadRequestSettled: settled,
    }
    const { rerender } = render(<ImageComposer {...common} />)
    expect(await screen.findByLabelText('Scene description')).toHaveValue(
      'Keep this description and size'
    )
    rerender(
      <ImageComposer
        {...common}
        loadRequest={{
          revision: 21,
          kind: 'edit-output',
          file: output,
          generation: previousGeneration(),
        }}
      />
    )
    expect(await screen.findByLabelText('Editing instructions')).toHaveValue(
      'Keep this description and size'
    )
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Custom width')).toHaveValue(1536)
    expect(screen.getByLabelText('Custom height')).toHaveValue(1024)
    expect(mocks.reference.replace).toHaveBeenCalledExactlyOnceWith(
      [output],
      expect.any(Object)
    )
    expect(settled).toHaveBeenCalledWith(21, true)
    await waitFor(() =>
      expect(mocks.asyncQuoteRequest).toMatchObject({
        mode: 'edit',
        asset_ids: [31],
        prompt: 'Keep this description and size',
        parameters: { variants: 1, dimensions: '1536x1024' },
      })
    )
    expect(mocks.asyncMutation.mutateAsync).not.toHaveBeenCalled()
    expect(mocks.generationMutation.mutateAsync).not.toHaveBeenCalled()
    expect(mocks.editMutation.mutateAsync).not.toHaveBeenCalled()
    const edit = screen.getByRole('button', { name: 'Edit image' })
    await waitFor(() => expect(edit).toBeEnabled())
    await user.click(edit)
    await waitFor(() =>
      expect(mocks.asyncMutation.mutateAsync).toHaveBeenCalledExactlyOnceWith({
        request: {
          token_id: 11,
          model: model.model,
          prompt: 'Keep this description and size',
          parameters: { variants: 1, dimensions: '1536x1024' },
          mode: 'edit',
          asset_ids: [31],
          quote_token: 'async-quote',
        },
        idempotencyKey: 'submission-key',
      })
    )
    expect(onSubmitted).toHaveBeenCalledWith(
      generation,
      expect.objectContaining({ mode: 'edit', referenceFiles: [output] })
    )
    expect(mocks.generationMutation.mutateAsync).not.toHaveBeenCalled()
    expect(mocks.editMutation.mutateAsync).not.toHaveBeenCalled()
  })

  test.each(['local', 'saved'] as const)(
    'confirms replacing %s references and preserves the edit draft on cancellation',
    async (source) => {
      const user = userEvent.setup()
      const model = sizedProfile()
      model.specification.max_reference_images = 2
      mocks.models = [model]
      const original = new File(['original'], 'original.png', {
        type: 'image/png',
      })
      const output = new File(['finished image'], 'finished.png', {
        type: 'image/png',
      })
      const originalGeneration = previousGeneration()
      originalGeneration.prompt = 'Keep my editing instructions'
      originalGeneration.parameters.dimensions = '1536x1024'
      if (source === 'local') {
        mocks.draftState.draft = {
          model_profile_id: model.id,
          prompt: originalGeneration.prompt,
          parameters: { ...originalGeneration.parameters },
        }
        mocks.reference.files = [original]
        mocks.reference.metadata = [
          { sha256: 'a'.repeat(64), size_bytes: original.size },
        ]
      }
      const settled = vi.fn()
      const common = {
        presentation: 'workbench' as const,
        tokenGate: asyncGate(),
        onSubmitted: vi.fn(),
        onLoadRequestSettled: settled,
        reuseGeneration: source === 'saved' ? originalGeneration : undefined,
        reuseInput: source === 'saved' ? previousEditInput() : undefined,
      }
      const { rerender } = render(<ImageComposer {...common} />)
      if (source === 'local') {
        await user.click(screen.getByRole('button', { name: 'Edit' }))
      }
      expect(await screen.findByLabelText('Editing instructions')).toHaveValue(
        originalGeneration.prompt
      )
      const originalAssetIds = source === 'saved' ? [81, 82] : [31]
      await waitFor(() =>
        expect(mocks.asyncQuoteRequest?.asset_ids).toEqual(originalAssetIds)
      )
      rerender(
        <ImageComposer
          {...common}
          loadRequest={{ revision: 22, kind: 'edit-output', file: output }}
        />
      )
      expect(await screen.findByRole('alertdialog')).toHaveAccessibleName(
        'Replace the current reference images?'
      )
      expect(
        screen.queryByText('Load these creation settings?')
      ).not.toBeInTheDocument()
      await user.click(
        screen.getByRole('button', { name: 'Keep editing current draft' })
      )
      expect(settled).toHaveBeenCalledWith(22, false)
      expect(mocks.reference.replace).not.toHaveBeenCalled()
      expect(screen.getByLabelText('Editing instructions')).toHaveValue(
        originalGeneration.prompt
      )
      expect(screen.getByLabelText('Custom width')).toHaveValue(1536)
      expect(screen.getByLabelText('Custom height')).toHaveValue(1024)
      expect(mocks.asyncQuoteRequest?.asset_ids).toEqual(originalAssetIds)
      expect(mocks.reference.files).toEqual(
        source === 'local' ? [original] : []
      )

      mocks.reference.replace.mockImplementation(
        async (files: File[], options?: { onCommit?: () => void }) => {
          mocks.reference.files = [...files]
          mocks.reference.metadata = [
            { sha256: 'b'.repeat(64), size_bytes: output.size },
          ]
          options?.onCommit?.()
          return true
        }
      )
      rerender(
        <ImageComposer
          {...common}
          loadRequest={{ revision: 23, kind: 'edit-output', file: output }}
        />
      )
      await user.click(
        await screen.findByRole('button', { name: 'Use this image to edit' })
      )
      await waitFor(() => expect(settled).toHaveBeenCalledWith(23, true))
      expect(mocks.reference.replace).toHaveBeenCalledExactlyOnceWith(
        [output],
        expect.any(Object)
      )
      expect(mocks.reference.files).toEqual([output])
      expect(screen.getByLabelText('Editing instructions')).toHaveValue(
        originalGeneration.prompt
      )
      await waitFor(() =>
        expect(mocks.asyncQuoteRequest).toMatchObject({
          mode: 'edit',
          asset_ids: [31],
          parameters: { variants: 1, dimensions: '1536x1024' },
        })
      )
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
      expect(mocks.asyncMutation.mutateAsync).not.toHaveBeenCalled()
    }
  )

  test('preserves mode and inputs when an output cannot replace the references', async () => {
    const user = userEvent.setup()
    mocks.reference.replace.mockResolvedValue(false)
    const settled = vi.fn()
    const { rerender } = render(
      <ImageComposer
        presentation='workbench'
        tokenGate={tokenGate}
        onSubmitted={vi.fn()}
      />
    )
    const prompt = await screen.findByLabelText('Scene description')
    await user.type(prompt, 'Keep this composition')
    rerender(
      <ImageComposer
        presentation='workbench'
        tokenGate={tokenGate}
        onSubmitted={vi.fn()}
        loadRequest={{
          revision: 6,
          kind: 'edit-output',
          file: new File(['image'], 'finished.png', { type: 'image/png' }),
        }}
        onLoadRequestSettled={settled}
      />
    )
    await waitFor(() => expect(settled).toHaveBeenCalledWith(6, false))
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    expect(prompt).toHaveValue('Keep this composition')
    expect(screen.getByRole('button', { name: 'Generate' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    expect(
      screen.queryByRole('button', { name: 'Restore previous draft' })
    ).not.toBeInTheDocument()
  })

  test('confirms replacing parameter-only edits even with an empty prompt', async () => {
    const user = userEvent.setup()
    mocks.models = [sizedProfile()]
    const { rerender } = render(
      <ImageComposer
        presentation='workbench'
        tokenGate={tokenGate}
        onSubmitted={vi.fn()}
      />
    )
    const width = await screen.findByLabelText('Custom width')
    fireEvent.change(width, { target: { value: '1536' } })
    expect(screen.getByLabelText('Scene description')).toHaveValue('')
    rerender(
      <ImageComposer
        presentation='workbench'
        tokenGate={tokenGate}
        onSubmitted={vi.fn()}
        loadRequest={{
          revision: 7,
          kind: 'regenerate',
          generation: previousGeneration(),
          input: previousInput(),
        }}
      />
    )
    expect(await screen.findByRole('alertdialog')).toBeInTheDocument()
    expect(mocks.reference.replace).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Load settings' }))
    expect(
      await screen.findByRole('button', { name: 'Restore previous draft' })
    ).toBeInTheDocument()
  })

  test('applies a revision sample in edit mode without dropping the reference files', async () => {
    const user = userEvent.setup()
    const original = profile()
    const target = {
      ...profile(),
      id: 8,
      model: 'another-model',
      display_name: 'Another model',
    }
    mocks.models = [original, target]
    const reference = new File(['reference'], 'reference.png', {
      type: 'image/png',
    })
    mocks.reference.files = [reference]
    mocks.reference.metadata = [{ sha256: 'a'.repeat(64), size_bytes: 9 }]
    const { rerender } = render(
      <ImageComposer
        presentation='workbench'
        tokenGate={tokenGate}
        onSubmitted={vi.fn()}
      />
    )
    await user.click(screen.getByRole('button', { name: 'Edit' }))
    const sample = publishedSample({
      id: 41,
      model_profile_id: 8,
      model: 'another-model',
      prompt: 'A sample edit',
      parameters: { variants: 4 },
    })
    rerender(
      <ImageComposer
        presentation='workbench'
        tokenGate={tokenGate}
        onSubmitted={vi.fn()}
        loadRequest={{ revision: 8, kind: 'sample', sample }}
      />
    )
    await user.click(
      await screen.findByRole('button', { name: 'Load settings' })
    )
    expect(await screen.findByLabelText('Editing instructions')).toHaveValue(
      'A sample edit'
    )
    expect(mocks.reference.replace).toHaveBeenCalledWith(
      [reference],
      expect.any(Object)
    )
    await waitFor(() =>
      expect(mocks.editQuoteRequest).toMatchObject({
        model: 'another-model',
        sample_id: 41,
        parameters: { variants: 1 },
      })
    )
  })

  test('invalidates a delayed output when the model specification changes without references', async () => {
    const revisions = vi.fn()
    const settled = vi.fn()
    const { rerender } = render(
      <ImageComposer
        presentation='workbench'
        tokenGate={tokenGate}
        onSubmitted={vi.fn()}
        onEditorRevisionChange={revisions}
      />
    )
    await screen.findByLabelText('Scene description')
    const expectedRevision = Number(revisions.mock.lastCall?.[0])
    mocks.models = [{ ...profile(), specification_version: 3 }]
    rerender(
      <ImageComposer
        presentation='workbench'
        tokenGate={tokenGate}
        onSubmitted={vi.fn()}
        onEditorRevisionChange={revisions}
      />
    )
    expect(Number(revisions.mock.lastCall?.[0])).toBeGreaterThan(
      expectedRevision
    )
    rerender(
      <ImageComposer
        presentation='workbench'
        tokenGate={tokenGate}
        onSubmitted={vi.fn()}
        loadRequest={{
          revision: 9,
          kind: 'edit-output',
          file: new File(['image'], 'finished.png', { type: 'image/png' }),
          expectedEditorRevision: expectedRevision,
        }}
        onLoadRequestSettled={settled}
      />
    )
    await waitFor(() => expect(settled).toHaveBeenCalledWith(9, false))
    expect(mocks.reference.replace).not.toHaveBeenCalled()
  })

  test('keeps deleted saved references deleted when applying a sample in edit mode', async () => {
    const user = userEvent.setup()
    const model = profile()
    model.specification.max_reference_images = 2
    mocks.models = [model]
    const common = {
      presentation: 'workbench' as const,
      tokenGate: asyncGate(),
      onSubmitted: vi.fn(),
      reuseGeneration: previousGeneration(),
      reuseInput: previousEditInput(),
    }
    const { rerender } = render(<ImageComposer {...common} />)
    await user.click(
      await screen.findByRole('button', { name: 'Remove reference image 2' })
    )
    await waitFor(() =>
      expect(mocks.asyncQuoteRequest?.asset_ids).toEqual([81])
    )
    const sample = publishedSample({
      id: 51,
      model_profile_id: 7,
      prompt: 'A sample edit',
      parameters: { variants: 1 },
    })
    rerender(
      <ImageComposer
        {...common}
        loadRequest={{ revision: 10, kind: 'sample', sample }}
      />
    )
    await user.click(
      await screen.findByRole('button', { name: 'Load settings' })
    )
    expect(await screen.findByLabelText('Editing instructions')).toHaveValue(
      'A sample edit'
    )
    await waitFor(() =>
      expect(mocks.asyncQuoteRequest?.asset_ids).toEqual([81])
    )
    expect(
      screen.queryByRole('button', { name: 'Remove reference image 2' })
    ).not.toBeInTheDocument()
  })

  test('keeps local replacement references when applying a sample to a reused edit task', async () => {
    const user = userEvent.setup()
    const model = profile()
    model.specification.max_reference_images = 2
    mocks.models = [model]
    mocks.reference.files = [
      new File(['local'], 'local.png', { type: 'image/png' }),
    ]
    mocks.reference.metadata = [{ sha256: 'a'.repeat(64), size_bytes: 5 }]
    const common = {
      presentation: 'workbench' as const,
      tokenGate: asyncGate(),
      onSubmitted: vi.fn(),
      reuseGeneration: previousGeneration(),
      reuseInput: previousEditInput(),
    }
    const { rerender } = render(<ImageComposer {...common} />)
    await user.click(
      await screen.findByRole('button', { name: 'Use new reference images' })
    )
    await waitFor(() =>
      expect(mocks.asyncQuoteRequest?.asset_ids).toEqual([31])
    )
    const sample = publishedSample({
      id: 52,
      model_profile_id: 7,
      prompt: 'A different sample',
      parameters: { variants: 1 },
    })
    rerender(
      <ImageComposer
        {...common}
        loadRequest={{ revision: 11, kind: 'sample', sample }}
      />
    )
    await user.click(
      await screen.findByRole('button', { name: 'Load settings' })
    )
    expect(await screen.findByLabelText('Editing instructions')).toHaveValue(
      'A different sample'
    )
    await waitFor(() =>
      expect(mocks.asyncQuoteRequest?.asset_ids).toEqual([31])
    )
    expect(
      screen.queryByText('Original reference images')
    ).not.toBeInTheDocument()
    expect(mocks.reference.replace).toHaveBeenCalledWith(
      mocks.reference.files,
      expect.any(Object)
    )
  })
})
