import requirements from '../../console-contract.json'

export interface ConsoleContract {
  format_version: 1
  api_contracts: number[]
  capabilities: string[]
}

export function parseConsoleContract(value: unknown): ConsoleContract | null {
  if (!value || typeof value !== 'object') return null
  const record = value as Record<string, unknown>
  if (
    record.format_version !== 1 ||
    !Array.isArray(record.api_contracts) ||
    record.api_contracts.length === 0 ||
    !record.api_contracts.every((item) => Number.isInteger(item) && item > 0) ||
    !Array.isArray(record.capabilities) ||
    !record.capabilities.every(
      (item) => typeof item === 'string' && /^[a-z][a-z0-9_]{0,63}$/.test(item)
    )
  ) {
    return null
  }
  return record as unknown as ConsoleContract
}

export function supportsConsole(contract: ConsoleContract | null): boolean {
  return Boolean(
    contract?.api_contracts.includes(requirements.api_contract) &&
    requirements.required_capabilities.every((name) =>
      contract.capabilities.includes(name)
    )
  )
}
