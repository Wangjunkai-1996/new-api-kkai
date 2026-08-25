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
import assert from 'node:assert/strict'

import { test } from 'vitest'

import { videoTokenCapabilitySchema } from './schemas'
import {
  getVideoTokenGateAction,
  getVideoTokenErrorKind,
  getVideoTokenRequestFailureAccess,
  getVideoTokenScopeAccess,
  getVideoTokenTerminalAccess,
  releaseVideoTokenScopeBlocker,
  rememberVideoTokenAutoPrompt,
  resolveVideoTokenAccess,
  shouldAutoPromptVideoToken,
} from './video-token-access'

const requiredGroup = 'Seedance 视频'

test('uses only a positive token id from the required video group', () => {
  const capability = videoTokenCapabilitySchema.parse({
    required_group: requiredGroup,
    has_usable_token: true,
    can_create: true,
    status: 'ready',
    reason: 'must-not-reach-the-ui-state',
    token: {
      id: 17,
      name: '视频工作室',
      group: requiredGroup,
      key: 'sk-must-not-reach-the-ui-state',
    },
  })

  assert.deepEqual(capability, {
    required_group: requiredGroup,
    has_usable_token: true,
    can_create: true,
    status: 'ready',
    token: {
      id: 17,
      name: '视频工作室',
      group: requiredGroup,
    },
  })
  assert.deepEqual(resolveVideoTokenAccess(capability), {
    kind: 'ready',
    requiredGroup,
    tokenId: 17,
    tokenName: '视频工作室',
  })
})

test('asks for confirmation only when the account may create the video key', () => {
  assert.deepEqual(
    resolveVideoTokenAccess({
      required_group: requiredGroup,
      has_usable_token: false,
      can_create: true,
      token: null,
      status: 'missing',
    }),
    { kind: 'missing', requiredGroup }
  )
})

test('maps every non-creatable capability status without conflating permissions', () => {
  assert.deepEqual(
    resolveVideoTokenAccess({
      required_group: requiredGroup,
      has_usable_token: false,
      can_create: false,
      token: null,
      status: 'group_unavailable',
    }),
    { kind: 'group-unavailable', requiredGroup }
  )
  assert.deepEqual(
    resolveVideoTokenAccess({
      required_group: requiredGroup,
      has_usable_token: false,
      can_create: false,
      token: null,
      status: 'limit_reached',
    }),
    { kind: 'limit-reached', requiredGroup }
  )
  assert.deepEqual(
    resolveVideoTokenAccess({
      required_group: requiredGroup,
      has_usable_token: false,
      can_create: false,
      token: null,
      status: 'models_unavailable',
    }),
    { kind: 'models-unavailable', requiredGroup }
  )
})

test('treats status as authoritative and only allows confirmed missing creation', () => {
  assert.deepEqual(
    resolveVideoTokenAccess({
      required_group: requiredGroup,
      has_usable_token: false,
      can_create: false,
      token: { id: 19, name: '视频工作室', group: requiredGroup },
      status: 'ready',
    }),
    {
      kind: 'ready',
      requiredGroup,
      tokenId: 19,
      tokenName: '视频工作室',
    }
  )
  assert.deepEqual(
    resolveVideoTokenAccess({
      required_group: requiredGroup,
      has_usable_token: false,
      can_create: false,
      token: null,
      status: 'missing',
    }),
    { kind: 'invalid', requiredGroup }
  )
  assert.deepEqual(
    resolveVideoTokenAccess({
      required_group: requiredGroup,
      has_usable_token: false,
      can_create: true,
      token: null,
      status: 'models_unavailable',
    }),
    { kind: 'models-unavailable', requiredGroup }
  )
})

test('fails closed for malformed or wrong-group token capabilities', () => {
  assert.deepEqual(
    resolveVideoTokenAccess({
      required_group: requiredGroup,
      has_usable_token: true,
      can_create: true,
      token: { id: 0, name: 'broken', group: requiredGroup },
      status: 'ready',
    }),
    { kind: 'invalid', requiredGroup }
  )
  assert.deepEqual(
    resolveVideoTokenAccess({
      required_group: requiredGroup,
      has_usable_token: true,
      can_create: true,
      token: { id: 18, name: 'wrong group', group: 'default' },
      status: 'ready',
    }),
    { kind: 'invalid', requiredGroup }
  )
})

test('remembers an automatic prompt for the bound key scope', () => {
  const promptedScopes = new Set<string>()
  const missingAccess = {
    kind: 'missing' as const,
    requiredGroup,
  }

  assert.equal(
    shouldAutoPromptVideoToken(promptedScopes, 11, missingAccess),
    true
  )
  rememberVideoTokenAutoPrompt(promptedScopes, 11, requiredGroup)

  // A model switch and return resolve to the same user/group prompt scope.
  assert.equal(
    shouldAutoPromptVideoToken(promptedScopes, 11, missingAccess),
    false
  )
  assert.equal(
    shouldAutoPromptVideoToken(promptedScopes, 11, missingAccess),
    false
  )
})

test('keeps automatic prompt memory independent across accounts', () => {
  const promptedScopes = new Set<string>()
  const missingAccess = {
    kind: 'missing' as const,
    requiredGroup,
  }

  rememberVideoTokenAutoPrompt(promptedScopes, 11, requiredGroup)

  assert.equal(
    shouldAutoPromptVideoToken(promptedScopes, 11, missingAccess),
    false
  )
  assert.equal(
    shouldAutoPromptVideoToken(promptedScopes, 22, missingAccess),
    true
  )
})

test('keeps group permission failures distinct from API and network failures', () => {
  assert.equal(
    getVideoTokenErrorKind('video_token_group_unavailable'),
    'group-unavailable'
  )
  assert.equal(
    getVideoTokenErrorKind('video_token_limit_reached'),
    'limit-reached'
  )
  assert.equal(
    getVideoTokenErrorKind('video_token_ip_forbidden'),
    'token-invalid'
  )
  assert.equal(getVideoTokenErrorKind(undefined), 'request-failed')
})

test('turns terminal create races into recheck-only states', () => {
  const missingAccess = { kind: 'missing' as const, requiredGroup }
  assert.equal(getVideoTokenGateAction(missingAccess, false), 'create')

  for (const errorKind of [
    'group-unavailable',
    'limit-reached',
    'models-unavailable',
  ] as const) {
    const terminalAccess = getVideoTokenTerminalAccess(errorKind, requiredGroup)
    assert.ok(terminalAccess)
    assert.equal(getVideoTokenGateAction(terminalAccess, false), 'recheck')
  }

  assert.equal(
    getVideoTokenTerminalAccess('request-failed', requiredGroup),
    null
  )
})

test('keeps a token request blocker over stale ready data until a check succeeds', () => {
  const readyAccess = {
    kind: 'ready' as const,
    requiredGroup,
    tokenId: 17,
    tokenName: '视频工作室',
  }
  const blockedAccess = getVideoTokenRequestFailureAccess(
    'token-invalid',
    requiredGroup
  )
  assert.ok(blockedAccess)
  const blocker = {
    userId: 11,
    access: blockedAccess,
  }

  assert.deepEqual(getVideoTokenScopeAccess(readyAccess, blocker, 11), {
    kind: 'invalid',
    requiredGroup,
  })
  const afterFailedCheck = releaseVideoTokenScopeBlocker(blocker, 11, false)
  assert.equal(afterFailedCheck, blocker)
  assert.deepEqual(
    getVideoTokenScopeAccess(readyAccess, afterFailedCheck, 11),
    { kind: 'invalid', requiredGroup }
  )

  const afterSuccessfulCheck = releaseVideoTokenScopeBlocker(blocker, 11, true)
  assert.equal(afterSuccessfulCheck, null)
  assert.equal(
    getVideoTokenScopeAccess(readyAccess, afterSuccessfulCheck, 11),
    readyAccess
  )
  assert.equal(getVideoTokenScopeAccess(readyAccess, blocker, 22), readyAccess)
})

test('maps quote and submission token failures to scope blockers', () => {
  assert.deepEqual(
    getVideoTokenRequestFailureAccess('token-invalid', requiredGroup),
    { kind: 'invalid', requiredGroup }
  )
  assert.deepEqual(
    getVideoTokenRequestFailureAccess('group-unavailable', requiredGroup),
    { kind: 'group-unavailable', requiredGroup }
  )
  assert.equal(
    getVideoTokenRequestFailureAccess('request-failed', requiredGroup),
    null
  )
})
