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
import { CopyIcon } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { LINKAI_HOME_ASSETS } from './assets'
import {
  API_DEMOS,
  buildApiDemoRequest,
  buildApiDemoResponse,
} from './quick-start-api-demos'

export function LinkAiQuickStartApiDemo() {
  const { t } = useTranslation()
  const [activeIndex, setActiveIndex] = useState(0)
  const demo = API_DEMOS[activeIndex]
  const request = buildApiDemoRequest(demo)
  const response = buildApiDemoResponse(demo)

  const copyRequest = async () => {
    const copyWithTextarea = () => {
      const textarea = document.createElement('textarea')
      textarea.value = request
      textarea.style.position = 'fixed'
      textarea.style.opacity = '0'
      document.body.appendChild(textarea)
      textarea.select()
      const copied = document.execCommand('copy')
      textarea.remove()
      if (!copied) throw new Error('Copy command was rejected')
    }

    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(request)
      } else {
        copyWithTextarea()
      }
      toast.success(t('linkaiHome.quick.copied'))
    } catch {
      try {
        copyWithTextarea()
        toast.success(t('linkaiHome.quick.copied'))
      } catch {
        toast.error(t('linkaiHome.quick.copyFailed'))
      }
    }
  }

  return (
    <div className='relative flex flex-col overflow-hidden rounded-[2rem] border border-white/10 bg-[#0c0c0c] lg:h-[652px]'>
      <img
        src={LINKAI_HOME_ASSETS.quickCodeGlow}
        alt=''
        width={785}
        height={697}
        className='pointer-events-none absolute right-[-22%] bottom-[-7%] h-[107%] w-[94%] object-cover opacity-70'
      />
      <div className='relative z-10 flex shrink-0 items-center justify-between gap-4 border-b border-white/10 px-5 sm:px-8'>
        <div
          role='tablist'
          aria-label={t('linkaiHome.quick.title')}
          className='flex min-w-0 flex-1 overflow-x-auto'
        >
          {API_DEMOS.map((item, index) => (
            <button
              key={item.id}
              id={`quick-start-tab-${item.id}`}
              type='button'
              role='tab'
              aria-selected={activeIndex === index}
              aria-controls='quick-start-api-panel'
              onClick={() => setActiveIndex(index)}
              className={`shrink-0 border-b-2 px-3 py-4 text-sm transition sm:px-4 sm:text-base ${
                activeIndex === index
                  ? 'border-white text-white'
                  : 'border-transparent text-white/40 hover:text-white/75'
              }`}
            >
              {item.label}
            </button>
          ))}
        </div>
        <span className='flex shrink-0 items-center gap-2 text-xs text-white/40'>
          <span className='size-2 rounded-full bg-emerald-400' /> 200 OK
        </span>
      </div>
      <div className='relative z-10 flex shrink-0 items-center gap-3 border-b border-white/10 px-5 py-4 sm:px-8'>
        <span className='rounded-full bg-emerald-950 px-3 py-1 text-xs font-bold text-emerald-400'>
          {demo.method}
        </span>
        <code className='min-w-0 flex-1 truncate text-xs text-white sm:text-sm'>
          {demo.endpoint}
        </code>
        <button
          type='button'
          onClick={copyRequest}
          className='flex size-10 shrink-0 items-center justify-center rounded-xl bg-transparent text-white/70 transition hover:bg-white/10 hover:text-white focus-visible:outline-2 focus-visible:outline-white'
          aria-label={t('linkaiHome.quick.copy')}
        >
          <CopyIcon
            aria-hidden='true'
            className='size-[22px]'
            strokeWidth={1.8}
          />
        </button>
      </div>
      <div
        id='quick-start-api-panel'
        role='tabpanel'
        aria-labelledby={`quick-start-tab-${demo.id}`}
        className='relative z-10 min-h-0 flex-1 overflow-y-auto p-5 sm:p-6'
      >
        <p className='text-xs font-bold tracking-[0.18em] text-white/30'>
          {t('Request')}
        </p>
        <pre className='mt-4 overflow-x-auto text-xs leading-6 whitespace-pre-wrap text-white sm:text-sm'>
          <code>{request}</code>
        </pre>
        <div className='my-5 h-px bg-white/10' />
        <p className='text-xs font-bold tracking-[0.18em] text-white/30'>
          {t('Response')}
        </p>
        <pre className='mt-4 overflow-x-auto text-xs leading-6 whitespace-pre-wrap text-white sm:text-sm'>
          <code>{response}</code>
        </pre>
      </div>
      <div className='relative z-10 flex shrink-0 flex-wrap gap-x-8 gap-y-2 border-t border-white/10 px-5 py-4 text-xs text-white/65 sm:px-8'>
        <span>{demo.latency} MS</span>
        <span>{demo.tokens} TOKENS</span>
        <span>COST ${(demo.tokens * 0.00003).toFixed(5)}</span>
        <span className='ml-auto'>STREAM · SSE</span>
      </div>
    </div>
  )
}
