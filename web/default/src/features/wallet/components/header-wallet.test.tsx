import { act, fireEvent, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { HeaderWallet } from './header-wallet'

const state = vi.hoisted(() => ({
  visible: true,
  balance: 125 as number | undefined,
  isPending: false,
  isFetching: false,
  error: null as unknown,
  lastUpdated: Date.parse('2026-11-19T03:04:00Z') as number | undefined,
  refetch: vi.fn(),
}))

vi.mock('@tanstack/react-router', () => ({
  Link: (props: {
    children?: ReactNode
    className?: string
    hash?: string
    onClick?: () => void
    to: string
  }) => (
    <a
      href={`${props.to}${props.hash ? `#${props.hash}` : ''}`}
      className={props.className}
      onClick={props.onClick}
    >
      {props.children}
    </a>
  ),
}))

vi.mock('@/hooks/use-sidebar-config', () => ({
  useIsSidebarModuleVisible: () => state.visible,
}))

vi.mock('@/features/wallet/hooks/use-wallet-balance', () => ({
  useWalletBalance: () => ({
    data: state.balance === undefined ? undefined : { quota: state.balance },
    dataUpdatedAt: state.lastUpdated ?? 0,
    error: state.error,
    isFetching: state.isFetching,
    isPending: state.isPending,
    refetch: state.refetch,
  }),
}))

vi.mock('@/lib/format', () => ({
  formatQuota: (quota: number) => `$${(quota / 100).toFixed(2)}`,
}))

beforeEach(() => {
  state.visible = true
  state.balance = 125
  state.isPending = false
  state.isFetching = false
  state.error = null
  state.lastUpdated = Date.parse('2026-11-19T03:04:00Z')
  state.refetch.mockReset()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('HeaderWallet', () => {
  it('opens the anchored wallet popover and keeps navigation outside payment flow', () => {
    render(<HeaderWallet />)

    fireEvent.click(screen.getByRole('button', { name: 'Wallet: $1.25' }))

    expect(screen.getByText('Current Balance')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /View/ })).toHaveAttribute(
      'href',
      '/wallet'
    )
    expect(screen.getByRole('link', { name: /recharge/i })).toHaveAttribute(
      'href',
      '/wallet#wallet-recharge-heading'
    )
    expect(state.refetch).not.toHaveBeenCalled()
  })

  it('uses the deliberate desktop hover open and close delays', () => {
    vi.useFakeTimers()
    render(<HeaderWallet />)
    const trigger = screen.getByRole('button', { name: 'Wallet: $1.25' })

    fireEvent.mouseEnter(trigger)
    act(() => vi.advanceTimersByTime(74))
    expect(screen.queryByText('Current Balance')).not.toBeInTheDocument()
    act(() => vi.advanceTimersByTime(1))
    expect(screen.getByText('Current Balance')).toBeInTheDocument()

    fireEvent.mouseLeave(trigger)
    act(() => vi.advanceTimersByTime(139))
    expect(trigger).toHaveAttribute('aria-expanded', 'true')
    act(() => vi.advanceTimersByTime(1))
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
  })

  it('masks the amount everywhere in the trigger and popover', () => {
    render(<HeaderWallet />)
    fireEvent.click(screen.getByRole('button', { name: 'Wallet: $1.25' }))

    fireEvent.click(screen.getByRole('button', { name: 'Hide' }))

    expect(screen.queryByText('$1.25')).not.toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Wallet: Hide' })
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Show' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
  })

  it('shows a retry state without manufacturing a zero balance', () => {
    state.balance = undefined
    state.error = new Error('unavailable')
    render(<HeaderWallet />)
    fireEvent.click(
      screen.getByRole('button', { name: 'Wallet: Not available' })
    )

    expect(screen.getByRole('status')).toHaveTextContent('Not available')
    expect(screen.queryByText('$0.00')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Retry balance' }))
    expect(state.refetch).toHaveBeenCalledTimes(1)
  })

  it('does not render when the wallet module is hidden', () => {
    state.visible = false
    render(<HeaderWallet />)

    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})
