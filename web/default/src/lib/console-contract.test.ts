import { describe, expect, it } from 'vitest'

import { parseConsoleContract, supportsConsole } from './console-contract'

describe('console API compatibility', () => {
  it('accepts compatible backends without a release ID, source SHA or schema', () => {
    const contract = parseConsoleContract({
      format_version: 1,
      api_contracts: [1, 2],
      capabilities: [],
    })
    expect(supportsConsole(contract)).toBe(true)
  })

  it.each([
    undefined,
    {},
    '<html>SPA fallback</html>',
    {
      format_version: 1,
      api_contracts: ['1'],
      capabilities: [],
    },
    { format_version: 1, api_contracts: [1], capabilities: [true] },
  ])('fails closed for missing or malformed declarations', (value) => {
    expect(supportsConsole(parseConsoleContract(value))).toBe(false)
  })

  it('does not infer backward compatibility from a newer API number', () => {
    expect(
      supportsConsole(
        parseConsoleContract({
          format_version: 1,
          api_contracts: [2],
          capabilities: [],
        })
      )
    ).toBe(false)
  })
})
