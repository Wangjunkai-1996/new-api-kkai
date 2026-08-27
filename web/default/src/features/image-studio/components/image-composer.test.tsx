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
  ImageModelProfile,
  ImageQuoteRequest,
} from '../types'
import { ImageComposer } from './image-composer'

type GenerationVariables = {
  request: CreateImageRequest
  idempotencyKey: string
}

const mocks = vi.hoisted(() => ({
  models: [] as ImageModelProfile[],
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
    draft: null,
    hydrate: vi.fn(),
    save: vi.fn(),
    clear: vi.fn(),
  },
  reference: {
    metadata: [],
    files: [],
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
  useImageEditQuote: () => ({
    data: undefined,
    isFetching: false,
    isError: false,
    refetch: vi.fn().mockResolvedValue({ data: undefined }),
  }),
}))

vi.mock('../hooks/use-image-edit-references', () => ({
  useImageEditReferences: () => ({
    profile: undefined,
    maxImages: 1,
    references: mocks.reference,
  }),
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

describe('image composer batch copy', () => {
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
    mocks.generationMutation.isPending = false
    mocks.generationMutation.variables = undefined
    mocks.editMutation.isPending = false
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
})
