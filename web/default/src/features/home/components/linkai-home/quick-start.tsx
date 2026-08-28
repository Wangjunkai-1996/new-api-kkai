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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import { LINKAI_HOME_ASSETS } from './assets'
import { LinkAiQuickStartApiDemo } from './quick-start-api-demo'

type ApiMode = 'enterprise' | 'personal'

export function LinkAiQuickStart() {
  const { t } = useTranslation()
  const [mode, setMode] = useState<ApiMode>('enterprise')
  const steps = [
    {
      title: t('linkaiHome.quick.stepOneTitle'),
      description: t('linkaiHome.quick.stepOneDescription'),
    },
    {
      title: t('linkaiHome.quick.stepTwoTitle'),
      description: t('linkaiHome.quick.stepTwoDescription'),
    },
    {
      title: t('linkaiHome.quick.stepThreeTitle'),
      description: t('linkaiHome.quick.stepThreeDescription'),
    },
  ]

  return (
    <section
      id='quick-start'
      className='px-5 py-24 sm:px-8 lg:px-[5vw] lg:pt-[170px] lg:pb-0'
    >
      <div className='mx-auto max-w-[1728px]'>
        <div className='text-center'>
          <span className='inline-flex items-center gap-3 rounded-full border border-white/35 bg-black px-8 py-4 text-lg text-white sm:text-xl'>
            <span className='size-2 rounded-full bg-white' />
            {t('linkaiHome.quick.eyebrow')}
          </span>
          <p className='mx-auto mt-[26px] max-w-[1000px] text-lg leading-[1.2] font-semibold text-white/90 sm:text-2xl lg:text-3xl'>
            {t('linkaiHome.quick.description')}
          </p>
          <h2 className='mt-6 text-[clamp(3.2rem,5.7vw,6.75rem)] leading-[1.2] font-bold tracking-[-0.055em] text-white'>
            {t('linkaiHome.quick.title')}
            <span className='mt-1 block'>
              {t('linkaiHome.shared.ourProducts')}
            </span>
          </h2>
        </div>

        <div className='mx-auto mt-16 grid gap-8 lg:mt-[50px] lg:max-w-[1569px] lg:grid-cols-[707fr_832fr] lg:gap-[30px]'>
          <div className='rounded-[2rem] border border-white/10 bg-[#0b0b0b] p-5 sm:p-8 lg:h-[652px]'>
            <ol className='space-y-5'>
              {steps.map((step, index) => (
                <li
                  key={step.title}
                  className='flex gap-4 rounded-3xl border border-white/10 bg-[#0f0f0f] p-5 sm:min-h-[129px] sm:gap-6 sm:p-7'
                >
                  <span className='flex size-12 shrink-0 items-center justify-center rounded-full bg-white text-lg font-bold text-black sm:size-14 sm:text-xl'>
                    {index + 1}
                  </span>
                  <div>
                    <h3 className='text-xl font-medium text-white sm:text-2xl'>
                      {step.title}
                    </h3>
                    <p className='mt-2 text-sm leading-relaxed text-white/50 sm:text-lg'>
                      {step.description}
                    </p>
                  </div>
                </li>
              ))}
            </ol>
            <div className='mt-5 grid grid-cols-2 gap-4'>
              {(['enterprise', 'personal'] as const).map((value) => (
                <button
                  key={value}
                  type='button'
                  aria-pressed={mode === value}
                  onClick={() => setMode(value)}
                  className={cn(
                    'relative flex min-h-[72px] items-center justify-between overflow-hidden rounded-2xl border border-white/15 px-5 text-left text-base font-semibold text-white transition sm:min-h-[98px] sm:text-2xl',
                    mode === value
                      ? 'ring-2 ring-white/80'
                      : 'opacity-75 hover:opacity-100'
                  )}
                >
                  <img
                    src={
                      value === 'enterprise'
                        ? LINKAI_HOME_ASSETS.heroEnterpriseButton
                        : LINKAI_HOME_ASSETS.heroPersonalButton
                    }
                    alt=''
                    className='absolute inset-0 h-full w-full object-cover'
                  />
                  <span className='relative z-10'>
                    {value === 'enterprise'
                      ? t('linkaiHome.quick.enterprise')
                      : t('linkaiHome.quick.personal')}
                  </span>
                  <img
                    src={LINKAI_HOME_ASSETS.arrowCircle}
                    alt=''
                    width={58}
                    height={58}
                    className='relative z-10 size-10 object-contain sm:size-[58px]'
                  />
                </button>
              ))}
            </div>
          </div>

          <LinkAiQuickStartApiDemo />
        </div>
      </div>
    </section>
  )
}
