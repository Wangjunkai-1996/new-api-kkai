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
const PIXEL_SIZE_TOKEN =
  /(?<![a-z\d_./+%-]|[x×*]\s*)([+-]?\d+(?:\.\d*)?\s*[x×*]\s*[+-]?\d+(?:\.\d*)?(?:\s*[x×*]\s*[+-]?\d+(?:\.\d*)?)*)(?:\s*(?:px|pixels?|像素))?(?![a-z\d_./+%-]|\s*[x×*])/giu

/**
 * Recognize complete pixel dimensions in prose, with or without a size command.
 * Quoted/reference examples are ignored; model capability checks belong to the caller.
 * ponytail: context exclusions are conservative; this does not interpret arbitrary prose.
 */
export function parseImagePromptSize(prompt: string): ImagePromptSizeResult {
  const unquoted = prompt
    .replaceAll(
      /```[\s\S]*?(?:```|$)|`[^`\n]*(?:`|$)|"(?:\\.|[^"\\])*(?:"|$)|(?<![\p{L}\p{N}])'(?:\\.|[^'\\])*(?:'|$)|“[^”]*(?:”|$)|‘[^’]*(?:’|$)|「[^」]*(?:」|$)|『[^』]*(?:』|$)/gu,
      ' '
    )
    .replaceAll(/^[\t ]*>[^\r\n]*/gm, '')
    .replaceAll(
      /\b(?:[a-z][a-z\d+.-]*:\/\/|www\.|[a-z\d.-]+\.[a-z]{2,}[/?#]|[a-z][\w.-]*\/)[^\s<>"'，。；]*/giu,
      ' '
    )
  const sizes = new Set<string>()
  let previousDirective = false
  let ignoreFollowingCommands = false

  // A period is a boundary only when it cannot be part of a decimal dimension.
  const clauses = unquoted.split(/[,，;；。!！?？\r\n]|\.(?=\s|$)/u)
  for (const clause of clauses) {
    if (clause.trim() === '') continue
    if (
      /^\s*(?:示例|例如|比如|参考图|引用|原图|不是|并非|不要(?:使用|设置)?|请勿(?:使用|设置)?|(?:for\s+)?examples?|reference(?:\s+image)?|do\s+not(?:\s+(?:use|set))?|don['’]t(?:\s+(?:use|set))?)\s*[:：]\s*$/iu.test(
        clause
      )
    ) {
      ignoreFollowingCommands = true
      previousDirective = false
      continue
    }
    const command = clause.match(OUTPUT_SIZE_COMMAND)
    const flags = [...clause.matchAll(/(?:^|\s)--size(?=\s|=|$)/giu)]
    const pixelSizes = [...clause.matchAll(PIXEL_SIZE_TOKEN)]
    const continuation: boolean =
      previousDirective &&
      /^\s*(?:(?:or|and)\b|或者?|和)?\s*[+-]?\d+(?:\.\d*)?\s*(?:[x×*]|$)/iu.test(
        clause
      )
    previousDirective = Boolean(
      command || flags.length > 0 || continuation || pixelSizes.length > 0
    )
    // A colon header scopes the adjacent command block; ordinary prose ends it.
    if (ignoreFollowingCommands && previousDirective) {
      previousDirective = false
      continue
    }
    ignoreFollowingCommands = false
    if (!previousDirective) continue

    const prefix = clause.slice(0, pixelSizes[0]?.index ?? flags[0]?.index)
    // Negation must target the dimensions; e.g. "without text" still permits a size.
    if (
      /示例|例如|比如|参考图|参考尺寸|引用|原图|\b(?:example|reference)\b/iu.test(
        prefix
      ) ||
      /(?:不要|不是|并非|无需|不需要|请勿|勿|禁止|别(?=\s*(?:使用|用|采用|设置|输出))|(?:^|\s)别)\s*(?:使用|用|采用|设置|输出)?\s*(?:(?:输出)?(?:尺寸|分辨率)\s*(?:为|是)?\s*[:：=]?)?\s*(?:--size\s*=?\s*)?$|\b(?:no|not|don't|don’t|never|avoid|without)\s*(?:use|using|set|output)?\s*(?:(?:output\s+)?(?:size|resolution)\s*[:：=]?)?\s*(?:--size\s*=?\s*)?$/iu.test(
        prefix
      )
    ) {
      previousDirective = false
      continue
    }

    const bodies = pixelSizes.map((match) => match[1] ?? '')
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
        /\s*(?:或者|或|和|以及|\bor\b|\band\b|\/|\|)\s*(?=(?:请\s*)?输出(?:尺寸|分辨率)|(?:尺寸|分辨率)\s*[:：]|(?:please\s+)?output\s+(?:size|resolution)|[+-]?\d|$)/iu
      )
      for (const [index, alternative] of alternatives.entries()) {
        const value =
          index > 0 ? alternative.replace(OUTPUT_SIZE_COMMAND, '') : alternative
        const matches = [...value.matchAll(PIXEL_SIZE_TOKEN)]
        const remainder = value.replaceAll(PIXEL_SIZE_TOKEN, '').trim()
        if (matches.length === 0 || /^[+-]?\d+(?:\.\d*)?$/u.test(remainder)) {
          return { kind: 'invalid' }
        }
        for (const match of matches) {
          const dimensions = match[1]?.match(/^(\d+)\s*[x×*]\s*(\d+)$/iu)
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
  }

  if (sizes.size > 1) return { kind: 'conflict' }
  const size = sizes.values().next().value
  return size === undefined ? { kind: 'none' } : { kind: 'size', size }
}
