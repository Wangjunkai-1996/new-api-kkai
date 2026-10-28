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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosError } from 'axios'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import type {
  VideoGeneration,
  VideoModelProfile,
  VideoSubmissionReceipt,
} from './types'
import { VideoCreatePage } from './video-create-page'

const mocks = vi.hoisted(() => ({
  blockAndRecheck: vi.fn(),
  markTokenHealthy: vi.fn(),
  navigate: vi.fn(),
  refetchModels: vi.fn(),
  gate: {} as Record<string, unknown>,
  modelsQuery: {} as Record<string, unknown>,
  sampleQuery: {} as Record<string, unknown>,
  generationsQuery: {} as Record<string, unknown>,
}))

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const original =
    await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...original,
    useNavigate: () => mocks.navigate,
    Link: (props: { children?: ReactNode; to: string }) => (
      <a href={props.to}>{props.children}</a>
    ),
  }
})

vi.mock('@/hooks/use-media-query', () => ({
  useMediaQuery: (query: string) => query.includes('min-width'),
}))

vi.mock('./hooks/use-video-token-gate', () => ({
  useVideoTokenGate: () => mocks.gate,
}))

vi.mock('./queries', () => ({
  useVideoModels: () => mocks.modelsQuery,
  useVideoSample: () => mocks.sampleQuery,
  useVideoGenerations: (
    _filters: unknown,
    _polling: boolean,
    _task: string | undefined,
    id: number | undefined
  ) => ({
    ...mocks.generationsQuery,
    targetGeneration: (mocks.generationsQuery.items as VideoGeneration[]).find(
      (item) => item.id === id
    ),
  }),
  isVideoGenerationTombstoneError: () => false,
}))

vi.mock('./components/video-composer', () => ({
  VideoComposer: (props: {
    onSubmitted?: (receipt: VideoSubmissionReceipt) => void
  }) => (
    <div>
      <span>composer-mounted</span>
      <input aria-label='Draft text' defaultValue='My draft' />
      <button
        type='button'
        onClick={() => props.onSubmitted?.({ task_id: 'task_accepted' })}
      >
        Accept submission
      </button>
    </div>
  ),
}))

vi.mock('@/lib/studio-media', () => ({
  useStudioMediaUrls: () => ({
    data: { urls: {}, deniedPaths: {} },
    identityDenied: false,
    refetch: vi.fn(),
  }),
  signedStudioMediaUrl: () => undefined,
}))
vi.mock('./components/video-generation-player', () => ({
  VideoGenerationPlayer: (props: {
    generation: VideoGeneration
    active: boolean
    onPlay?: () => void
  }) => (
    <div data-testid='result-player' data-active={props.active}>
      <span>{props.generation.prompt}</span>
      <button type='button' onClick={props.onPlay}>
        Play result
      </button>
    </div>
  ),
}))
vi.mock('./components/video-generation-details', () => ({
  VideoGenerationDetails: () => null,
}))

vi.mock('./components/video-sample-gallery', () => ({
  VideoSampleGallery: (props: {
    previewActive?: boolean
    onPreviewStart?: () => void
  }) => (
    <div data-testid='gallery' data-active={props.previewActive}>
      <span>gallery-mounted</span>
      <button type='button' onClick={props.onPreviewStart}>
        Play inspiration
      </button>
    </div>
  ),
}))

vi.mock('./components/video-studio-nav', () => ({
  VideoStudioNav: () => <nav>studio-nav</nav>,
}))

const profile: VideoModelProfile = {
  id: 1,
  model: 'video-model',
  display_name: 'Video Model',
  description: '',
  provider_label: '',
  specification_version: 1,
  specification: {
    version: 1,
    modes: ['text_to_video'],
    parameters: [],
  },
  default_parameters: {},
  enabled: true,
  sort_order: 0,
  created_at: 1,
  updated_at: 1,
}

const setPreparingGate = () => {
  Object.assign(mocks.gate, {
    access: null,
    tokenId: null,
    requiredGroup: 'Seedance 视频',
    effectiveGroup: 'Seedance 视频',
    checking: false,
    preparing: true,
    checkFailed: false,
    gateAction: 'create',
    actionAvailable: true,
    queryFetching: false,
    creating: true,
    createError: null,
    blockAndRecheck: mocks.blockAndRecheck,
    markTokenHealthy: mocks.markTokenHealthy,
    retry: vi.fn(),
    createAndContinue: vi.fn(),
  })
}

const setReadyGate = () => {
  Object.assign(mocks.gate, {
    access: {
      kind: 'ready',
      requiredGroup: 'Seedance 视频',
      tokenId: 17,
      tokenName: '视频工作室',
    },
    tokenId: 17,
    preparing: false,
    creating: false,
    gateAction: 'none',
    actionAvailable: false,
  })
}

const setModelsQuery = (input: {
  data?: VideoModelProfile[]
  error?: unknown
  isError?: boolean
  isFetching?: boolean
}) => {
  Object.assign(mocks.modelsQuery, {
    data: input.data,
    error: input.error ?? null,
    isError: input.isError ?? false,
    isFetching: input.isFetching ?? false,
    isSuccess: input.data !== undefined && !input.isError,
    refetch: mocks.refetchModels,
  })
}

const setSampleQuery = (input: { error?: unknown; isError?: boolean } = {}) => {
  Object.assign(mocks.sampleQuery, {
    data: undefined,
    error: input.error ?? null,
    isError: input.isError ?? false,
  })
}

describe('video studio entry gate', () => {
  beforeEach(() => {
    setPreparingGate()
    setModelsQuery({})
    setSampleQuery()
    Object.assign(mocks.generationsQuery, {
      items: [],
      isError: false,
      isLoading: false,
      isSuccess: true,
      targetDiscoveryExpired: false,
      targetUnavailable: false,
      refresh: vi.fn(),
      retryTargetDiscovery: vi.fn(),
    })
  })

  test('does not mount false model or sample empty states while ensuring access', () => {
    const { rerender } = render(<VideoCreatePage />)

    expect(
      screen.getByText('videoStudio.workspace.preparing')
    ).toBeInTheDocument()
    expect(screen.queryByText('gallery-mounted')).not.toBeInTheDocument()
    expect(screen.queryByText('composer-mounted')).not.toBeInTheDocument()
    expect(screen.queryByText('videoStudio.noModels')).not.toBeInTheDocument()
    expect(screen.queryByText('videoStudio.noSamples')).not.toBeInTheDocument()

    setReadyGate()
    setModelsQuery({ data: [profile] })
    rerender(<VideoCreatePage />)

    expect(screen.getByText('gallery-mounted')).toBeInTheDocument()
    expect(screen.getByText('composer-mounted')).toBeInTheDocument()
  })

  test.each([
    {
      name: 'failed request',
      query: { isError: true, error: new Error('failed') },
      message: 'videoStudio.workspace.prepareFailed',
    },
    {
      name: 'empty model catalog',
      query: { data: [] },
      message: 'videoStudio.noModels',
    },
  ])(
    'keeps a $name recoverable without mounting the workspace',
    async (scenario) => {
      const user = userEvent.setup()
      setReadyGate()
      setModelsQuery(scenario.query)

      render(<VideoCreatePage />)

      expect(screen.getByText(scenario.message)).toBeInTheDocument()
      expect(screen.queryByText('gallery-mounted')).not.toBeInTheDocument()
      expect(screen.queryByText('composer-mounted')).not.toBeInTheDocument()

      await user.click(
        screen.getByRole('button', { name: 'videoStudio.retry' })
      )
      expect(mocks.refetchModels).toHaveBeenCalledTimes(1)
    }
  )

  test('keeps the workspace mounted when a refresh fails with cached models', () => {
    setReadyGate()
    setModelsQuery({
      data: [profile],
      isError: true,
      error: new Error('failed refresh'),
    })

    render(<VideoCreatePage />)

    expect(screen.getByText('gallery-mounted')).toBeInTheDocument()
    expect(screen.getByText('composer-mounted')).toBeInTheDocument()
    expect(
      screen.queryByText('videoStudio.workspace.prepareFailed')
    ).not.toBeInTheDocument()
  })

  test('an accepted submission stays on create, keeps the draft and locates its task', async () => {
    setReadyGate()
    setModelsQuery({ data: [profile] })
    render(<VideoCreatePage />)
    const draft = screen.getByRole('textbox', { name: 'Draft text' })
    fireEvent.change(draft, { target: { value: 'My ongoing edit' } })
    fireEvent.click(screen.getByRole('button', { name: 'Accept submission' }))
    expect(screen.getByRole('textbox', { name: 'Draft text' })).toBe(draft)
    expect(draft).toHaveValue('My ongoing edit')
    expect(screen.getByText('task_accepted')).toBeInTheDocument()
    expect(
      screen.getByText('videoStudio.generationLocating')
    ).toBeInTheDocument()
    expect(mocks.navigate).not.toHaveBeenCalled()
  })

  test('choosing a recent result keeps the editor instance and media owners exclude one another', () => {
    setReadyGate()
    setModelsQuery({ data: [profile] })
    mocks.generationsQuery.items = [
      {
        id: 2,
        task_id: 'task_2',
        model: 'Model 2',
        prompt: 'Scene two',
        status: 'ready',
        parameters: {},
        progress: '',
        created_at: 1,
      },
      {
        id: 1,
        task_id: 'task_1',
        model: 'Model 1',
        prompt: 'Scene one',
        status: 'ready',
        parameters: {},
        progress: '',
        created_at: 1,
      },
    ]
    render(<VideoCreatePage />)
    const draft = screen.getByRole('textbox', { name: 'Draft text' })
    fireEvent.change(draft, { target: { value: 'Keep this draft' } })
    fireEvent.click(screen.getByRole('button', { name: /Scene one/ }))
    expect(screen.getByRole('textbox', { name: 'Draft text' })).toBe(draft)
    expect(draft).toHaveValue('Keep this draft')
    expect(screen.getByTestId('result-player')).toHaveTextContent('Scene one')
    fireEvent.click(screen.getByRole('button', { name: 'Play result' }))
    expect(screen.getByTestId('result-player')).toHaveAttribute(
      'data-active',
      'true'
    )
    fireEvent.click(screen.getByRole('button', { name: 'Play inspiration' }))
    expect(screen.getByTestId('result-player')).toHaveAttribute(
      'data-active',
      'false'
    )
    expect(screen.getByTestId('gallery')).toHaveAttribute('data-active', 'true')
  })

  test('marks the token healthy after models load successfully', async () => {
    setReadyGate()
    setModelsQuery({ data: [profile] })

    render(<VideoCreatePage />)

    await waitFor(() => {
      expect(mocks.markTokenHealthy).toHaveBeenCalledWith(17)
    })
  })

  test('rechecks capability when the initial sample rejects the bound token', async () => {
    const tokenError = new AxiosError('invalid video token')
    Object.assign(tokenError, {
      response: { data: { code: 'video_token_invalid' } },
    })
    setReadyGate()
    setModelsQuery({ data: [profile] })
    setSampleQuery({ error: tokenError, isError: true })

    render(<VideoCreatePage initialSampleId={7} />)

    await waitFor(() => {
      expect(mocks.blockAndRecheck).toHaveBeenCalledWith('token-invalid')
    })
  })

  test('does not block the workspace for a generic initial sample error', () => {
    setReadyGate()
    setModelsQuery({ data: [profile] })
    setSampleQuery({ error: new Error('network unavailable'), isError: true })

    render(<VideoCreatePage initialSampleId={7} />)

    expect(mocks.blockAndRecheck).toHaveBeenCalledWith('request-failed')
    expect(screen.getByText('gallery-mounted')).toBeInTheDocument()
    expect(screen.getByText('composer-mounted')).toBeInTheDocument()
  })

  test('rechecks capability when the model catalog rejects the bound token', async () => {
    const tokenError = new AxiosError('invalid video token')
    Object.assign(tokenError, {
      response: { data: { code: 'video_token_invalid' } },
    })
    setReadyGate()
    setModelsQuery({ error: tokenError, isError: true })

    render(<VideoCreatePage />)

    await waitFor(() => {
      expect(mocks.blockAndRecheck).toHaveBeenCalledWith('token-invalid')
    })
  })
})
