import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ConsoleCompatibilityBoundary } from './console-compatibility-boundary'

const state = vi.hoisted(() => ({
  required: true,
  compatible: true,
  loading: false,
  retry: vi.fn(),
}))
vi.mock('@/hooks/use-console-contract', () => ({
  useConsoleContract: () => state,
}))

function Editor() {
  const [draft, setDraft] = useState('')
  return (
    <input
      aria-label='Draft'
      value={draft}
      onChange={(event) => setDraft(event.target.value)}
    />
  )
}

describe('console compatibility recovery', () => {
  beforeEach(() => {
    state.required = true
    state.compatible = true
    state.loading = false
  })

  it('shows loading until compatibility is known and an error only after failure', () => {
    state.compatible = false
    state.loading = true
    const view = render(
      <ConsoleCompatibilityBoundary>
        <Editor />
      </ConsoleCompatibilityBoundary>
    )

    expect(screen.getByRole('status')).toHaveTextContent('Loading...')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Retry' })
    ).not.toBeInTheDocument()
    expect(screen.getByLabelText('Draft').parentElement).toHaveAttribute(
      'inert'
    )

    state.loading = false
    view.rerender(
      <ConsoleCompatibilityBoundary>
        <Editor />
      </ConsoleCompatibilityBoundary>
    )
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.getByRole('alert')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeVisible()
    expect(screen.getByLabelText('Draft').parentElement).toHaveAttribute(
      'inert'
    )

    state.compatible = true
    view.rerender(
      <ConsoleCompatibilityBoundary>
        <Editor />
      </ConsoleCompatibilityBoundary>
    )
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Draft').parentElement).not.toHaveAttribute(
      'inert'
    )
  })

  it('keeps edits mounted while compatibility is unavailable and retries without a reload', () => {
    const view = render(
      <ConsoleCompatibilityBoundary>
        <Editor />
      </ConsoleCompatibilityBoundary>
    )
    fireEvent.change(screen.getByRole('textbox', { name: 'Draft' }), {
      target: { value: 'unsaved work' },
    })
    state.compatible = false
    view.rerender(
      <ConsoleCompatibilityBoundary>
        <Editor />
      </ConsoleCompatibilityBoundary>
    )
    expect(screen.getByRole('alert')).toBeVisible()
    expect(screen.getByLabelText('Draft').parentElement).toHaveAttribute(
      'inert'
    )
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(state.retry).toHaveBeenCalledOnce()
    state.compatible = true
    view.rerender(
      <ConsoleCompatibilityBoundary>
        <Editor />
      </ConsoleCompatibilityBoundary>
    )
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Draft' })).toHaveValue(
      'unsaved work'
    )
  })
})
