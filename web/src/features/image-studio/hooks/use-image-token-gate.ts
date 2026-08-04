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
import { useEffect, useState } from 'react'

import { useCreateImageToken, useImageTokenCapability } from '../queries'

export const useImageTokenGate = () => {
  const capabilityQuery = useImageTokenCapability()
  const createMutation = useCreateImageToken()
  const [dialogOpen, setDialogOpen] = useState(false)
  const capability = capabilityQuery.data

  useEffect(() => {
    if (capability?.status === 'missing' && capability.can_create) {
      setDialogOpen(true)
    }
    if (capability?.status === 'ready') setDialogOpen(false)
  }, [capability?.can_create, capability?.status])

  const createAndContinue = async (): Promise<void> => {
    try {
      const result = await createMutation.mutateAsync()
      if (result.status === 'ready') setDialogOpen(false)
    } catch {
      // The mutation exposes the error to the dialog without leaking a rejected event promise.
    }
  }

  return {
    capability,
    tokenId:
      capability?.status === 'ready' && capability.token
        ? capability.token.id
        : null,
    checking: !capability && capabilityQuery.isLoading,
    checkFailed: !capability && capabilityQuery.isError,
    refetch: capabilityQuery.refetch,
    dialogOpen,
    setDialogOpen,
    createAndContinue,
    creating: createMutation.isPending,
    createError: createMutation.error,
  }
}

export type ImageTokenGateState = ReturnType<typeof useImageTokenGate>
