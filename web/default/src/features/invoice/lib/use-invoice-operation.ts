import { useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'

import { getServerErrorSources } from '@/lib/server-error-message'

export function useInvoiceOperation() {
  const client = useQueryClient()
  const keys = useRef(new Map<string, string>())
  const busyRef = useRef(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  async function run<T>(
    operation: string,
    payload: unknown,
    action: (key: string) => Promise<T>,
    onError?: (cause: unknown) => void
  ): Promise<T | undefined> {
    if (busyRef.current) return undefined
    const digest = operation + JSON.stringify(payload)
    const key = keys.current.get(digest) || crypto.randomUUID()
    keys.current.set(digest, key)
    busyRef.current = true
    setBusy(true)
    setError(null)
    try {
      const result = await action(key)
      keys.current.delete(digest)
      await client.invalidateQueries({ queryKey: ['invoices'] })
      return result
    } catch (cause) {
      setError(cause)
      onError?.(cause)
      const code = getServerErrorSources(cause)
        .map((source) => source.code)
        .find((value) => typeof value === 'string')
      if (
        String(code).includes('CONFLICT') ||
        [
          'INVALID_STATE',
          'PAYMENT_CHANGED',
          'DELIVERY_VERSION_CHANGED',
        ].includes(String(code))
      ) {
        await client.invalidateQueries({ queryKey: ['invoices'] })
      }
      return undefined
    } finally {
      busyRef.current = false
      setBusy(false)
    }
  }
  return { run, busy, error, setError }
}
