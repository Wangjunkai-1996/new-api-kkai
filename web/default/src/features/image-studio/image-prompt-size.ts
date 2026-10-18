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
export type ImagePromptSizeResult =
  | { kind: 'none' }
  | { kind: 'size'; size: string }
  | { kind: 'invalid' }
  | { kind: 'conflict' }

const OUTPUT_SIZE_COMMAND =
  /^\s*(?:(?:或者?|以及|并且|和)\s*|(?:or|and)\s+)?(?:(?:请\s*)?输出(?:尺寸|分辨率)\s*(?:为|是|[:：=])?|(?:尺寸|分辨率)\s*[:：]|(?:please\s+)?output\s+(?:size|resolution)\b\s*[:：=]?)/iu

/**
 * Recognize explicit output-size clauses and inline --size flags only.
 * Quoted examples are ignored; model capability checks belong to the caller.
 * ponytail: this is a command grammar, not natural-language interpretation.
 */
export function parseImagePromptSize(prompt: string): ImagePromptSizeResult {
  const unquoted = prompt
    .replaceAll(
      /```[\s\S]*?(?:```|$)|`[^`\n]*(?:`|$)|"(?:\\.|[^"\\])*(?:"|$)|(?<![\p{L}\p{N}])'(?:\\.|[^'\\])*(?:'|$)|“[^”]*(?:”|$)|‘[^’]*(?:’|$)|「[^」]*(?:」|$)|『[^』]*(?:』|$)/gu,
      ' '
    )
    .replaceAll(/^[\t ]*>[^\r\n]*/gm, '')
  const sizes = new Set<string>()
  let previousDirective = false
  let ignoreFollowingCommands = false

  // A period is a boundary only when it cannot be part of a decimal dimension.
  const clauses = unquoted.split(/[,，;；。!！?？\r\n]|\.(?!\d)/u)
  for (const clause of clauses) {
    if (clause.trim() === '') continue
    if (
      /^\s*(?:示例|例如|比如|不要(?:使用|设置)?|请勿(?:使用|设置)?|(?:for\s+)?examples?|do\s+not(?:\s+(?:use|set))?|don['’]t(?:\s+(?:use|set))?)\s*[:：]\s*$/iu.test(
        clause
      )
    ) {
      ignoreFollowingCommands = true
      previousDirective = false
      continue
    }
    const command = clause.match(OUTPUT_SIZE_COMMAND)
    const flags = [...clause.matchAll(/(?:^|\s)--size(?=\s|=|$)/giu)]
    const continuation: boolean =
      previousDirective &&
      /^\s*(?:(?:or|and)\b|或者?|和)?\s*[+-]?\d+(?:\.\d*)?\s*(?:[x×*]|$)/iu.test(
        clause
      )
    previousDirective = Boolean(command || flags.length > 0 || continuation)
    // A colon header scopes the adjacent command block; ordinary prose ends it.
    if (ignoreFollowingCommands && previousDirective) {
      previousDirective = false
      continue
    }
    ignoreFollowingCommands = false
    if (!previousDirective) continue

    // For inline flags the preceding prose can negate or quote their purpose.
    if (
      flags.length > 0 &&
      !command &&
      /不要|无需|不需要|勿|禁止|别(?:\s|用|使用|设置|输出|采用)|示例|例如|比如|\b(?:no|not|don't|don’t|never|avoid|without|example)\b/iu.test(
        clause.slice(0, flags[0]?.index)
      )
    ) {
      previousDirective = false
      continue
    }

    const bodies: string[] = []
    if (continuation) {
      bodies.push(clause.replace(/^\s*(?:(?:or|and)\b|或者?|和)\s*/iu, ''))
    }
    if (command) {
      bodies.push(clause.slice(command[0].length, flags[0]?.index))
    }
    for (let index = 0; index < flags.length; index++) {
      const flag = flags[index]
      if (!flag) continue
      bodies.push(
        clause
          .slice((flag.index ?? 0) + flag[0].length, flags[index + 1]?.index)
          .replace(/^\s*=\s*/u, '')
      )
    }

    for (const body of bodies) {
      const alternatives = body.split(
        /\s*(?:或者|或|和|以及|\bor\b|\band\b|\/|\|)\s*/iu
      )
      for (const [index, alternative] of alternatives.entries()) {
        const value =
          index > 0 ? alternative.replace(OUTPUT_SIZE_COMMAND, '') : alternative
        const dimensions = value.match(
          /^\s*(\d+)\s*[x×*]\s*(\d+)\s*(?:px|pixels?|像素)?\s*$/iu
        )
        if (!dimensions) return { kind: 'invalid' }
        const width = Number(dimensions[1])
        const height = Number(dimensions[2])
        if (!Number.isSafeInteger(width) || !Number.isSafeInteger(height)) {
          return { kind: 'invalid' }
        }
        sizes.add(`${width}x${height}`)
      }
    }
  }

  if (sizes.size > 1) return { kind: 'conflict' }
  const size = sizes.values().next().value
  return size === undefined ? { kind: 'none' } : { kind: 'size', size }
}
