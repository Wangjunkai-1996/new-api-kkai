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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { beforeAll, beforeEach, describe, expect, test, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import { useAuthStore } from '@/stores/auth-store'

import type { ImageTokenGateState } from '../hooks/use-image-token-gate'
import type {
  CreateImageRequest,
  ImageComposerValues,
  ImageEditQuoteRequest,
  ImageModelProfile,
  ImageQuoteRequest,
  ImageReferenceMetadata,
  ImageSample,
} from '../types'
import { ImageComposer } from './image-composer'

type GenerationVariables = {
  request: CreateImageRequest
  idempotencyKey: string
}

const mocks = vi.hoisted(() => ({
  models: [] as ImageModelProfile[],
  editQuoteRequest: null as ImageEditQuoteRequest | null,
  generationMutation: {
    isPending: false,
    variables: undefined as GenerationVariables | undefined,
    mutateAsync: vi.fn(),
  },
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
  },
}))

vi.mock('../queries', () => ({
  useImageModels: () => ({ data: mocks.models, isLoading: false }),
  useCreateImageGeneration: () => mocks.generationMutation,
  useCreateImageEdit: () => mocks.editMutation,
  useImageQuote: (request: ImageQuoteRequest | null) => {
    const count = Number(request?.parameters.variants ?? 1)
    const quote = request
      ? {
          quota: count,
          display_amount: `$0.0${String(count)}`,
          quote_token: `quote-${String(count)}`,
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

vi.mock('../hooks/use-image-references', () => ({
  useImageReferences: () => mocks.reference,
}))

vi.mock('@/stores/image-studio-draft-store', () => ({
  clearImageStudioSubmissionKey: vi.fn(),
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
    mocks.models = [profile()]
    mocks.draftState.draft = null
    mocks.editQuoteRequest = null
    mocks.generationMutation.isPending = false
    mocks.generationMutation.variables = undefined
    mocks.editMutation.isPending = false
    mocks.reference.metadata = []
    mocks.reference.files = []
    mocks.reference.processing = false
    mocks.reference.clear.mockClear()
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
})
