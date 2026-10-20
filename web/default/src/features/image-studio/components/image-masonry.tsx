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
import { motion, useReducedMotion } from 'motion/react'
import { useLayoutEffect, useRef, useState, type ReactNode } from 'react'

import type { ImageGeneration } from '../types'

export function ImageMasonry(props: {
  items: ImageGeneration[]
  renderItem: (generation: ImageGeneration) => ReactNode
}) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  const [measurements, setMeasurements] = useState(
    new Map<number, { width: number; height: number }>()
  )
  const reduceMotion = useReducedMotion()

  useLayoutEffect(() => {
    const container = containerRef.current
    if (!container) return
    const observer = new ResizeObserver((entries) => {
      const nextWidth = entries[0]?.contentRect.width ?? 0
      if (nextWidth > 0) setWidth(nextWidth)
    })
    const initialWidth = container.getBoundingClientRect().width
    if (initialWidth > 0) setWidth(initialWidth)
    observer.observe(container)
    return () => observer.disconnect()
  }, [])

  useLayoutEffect(() => {
    const container = containerRef.current
    if (!container) return
    const observer = new ResizeObserver((entries) => {
      setMeasurements((previous) => {
        const next = new Map(previous)
        let changed = false
        for (const entry of entries) {
          const id = Number(
            (entry.target as HTMLElement).dataset.masonryContent
          )
          const rect = entry.contentRect
          const measured = previous.get(id)
          if (
            rect.height > 0 &&
            (!measured ||
              Math.abs(measured.height - rect.height) > 0.5 ||
              Math.abs(measured.width - rect.width) > 0.5)
          ) {
            next.set(id, { width: rect.width, height: rect.height })
            changed = true
          }
        }
        return changed ? next : previous
      })
    })
    container.querySelectorAll('[data-masonry-content]').forEach((element) => {
      observer.observe(element)
    })
    return () => observer.disconnect()
  }, [props.items])

  const gap = width < 640 ? 12 : 16
  const columns = width < 340 ? 1 : Math.max(2, Math.floor((width + gap) / 264))
  const columnWidth = (width - gap * (columns - 1)) / columns
  const columnHeights = Array.from({ length: columns }, () => 0)
  const positions = props.items.map((generation) => {
    const column = columnHeights.indexOf(Math.min(...columnHeights))
    const measurement = measurements.get(generation.id)
    const cover = generation.assets
      .filter((asset) => asset.state === 'ready')
      .sort(
        (left, right) => left.position - right.position || left.id - right.id
      )[0]
    const ratio =
      cover && cover.width > 0 && cover.height > 0
        ? cover.width / cover.height
        : 4 / 3
    const estimatedHeight = columnWidth / ratio + 96
    const height =
      measurement && Math.abs(measurement.width - columnWidth) < 1
        ? measurement.height
        : estimatedHeight
    const position = {
      x: column * (columnWidth + gap),
      y: columnHeights[column],
    }
    columnHeights[column] += height + gap
    return position
  })

  return (
    <div
      ref={containerRef}
      role='list'
      className='relative w-full'
      style={
        width > 0 ? { height: Math.max(0, ...columnHeights) - gap } : undefined
      }
    >
      {props.items.map((generation, index) => {
        const position = positions[index]
        return (
          <motion.div
            key={generation.id}
            role='listitem'
            data-generation-id={generation.id}
            className={width > 0 ? 'absolute top-0 left-0' : 'mb-4'}
            style={width > 0 ? { width: columnWidth } : undefined}
            initial={false}
            animate={{
              x: width > 0 ? position.x : 0,
              y: width > 0 ? position.y : 0,
            }}
            transition={
              reduceMotion
                ? { duration: 0 }
                : {
                    duration: 0.3,
                    ease: [0.33, 1, 0.68, 1],
                  }
            }
          >
            <motion.div
              data-masonry-content={generation.id}
              initial={
                reduceMotion ? false : { opacity: 0, y: 14, scale: 0.98 }
              }
              whileInView={
                reduceMotion ? undefined : { opacity: 1, y: 0, scale: 1 }
              }
              viewport={{ once: true, amount: 0.1 }}
              transition={{
                duration: 0.28,
                delay: Math.min(index % columns, 6) * 0.035,
                ease: [0.33, 1, 0.68, 1],
              }}
            >
              {props.renderItem(generation)}
            </motion.div>
          </motion.div>
        )
      })}
    </div>
  )
}
