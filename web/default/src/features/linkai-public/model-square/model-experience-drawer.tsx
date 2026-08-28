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
import {
  Camera,
  Check,
  ChevronsUpDown,
  Copy,
  FileUp,
  Globe2,
  ImageUp,
  Paperclip,
  ScanLine,
  Send,
  SlidersHorizontal,
  Trash2,
  X,
} from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { formatPrice } from '@/features/pricing/lib/price'
import type { PricingModel } from '@/features/pricing/types'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'

type MenuName = 'attachments' | 'settings' | 'group' | null

type ModelExperienceDrawerProps = {
  open: boolean
  model: PricingModel
  priceRate: number
  usdExchangeRate: number
  onClose: () => void
}

const SETTINGS = [
  {
    id: 'temperature',
    label: 'Temperature',
    hint: 'Controls randomness and creativity in the output',
    value: 0.7,
    max: 2,
  },
  {
    id: 'top-p',
    label: 'Top P',
    hint: 'Limits the token selection range by probability mass',
    value: 1,
    max: 1,
  },
  {
    id: 'frequency',
    label: 'Frequency penalty',
    hint: 'Reduces repeated words and phrases',
    value: 1,
    max: 2,
  },
  {
    id: 'presence',
    label: 'Presence penalty',
    hint: 'Encourages discussion of new topics',
    value: 1,
    max: 2,
  },
] as const

export function ModelExperienceDrawer(props: ModelExperienceDrawerProps) {
  const { t } = useTranslation()
  const { open, onClose } = props
  const [view, setView] = useState<'chat' | 'code'>('chat')
  const [menu, setMenu] = useState<MenuName>(null)
  const [message, setMessage] = useState('')
  const [messages, setMessages] = useState<Array<{ id: number; text: string }>>(
    []
  )
  const [group, setGroup] = useState(
    props.model.enable_groups?.[0] || 'default'
  )
  const [copied, setCopied] = useState(false)
  const [settingValues, setSettingValues] = useState<Record<string, number>>(
    () =>
      Object.fromEntries(SETTINGS.map((setting) => [setting.id, setting.value]))
  )
  const [enabledSettings, setEnabledSettings] = useState<
    Record<string, boolean>
  >(() => Object.fromEntries(SETTINGS.map((setting) => [setting.id, true])))
  const composerRef = useRef<HTMLDivElement>(null)

  const iconKey = props.model.icon || props.model.vendor_icon
  const modelIcon = iconKey ? getLobeIcon(iconKey, 30) : null
  const groups = props.model.enable_groups?.length
    ? props.model.enable_groups
    : ['default']
  const inputPrice = formatPrice(
    props.model,
    'input',
    'M',
    false,
    props.priceRate,
    props.usdExchangeRate,
    group
  )
  const outputPrice = formatPrice(
    props.model,
    'output',
    'M',
    false,
    props.priceRate,
    props.usdExchangeRate,
    group
  )
  const code = JSON.stringify(
    {
      model: props.model.model_name,
      messages: [{ role: 'user', content: 'Hello' }],
      stream: true,
    },
    null,
    2
  )

  useEffect(() => {
    if (!open) return
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const onPointerDown = (event: PointerEvent) => {
      if (menu && !composerRef.current?.contains(event.target as Node)) {
        setMenu(null)
      }
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      if (menu) setMenu(null)
      else onClose()
    }
    document.addEventListener('pointerdown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.body.style.overflow = previousOverflow
      document.removeEventListener('pointerdown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [menu, onClose, open])

  if (!open) return null

  const sendMessage = () => {
    const value = message.trim()
    if (!value) return
    setMessages((current) => [...current, { id: Date.now(), text: value }])
    setMessage('')
  }

  const copyCode = async () => {
    await navigator.clipboard.writeText(code)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1600)
  }

  return (
    <div className='fixed inset-0 z-[90]'>
      <button
        type='button'
        className='absolute inset-0 cursor-default bg-black/5'
        aria-label={t('Close model experience')}
        onClick={props.onClose}
      />
      <aside
        className='absolute inset-y-0 right-0 flex w-full max-w-[1052px] flex-col overflow-hidden border-l border-white/10 bg-[#1a1a1a] text-[#eeeeee] shadow-[-35px_0_90px_rgba(0,0,0,.55)]'
        aria-label={t('Model Experience')}
      >
        <div className='no-scrollbar flex-1 overflow-y-auto px-5 pt-7 pb-44 sm:px-8'>
          <header className='flex items-center gap-3'>
            <div className='flex size-9 items-center justify-center'>
              {modelIcon || (
                <span className='text-xl font-semibold'>
                  {props.model.model_name.charAt(0).toUpperCase()}
                </span>
              )}
            </div>
            <h2 className='min-w-0 flex-1 truncate text-xl sm:text-2xl'>
              {props.model.model_name}
            </h2>
            <button
              type='button'
              onClick={props.onClose}
              className='flex size-10 items-center justify-center rounded-full text-white/60 transition hover:bg-white/10 hover:text-white'
              aria-label={t('Close')}
            >
              <X className='size-5' aria-hidden />
            </button>
          </header>

          <div className='mt-7 flex flex-wrap gap-3'>
            {groups.map((item) => (
              <button
                key={item}
                type='button'
                onClick={() => setGroup(item)}
                className={cn(
                  'rounded-xl border border-white/10 px-5 py-3 text-base text-white/55 transition hover:border-white/30 hover:text-white',
                  group === item && 'border-[#2e2e2e] bg-[#2c2c2c] text-white'
                )}
              >
                {t('{{group}} group', { group: item })}
              </button>
            ))}
          </div>

          <dl className='mt-5 grid overflow-hidden rounded-xl border border-white/10 bg-[#202020] sm:grid-cols-2'>
            <div className='px-5 py-5 text-center'>
              <dt className='text-sm text-white/45'>
                {t('Final input price')}
              </dt>
              <dd className='mt-2 text-xl'>{inputPrice} / M</dd>
            </div>
            <div className='border-t border-white/5 bg-[#242424] px-5 py-5 text-center sm:border-t-0 sm:border-l'>
              <dt className='text-sm text-white/45'>
                {t('Final output price')}
              </dt>
              <dd className='mt-2 text-xl'>{outputPrice} / M</dd>
            </div>
          </dl>

          <div className='relative mt-10 flex justify-center border-t border-white/10'>
            <div className='-mt-6 inline-flex rounded-full border border-white/10 bg-[#191919] p-1'>
              {(['chat', 'code'] as const).map((item) => (
                <button
                  key={item}
                  type='button'
                  onClick={() => setView(item)}
                  className={cn(
                    'rounded-full px-6 py-2 text-base text-white/45',
                    view === item && 'bg-[#2b2b2b] text-white'
                  )}
                >
                  {item === 'chat' ? t('Conversation View') : t('Code View')}
                </button>
              ))}
            </div>
          </div>

          {view === 'chat' ? (
            <div className='mt-9 space-y-4'>
              <div className='w-fit rounded-full bg-[#292929] px-5 py-3 text-base'>
                {t('Hello, I am {{model}}. You can start exploring now!', {
                  model: props.model.model_name,
                })}
              </div>
              {messages.map((item) => (
                <div
                  key={item.id}
                  className='ml-auto w-fit max-w-[80%] rounded-3xl bg-[#7357ff] px-5 py-3 text-base text-white'
                >
                  {item.text}
                </div>
              ))}
            </div>
          ) : (
            <div className='relative mt-9 overflow-hidden rounded-2xl border border-white/10 bg-[#101010]'>
              <div className='flex items-center justify-between border-b border-white/10 px-5 py-4'>
                <span className='text-sm text-white/55'>JSON</span>
                <button
                  type='button'
                  onClick={copyCode}
                  className='flex size-9 items-center justify-center rounded-lg text-white/60 transition hover:bg-white/10 hover:text-white'
                  aria-label={t('Copy code')}
                >
                  {copied ? (
                    <Check className='size-4' aria-hidden />
                  ) : (
                    <Copy className='size-4' aria-hidden />
                  )}
                </button>
              </div>
              <pre className='overflow-x-auto p-6 text-sm leading-7 text-white/80'>
                <code>{code}</code>
              </pre>
            </div>
          )}
        </div>

        <div
          className='absolute inset-x-5 bottom-5 sm:inset-x-8'
          ref={composerRef}
        >
          {menu === 'attachments' && (
            <div className='absolute bottom-[86px] left-3 w-44 rounded-xl border border-white/10 bg-[#2e2e2e] p-2 shadow-2xl'>
              <label className='flex w-full cursor-pointer items-center gap-3 rounded-lg px-3 py-2.5 text-left text-sm hover:bg-white/10'>
                <FileUp className='size-4' aria-hidden />
                {t('Upload file')}
                <input type='file' className='hidden' />
              </label>
              <label className='flex w-full cursor-pointer items-center gap-3 rounded-lg px-3 py-2.5 text-left text-sm hover:bg-white/10'>
                <ImageUp className='size-4' aria-hidden />
                {t('Upload photo')}
                <input type='file' accept='image/*' className='hidden' />
              </label>
              <button
                type='button'
                onClick={() => setMenu(null)}
                className='flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left text-sm hover:bg-white/10'
              >
                <ScanLine className='size-4' aria-hidden />
                {t('Screenshot')}
              </button>
              <button
                type='button'
                onClick={() => setMenu(null)}
                className='flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left text-sm hover:bg-white/10'
              >
                <Camera className='size-4' aria-hidden />
                {t('Take photo')}
              </button>
            </div>
          )}

          {menu === 'settings' && (
            <div className='absolute bottom-[86px] left-24 w-[min(408px,calc(100vw-72px))] rounded-xl border border-white/10 bg-[#343434] p-4 shadow-2xl'>
              <h3 className='font-medium'>{t('Parameter settings')}</h3>
              <p className='mt-1 text-xs text-white/45'>
                {t('Only enabled parameters are sent with the request.')}
              </p>
              <div className='mt-3 space-y-2'>
                {SETTINGS.map((setting) => (
                  <div key={setting.id} className='rounded-xl bg-[#292929] p-3'>
                    <div className='flex items-center gap-2'>
                      <strong className='text-sm'>{t(setting.label)}</strong>
                      <span className='rounded-full border border-white/10 px-2 py-0.5 text-xs text-white/60'>
                        {settingValues[setting.id]}
                      </span>
                      <button
                        type='button'
                        role='switch'
                        aria-checked={enabledSettings[setting.id]}
                        onClick={() =>
                          setEnabledSettings((current) => ({
                            ...current,
                            [setting.id]: !current[setting.id],
                          }))
                        }
                        className={cn(
                          'ml-auto h-5 w-9 rounded-full bg-white/20 p-0.5',
                          enabledSettings[setting.id] && 'bg-emerald-500'
                        )}
                      >
                        <span
                          className={cn(
                            'block size-4 rounded-full bg-white transition-transform',
                            enabledSettings[setting.id] && 'translate-x-4'
                          )}
                        />
                      </button>
                    </div>
                    <p className='mt-2 text-xs text-white/45'>
                      {t(setting.hint)}
                    </p>
                    <input
                      type='range'
                      min='0'
                      max={setting.max}
                      step='0.1'
                      value={settingValues[setting.id]}
                      onChange={(event) =>
                        setSettingValues((current) => ({
                          ...current,
                          [setting.id]: Number(event.target.value),
                        }))
                      }
                      className='mt-2 h-1 w-full accent-sky-500'
                      aria-label={t(setting.label)}
                    />
                  </div>
                ))}
                <div className='flex items-center gap-3 rounded-xl bg-[#292929] p-3 text-sm text-white/55'>
                  <span>{t('Max Tokens')}</span>
                  <span className='rounded-full border border-white/10 px-2 py-0.5 text-xs'>
                    4096
                  </span>
                </div>
              </div>
            </div>
          )}

          {menu === 'group' && (
            <div className='absolute right-36 bottom-[86px] w-56 rounded-xl border border-white/10 bg-[#202020] p-2 shadow-2xl'>
              {groups.map((item) => (
                <button
                  key={item}
                  type='button'
                  onClick={() => {
                    setGroup(item)
                    setMenu(null)
                  }}
                  className={cn(
                    'flex w-full items-center justify-between rounded-lg px-4 py-3 text-left hover:bg-white/10',
                    group === item && 'bg-[#303030]'
                  )}
                >
                  {item}
                  {group === item && <Check className='size-4' aria-hidden />}
                </button>
              ))}
            </div>
          )}

          <div className='rounded-3xl border-2 border-white/15 bg-[#101010] px-5 pt-5 pb-3 shadow-2xl'>
            <textarea
              value={message}
              onChange={(event) => setMessage(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && !event.shiftKey) {
                  event.preventDefault()
                  sendMessage()
                }
              }}
              placeholder={t(
                'Type a message. Enter to send / Shift+Enter for a new line'
              )}
              className='h-14 w-full resize-none bg-transparent text-base outline-none placeholder:text-white/40'
            />
            <div className='flex flex-wrap items-center gap-1.5'>
              <button
                type='button'
                onClick={() =>
                  setMenu(menu === 'attachments' ? null : 'attachments')
                }
                className='flex size-10 items-center justify-center rounded-xl text-white/45 hover:bg-white/10 hover:text-white'
                aria-label={t('Attachments')}
                aria-expanded={menu === 'attachments'}
              >
                <Paperclip className='size-5' aria-hidden />
              </button>
              <button
                type='button'
                className='flex size-10 items-center justify-center rounded-xl text-white/45 hover:bg-white/10 hover:text-white'
                aria-label={t('Web access')}
              >
                <Globe2 className='size-5' aria-hidden />
              </button>
              <button
                type='button'
                onClick={() => setMenu(menu === 'settings' ? null : 'settings')}
                className='flex size-10 items-center justify-center rounded-xl text-white/45 hover:bg-white/10 hover:text-white'
                aria-label={t('Parameter settings')}
                aria-expanded={menu === 'settings'}
              >
                <SlidersHorizontal className='size-5' aria-hidden />
              </button>
              <button
                type='button'
                onClick={() => setMessages([])}
                className='flex size-10 items-center justify-center rounded-xl text-white/45 hover:bg-white/10 hover:text-white'
                aria-label={t('Clear conversation')}
              >
                <Trash2 className='size-5' aria-hidden />
              </button>
              <button
                type='button'
                onClick={() => setMenu(menu === 'group' ? null : 'group')}
                className='ml-auto flex h-11 min-w-40 items-center justify-between gap-3 rounded-xl border border-white/10 px-4 text-left text-sm hover:bg-white/5'
                aria-expanded={menu === 'group'}
              >
                <span className='truncate'>{group}</span>
                <ChevronsUpDown className='size-4' aria-hidden />
              </button>
              <button
                type='button'
                onClick={sendMessage}
                className='flex h-11 items-center gap-2 rounded-full border border-white/10 px-5 text-white/80 transition hover:bg-white/10 hover:text-white'
              >
                <Send className='size-5' aria-hidden />
                {t('Send')}
              </button>
            </div>
          </div>
        </div>
      </aside>
    </div>
  )
}
