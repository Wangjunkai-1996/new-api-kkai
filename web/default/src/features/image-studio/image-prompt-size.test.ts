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
import { describe, expect, test } from 'vitest'

import { parseImagePromptSize } from './image-prompt-size'

describe('image prompt sizes', () => {
  test.each([
    '2048×1536',
    '2048x1536',
    '2048 X 1536',
    '2048*1536',
    '02048 × 01536',
    '画一张2048×1536的咖啡海报',
    'a 2048x1536 poster',
    '画一张2048x1536海报，颜色温暖',
    'a 2048x1536 black and white poster',
    'a poster without text 2048x1536',
    '不要文字的2048×1536海报',
    '输出尺寸：2048x1536的海报',
    'output size: 2048x1536 black and white poster',
    '画一张咖啡海报 --size 2048x1536 with warm colors',
    '生成一张咖啡店海报，输出尺寸：2048×1536',
    '输出分辨率 2048x1536',
    '请输出尺寸为 2048 X 1536 像素。暖色调',
    '尺寸：02048 * 01536',
    '海报\n分辨率：2048x1536px',
    'A poster; output size: 2048 x 1536 pixels.',
    'Please output resolution = 2048x1536',
    'Draw a cat --size 2048x1536',
    '特别明亮的海报 --size 2048x1536',
    '--size=2048x1536',
    '输出尺寸：2048x1536；输出分辨率：02048×01536',
    '--size 2048x1536 --size 2048x1536',
    '输出尺寸：2048x1536 或 2048x1536',
    '写上“输出尺寸：1024x1024”，输出尺寸：2048x1536',
    '2048x1536 的海报，参考图尺寸：1024x1024',
    '参考图尺寸：1024x1024，画一张2048×1536的海报',
    '参考 https://example.com/1024x1024.png，画一张2048x1536海报',
    '2048x1536 的海报，2048×1536',
    '2048x1536，不是1024x1024',
  ])('normalizes an unambiguous directive: %s', (prompt) => {
    expect(parseImagePromptSize(prompt)).toEqual({
      kind: 'size',
      size: '2048x1536',
    })
  })

  test.each([
    '',
    '参考图尺寸：1024x1024；制作高清海报，16:9，4K',
    'Reference image: 1024x1024',
    '参考图：\n1024x1024',
    '不要使用2048x1536',
    '不是1024x1024',
    '并非1024x1024',
    'Not 2048x1536',
    '例如 2048x1536',
    '例如：\n2048x1536',
    '“2048x1536”',
    '`2048x1536`',
    '> 2048x1536',
    'abc2048x1536',
    '2048x1536abc',
    'abc2048 x 1536 x 3',
    'image_2048x1536',
    '2048x1536.png',
    'https://example.com/2048x1536.png',
    'https://example.com/image?size=2048x1536',
    'www.example.com/2048x1536',
    'example.com/image?size=2048x1536',
    'assets/image.png?size=2048x1536',
    '不要输出尺寸：2048x1536',
    '不要使用 --size 2048x1536',
    'Do not use --size 2048x1536',
    'No --size 2048x1536',
    '别使用 --size 2048x1536',
    'This is an example --size 2048x1536',
    '例如：输出尺寸：2048x1536',
    'Print "output size: 2048x1536" on the sign',
    "Print 'output size: 2048x1536' on the sign",
    '请写上“输出尺寸：2048x1536”',
    '文本「尺寸：2048x1536」',
    'Explain `--size 2048x1536`',
    '```\n输出尺寸：2048x1536\n```',
    '引用：“输出尺寸：2048x1536',
    '> --size 2048x1536',
    '> output size: 2048x1536',
    '示例：\n输出尺寸：2048x1536',
    '例如：\n\n--size 2048x1536\n输出尺寸：1024x1024',
    'Do not use:\noutput size: 2048x1536',
    "Don't use:\n--size 2048x1536",
  ])(
    'ignores references, quoted examples, negation and non-size tokens: %s',
    (prompt) => {
      expect(parseImagePromptSize(prompt)).toEqual({ kind: 'none' })
    }
  )

  test.each([
    '2048.5x1536',
    '2048x1536.5',
    '2048x1536x3',
    '2048 x 1536 x 3',
    '画一张2048.5x1536的海报',
    '画一张2048x1536x3的海报',
    '9007199254740992x1536',
    '输出尺寸：',
    '输出尺寸：2048x',
    'output size: 4K',
    '分辨率：16:9',
    '--size',
    '输出尺寸：2048.5x1536',
    '输出尺寸：2048x1536.5',
    '输出尺寸：2048x1536x3',
    '输出尺寸：2048x1536 1024',
    '输出尺寸：2048x1536，1024',
    '输出尺寸：2048x1536 或 1024',
    '输出尺寸：-2048x1536',
    '输出尺寸：2048x1536abc',
    '输出尺寸：9007199254740992x1536',
    '输出尺寸：2048x1536；输出尺寸：',
  ])(
    'blocks malformed dimensions or incomplete explicit directives: %s',
    (prompt) => {
      expect(parseImagePromptSize(prompt)).toEqual({ kind: 'invalid' })
    }
  )

  test.each([
    '2048x1536 或 1024x1024',
    '尺寸分别2048x1536和1024x1024',
    '画一张2048x1536的海报，另外一张1024x1024',
    '2048x1536 poster --size 1024x1024',
    '输出尺寸：2048x1536 1024x1024',
    '输出尺寸：1024x1024；输出尺寸：2048x1536',
    '输出尺寸：1024x1024 或 2048x1536',
    'output size: 1024x1024 or 2048x1536',
    '输出尺寸：1024x1024，2048x1536',
    '输出尺寸：1024x1024，或者 2048x1536',
    '输出尺寸：1024x1024，或者输出尺寸：2048x1536',
    '输出尺寸：1024x1024 或者输出尺寸：2048x1536',
    'output size: 1024x1024, or output size: 2048x1536',
    '--size 1024x1024 --size 2048x1536',
    '尺寸：1024x1024 --size 2048x1536',
  ])('rejects competing dimensions: %s', (prompt) => {
    expect(parseImagePromptSize(prompt)).toEqual({ kind: 'conflict' })
  })

  test('leaves model-specific limits to the shared parameter validator', () => {
    expect(parseImagePromptSize('输出尺寸：4096x2048')).toEqual({
      kind: 'size',
      size: '4096x2048',
    })
    expect(parseImagePromptSize('画一张4096x2048的海报')).toEqual({
      kind: 'size',
      size: '4096x2048',
    })
    expect(parseImagePromptSize('2049x1536')).toEqual({
      kind: 'size',
      size: '2049x1536',
    })
  })

  test('recognizes dimensions in ordinary subject descriptions without requiring output terminology', () => {
    expect(parseImagePromptSize('画一台分辨率为1920×1080的显示器')).toEqual({
      kind: 'size',
      size: '1920x1080',
    })
    expect(parseImagePromptSize('显示器的输出尺寸：1920x1080')).toEqual({
      kind: 'size',
      size: '1920x1080',
    })
  })

  test('resumes output commands after ordinary prose ends an example block', () => {
    expect(
      parseImagePromptSize(
        '示例：\n输出尺寸：1024x1024\n画一张海报，输出尺寸：2048x1536'
      )
    ).toEqual({ kind: 'size', size: '2048x1536' })
    expect(
      parseImagePromptSize('> --size 1024x1024\noutput size: 2048x1536')
    ).toEqual({ kind: 'size', size: '2048x1536' })
  })
})
