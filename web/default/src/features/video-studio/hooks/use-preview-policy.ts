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
import { useReducedMotion } from 'motion/react'
import { useSyncExternalStore } from 'react'

import { useMediaQuery } from '@/hooks/use-media-query'

type NetworkInformation = EventTarget & {
  saveData?: boolean
}

const getConnection = (): NetworkInformation | undefined => {
  if (typeof navigator === 'undefined') return undefined
  return (navigator as Navigator & { connection?: NetworkInformation })
    .connection
}

const subscribeToSaveData = (onChange: () => void): (() => void) => {
  const connection = getConnection()
  if (!connection) return () => {}
  connection.addEventListener('change', onChange)
  return () => connection.removeEventListener('change', onChange)
}

const getSaveDataSnapshot = (): boolean => Boolean(getConnection()?.saveData)

export const usePreviewPolicy = () => {
  const reducedMotion = useReducedMotion()
  const coarsePointer = useMediaQuery('(pointer: coarse)')
  const saveData = useSyncExternalStore(
    subscribeToSaveData,
    getSaveDataSnapshot,
    () => false
  )

  return {
    autoplay: !reducedMotion && !coarsePointer && !saveData,
    motion: !reducedMotion && !coarsePointer,
  }
}
