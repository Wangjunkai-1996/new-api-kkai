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
import {
  Activity02Icon,
  Cancel01Icon,
  DragDropHorizontalIcon,
  Refresh01Icon,
  SparklesIcon,
  WifiFullSignalIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type PointerEvent,
} from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useSiteConfig } from '@/features/site-config'

import {
  runNetworkCheck,
  summarizeCheck,
  type CheckEnd,
  type ConnectionVerdict,
  type ProbeSample,
} from './network-check'

import './network-diagnostic.css'

type Position = { x: number; y: number }
type Drag = {
  pointerId: number
  startX: number
  startY: number
  origin: Position
  width: number
  height: number
  mobile: boolean
  moved: boolean
  last: Position
}

const POSITION_KEY = 'kkai-network-diagnostic-position'
const VIEWPORT_MARGIN = 12

function readPosition(): Position | null {
  try {
    const raw = localStorage.getItem(POSITION_KEY)
    if (!raw) return null
    const parsed: unknown = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object') return null
    const value = parsed as Record<string, unknown>
    if (typeof value.x !== 'number' || typeof value.y !== 'number') return null
    if (!Number.isFinite(value.x) || !Number.isFinite(value.y)) return null
    return { x: value.x, y: value.y }
  } catch {
    return null
  }
}

function savePosition(position: Position): void {
  try {
    localStorage.setItem(POSITION_KEY, JSON.stringify(position))
  } catch {
    // Position memory is optional when storage is unavailable.
  }
}

function clampPosition(
  position: Position,
  width: number,
  height: number
): Position {
  return {
    x: Math.min(
      Math.max(VIEWPORT_MARGIN, position.x),
      Math.max(VIEWPORT_MARGIN, window.innerWidth - width - VIEWPORT_MARGIN)
    ),
    y: Math.min(
      Math.max(VIEWPORT_MARGIN, position.y),
      Math.max(VIEWPORT_MARGIN, window.innerHeight - height - VIEWPORT_MARGIN)
    ),
  }
}

function isMobile(): boolean {
  return window.matchMedia('(max-width: 500px)').matches
}

export function NetworkDiagnostic() {
  const site = useSiteConfig()
  if (site.data?.tenant_id !== 0) return null
  return <NetworkDiagnosticWidget />
}

export function NetworkDiagnosticWidget() {
  const { t } = useTranslation()
  const titleId = useId()
  const panelRef = useRef<HTMLElement>(null)
  const launcherRef = useRef<HTMLButtonElement>(null)
  const closeRef = useRef<HTMLButtonElement>(null)
  const abortRef = useRef<AbortController | null>(null)
  const closeTimerRef = useRef<number | null>(null)
  const runIdRef = useRef(0)
  const dragRef = useRef<Drag | null>(null)
  const [open, setOpen] = useState(false)
  const [closing, setClosing] = useState(false)
  const [running, setRunning] = useState(false)
  const [samples, setSamples] = useState<ProbeSample[]>([])
  const [end, setEnd] = useState<CheckEnd | null>(null)
  const [position, setPosition] = useState<Position | null>(readPosition)
  const [pageHidden, setPageHidden] = useState(document.hidden)

  const summary = end ? summarizeCheck({ samples, end }) : null
  const verdict = summary?.verdict
  let mascotState = 'idle'
  if (running) mascotState = 'testing'
  else if (verdict === 'smooth') mascotState = 'good'
  else if (verdict) mascotState = 'weak'

  const closePanel = useCallback(() => {
    abortRef.current?.abort()
    abortRef.current = null
    runIdRef.current += 1
    setRunning(false)
    setOpen(false)
    setClosing(true)
    if (closeTimerRef.current !== null) {
      window.clearTimeout(closeTimerRef.current)
    }
    closeTimerRef.current = window.setTimeout(
      () => {
        setSamples([])
        setEnd(null)
        setClosing(false)
        closeTimerRef.current = null
        requestAnimationFrame(() => launcherRef.current?.focus())
      },
      window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 200
    )
  }, [])

  useEffect(() => {
    const onVisibilityChange = () => setPageHidden(document.hidden)
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () =>
      document.removeEventListener('visibilitychange', onVisibilityChange)
  }, [])

  useEffect(
    () => () => {
      abortRef.current?.abort()
      if (closeTimerRef.current !== null) {
        window.clearTimeout(closeTimerRef.current)
      }
    },
    []
  )

  useEffect(() => {
    if (!open) return
    closeRef.current?.focus()
  }, [open])

  useLayoutEffect(() => {
    if (!open || !position || isMobile()) return
    const panel = panelRef.current
    if (!panel) return
    const rect = panel.getBoundingClientRect()
    const clamped = clampPosition(position, rect.width, rect.height)
    if (clamped.x === position.x && clamped.y === position.y) return
    setPosition(clamped)
    savePosition(clamped)
  }, [open, position, samples.length, end])

  useEffect(() => {
    if (!open) return
    const onResize = () => {
      const panel = panelRef.current
      if (!panel || isMobile()) return
      const rect = panel.getBoundingClientRect()
      const current = position ?? { x: rect.left, y: rect.top }
      const clamped = clampPosition(current, rect.width, rect.height)
      if (position && clamped.x === position.x && clamped.y === position.y) {
        return
      }
      setPosition(clamped)
      savePosition(clamped)
    }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [open, position])

  async function startCheck() {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    const runId = ++runIdRef.current
    setSamples([])
    setEnd(null)
    setRunning(true)

    try {
      const result = await runNetworkCheck(controller.signal, (next) => {
        if (runId === runIdRef.current) setSamples(next)
      })
      if (runId === runIdRef.current) setEnd(result.end)
    } catch {
      if (runId === runIdRef.current && !controller.signal.aborted) {
        setEnd('unavailable')
      }
    } finally {
      if (runId === runIdRef.current) {
        setRunning(false)
        abortRef.current = null
      }
    }
  }

  function onDragStart(event: PointerEvent<HTMLElement>) {
    if (event.button !== 0 || !event.isPrimary) return
    const target = event.target
    if (
      !(target instanceof Element) ||
      target.closest('.network-diagnostic__close')
    ) {
      return
    }
    if (isMobile() && !target.closest('.network-diagnostic__grip')) return
    const panel = panelRef.current
    if (!panel) return
    const rect = panel.getBoundingClientRect()
    dragRef.current = {
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      origin: { x: rect.left, y: rect.top },
      width: rect.width,
      height: rect.height,
      mobile: isMobile(),
      moved: false,
      last: { x: rect.left, y: rect.top },
    }
    panel.dataset.dragging = 'true'
    event.currentTarget.setPointerCapture(event.pointerId)
  }

  function onDragMove(event: PointerEvent<HTMLElement>) {
    const drag = dragRef.current
    if (!drag || drag.pointerId !== event.pointerId) return
    const dx = event.clientX - drag.startX
    const dy = event.clientY - drag.startY
    if (!drag.moved && Math.hypot(dx, dy) < 5) return
    drag.moved = true
    if (drag.mobile) {
      panelRef.current?.style.setProperty(
        '--network-swipe-y',
        `${Math.max(0, dy)}px`
      )
      return
    }
    drag.last = clampPosition(
      { x: drag.origin.x + dx, y: drag.origin.y + dy },
      drag.width,
      drag.height
    )
    setPosition(drag.last)
  }

  function onDragEnd(event: PointerEvent<HTMLElement>) {
    const drag = dragRef.current
    if (!drag || drag.pointerId !== event.pointerId) return
    dragRef.current = null
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId)
    }
    panelRef.current?.removeAttribute('data-dragging')
    if (drag.mobile) {
      if (
        drag.moved &&
        event.type === 'pointerup' &&
        event.clientY - drag.startY > 90
      ) {
        closePanel()
      } else {
        panelRef.current?.style.setProperty('--network-swipe-y', '0px')
      }
    } else if (drag.moved) {
      savePosition(drag.last)
    }
  }

  function onGripKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    if (isMobile()) return
    const panel = panelRef.current
    if (!panel) return
    const directions: Record<string, Position> = {
      ArrowLeft: { x: -1, y: 0 },
      ArrowRight: { x: 1, y: 0 },
      ArrowUp: { x: 0, y: -1 },
      ArrowDown: { x: 0, y: 1 },
    }
    const direction = directions[event.key]
    if (!direction) return
    event.preventDefault()
    const rect = panel.getBoundingClientRect()
    const step = event.shiftKey ? 32 : 12
    const next = clampPosition(
      { x: rect.left + direction.x * step, y: rect.top + direction.y * step },
      rect.width,
      rect.height
    )
    setPosition(next)
    savePosition(next)
  }

  function onPanelKeyDown(event: KeyboardEvent<HTMLElement>) {
    if (event.key !== 'Escape') return
    event.stopPropagation()
    closePanel()
  }

  function verdictLabel(value: ConnectionVerdict | undefined): string {
    switch (value) {
      case 'smooth':
        return t('Connection smooth')
      case 'fair':
        return t('Connection fair')
      case 'slow':
        return t('Connection slow')
      case 'fluctuating':
        return t('Connection fluctuates')
      case 'unstable':
        return t('Connection unstable')
      case 'incomplete':
        return t('Unable to complete test')
      case 'limited':
        return t('Test limited')
      case 'unavailable':
        return t('Unable to diagnose')
      default:
        return running ? t('Checking connection') : t('Ready to test')
    }
  }

  function verdictDetail(value: ConnectionVerdict | undefined): string {
    switch (value) {
      case 'smooth':
        return t('Connection is steady')
      case 'fair':
      case 'slow':
      case 'fluctuating':
      case 'unstable':
        return t('Try another network or VPN node, then test again')
      case 'incomplete':
        return t('Too few checks succeeded to judge the connection')
      case 'limited':
        return t('The test is rate limited. Try again later')
      case 'unavailable':
        return end === 'invalid-response'
          ? t('The API did not return a valid test response')
          : t('Browser access may be restricted or the connection failed')
      default:
        return running
          ? t('Checking the API · {{completed}}/5', {
              completed: samples.length,
            })
          : t('Check your connection to the API')
    }
  }

  const successes = samples.filter((sample) => sample.kind === 'success')
  const maxMs = Math.max(1, ...successes.map((sample) => sample.ms))
  const failureCount = samples.length - successes.length
  const state = running ? 'testing' : (verdict ?? 'idle')
  let samplesCaption = t('5 checks')
  if (running) {
    samplesCaption = t('{{completed}} of 5 completed', {
      completed: samples.length,
    })
  } else if (end) {
    samplesCaption = t('{{success}} succeeded · {{failed}} failed', {
      success: successes.length,
      failed: failureCount,
    })
  }
  let actionLabel = t('Start check')
  if (running) actionLabel = t('Testing...')
  else if (end) actionLabel = t('Check again')
  const panelVisible = open || closing

  return (
    <div
      className='network-diagnostic'
      data-state={state}
      data-mascot={mascotState}
      data-paused={pageHidden ? 'true' : undefined}
    >
      <button
        ref={launcherRef}
        type='button'
        className='network-diagnostic__launcher'
        aria-label={t('Open network diagnostics')}
        aria-expanded={open}
        aria-controls={titleId}
        onClick={() => setOpen(true)}
        hidden={panelVisible}
      >
        <span
          className='network-diagnostic__launcher-mascot'
          aria-hidden='true'
        />
        <span className='network-diagnostic__launcher-dot' aria-hidden='true' />
        <span className='network-diagnostic__launcher-hint' aria-hidden='true'>
          {t('Having trouble? Let me check')}
        </span>
      </button>

      {panelVisible && (
        <section
          id={titleId}
          ref={panelRef}
          className='network-diagnostic__panel'
          role='dialog'
          aria-labelledby={`${titleId}-title`}
          aria-hidden={closing}
          inert={closing}
          data-closing={closing ? 'true' : undefined}
          onKeyDown={onPanelKeyDown}
          style={position ? { left: position.x, top: position.y } : undefined}
        >
          <header
            className='network-diagnostic__header'
            onPointerDown={onDragStart}
            onPointerMove={onDragMove}
            onPointerUp={onDragEnd}
            onPointerCancel={onDragEnd}
          >
            <button
              type='button'
              className='network-diagnostic__grip'
              aria-label={t('Move network diagnostics')}
              title={t('Move network diagnostics')}
              onKeyDown={onGripKeyDown}
            >
              <HugeiconsIcon icon={DragDropHorizontalIcon} aria-hidden='true' />
            </button>
            <span
              className='network-diagnostic__character'
              aria-hidden='true'
            />
            <span className='network-diagnostic__signal' aria-hidden='true'>
              <HugeiconsIcon icon={WifiFullSignalIcon} />
            </span>
            <div className='network-diagnostic__heading'>
              <div className='network-diagnostic__eyebrow'>
                <HugeiconsIcon icon={SparklesIcon} aria-hidden='true' />
                {t('KKAI assistant')}
              </div>
              <h2 id={`${titleId}-title`}>{t('Network diagnostics')}</h2>
              <span>api.kkrich.ltd</span>
            </div>
            <button
              ref={closeRef}
              type='button'
              className='network-diagnostic__close'
              aria-label={t('Close network diagnostics')}
              title={t('Close network diagnostics')}
              onClick={closePanel}
            >
              <HugeiconsIcon icon={Cancel01Icon} aria-hidden='true' />
            </button>
          </header>

          <div className='network-diagnostic__body'>
            <div className='network-diagnostic__metric-head'>
              <span
                className='network-diagnostic__verdict'
                data-verdict={state}
                role='status'
              >
                {verdictLabel(verdict)}
              </span>
              <span className='network-diagnostic__metric-label'>
                {t('Average latency')}
              </span>
            </div>
            <div className='network-diagnostic__metric' aria-live='polite'>
              <span className='network-diagnostic__number'>
                {running ? `${samples.length}/5` : (summary?.averageMs ?? '--')}
              </span>
              <span className='network-diagnostic__unit'>
                {running ? '' : t('ms')}
              </span>
            </div>
            <p className='network-diagnostic__detail'>
              {verdictDetail(verdict)}
            </p>

            <div
              className='network-diagnostic__samples'
              role='list'
              aria-label={t('5 checks')}
            >
              {Array.from({ length: 5 }, (_, index) => {
                const sample = samples[index]
                let label = t('Check {{index}}: not run', { index: index + 1 })
                if (sample?.kind === 'success') {
                  label = t('Check {{index}}: {{latency}} ms', {
                    index: index + 1,
                    latency: sample.ms,
                  })
                } else if (sample?.kind === 'timeout') {
                  label = t('Check {{index}}: timed out', { index: index + 1 })
                } else if (sample?.kind === 'failed') {
                  label = t('Check {{index}}: failed', { index: index + 1 })
                }
                const barHeight =
                  sample?.kind === 'success'
                    ? Math.max(10, (sample.ms / maxMs) * 41)
                    : 8
                return (
                  <span
                    key={index}
                    className='network-diagnostic__sample'
                    data-result={sample?.kind ?? 'pending'}
                    role='listitem'
                    aria-label={label}
                    title={label}
                    style={{ height: barHeight }}
                  >
                    {sample?.kind === 'timeout' || sample?.kind === 'failed' ? (
                      <HugeiconsIcon icon={Cancel01Icon} aria-hidden='true' />
                    ) : null}
                  </span>
                )
              })}
            </div>
            <p className='network-diagnostic__samples-caption'>
              {samplesCaption}
            </p>

            <Button
              className='network-diagnostic__action'
              size='lg'
              disabled={running}
              onClick={() => void startCheck()}
            >
              <HugeiconsIcon
                icon={end ? Refresh01Icon : Activity02Icon}
                data-icon='inline-start'
                aria-hidden='true'
              />
              {actionLabel}
            </Button>
            <p className='network-diagnostic__foot'>
              {t('Current browser · HTTPS connection')}
            </p>
          </div>
        </section>
      )}
    </div>
  )
}
