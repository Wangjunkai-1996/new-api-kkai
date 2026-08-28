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
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { LINKAI_HOME_ASSETS } from './assets'

type HeroProps = {
  isAuthenticated: boolean
}

export function LinkAiHero(props: HeroProps) {
  const { t } = useTranslation()
  const stats = [
    {
      value: '300+',
      label: t('linkaiHome.stats.models.label'),
      description: t('linkaiHome.stats.models.description'),
    },
    {
      value: '500+',
      label: t('linkaiHome.stats.businesses.label'),
      description: t('linkaiHome.stats.businesses.description'),
    },
    {
      value: '99.9%',
      label: t('linkaiHome.stats.delivery.label'),
      description: t('linkaiHome.stats.delivery.description'),
    },
    {
      value: '0',
      suffix: 'leave',
      label: t('linkaiHome.stats.security.label'),
      description: t('linkaiHome.stats.security.description'),
    },
  ]

  return (
    <section className='relative overflow-hidden px-5 pt-36 sm:px-8 sm:pt-44 lg:min-h-[2024px] lg:px-[5vw] lg:pt-[295px]'>
      <div
        aria-hidden='true'
        className='pointer-events-none absolute inset-x-0 top-[520px] mx-auto h-[1142px] max-w-[1938px] lg:top-[713px]'
      >
        <img
          src={LINKAI_HOME_ASSETS.heroPlanetGlow}
          alt=''
          width={1938}
          height={1142}
          className='h-full w-full object-cover object-top'
        />
        <img
          src={LINKAI_HOME_ASSETS.heroPlanet}
          alt=''
          width={1505}
          height={522}
          className='absolute top-[8%] left-1/2 w-[78%] max-w-[1505px] -translate-x-1/2 object-contain'
        />
      </div>

      <div className='relative z-10 mx-auto max-w-[1728px] text-center'>
        <h1 className='mx-auto max-w-[1096px] text-[clamp(2.7rem,5.2vw,5.625rem)] leading-[1.2] font-medium tracking-[-0.045em] text-white'>
          {t('linkaiHome.hero.title')}
        </h1>
        <p className='mx-auto mt-[37px] max-w-[1217px] text-[clamp(1.1rem,2.45vw,3rem)] leading-[1.2] text-white'>
          {t('linkaiHome.hero.subtitle')}
        </p>
        <p className='mx-auto mt-5 max-w-[860px] text-[clamp(1rem,2.45vw,3rem)] leading-[1.2] text-white/90'>
          {t('linkaiHome.hero.description')}
        </p>

        <div className='mx-auto mt-12 grid max-w-[950px] gap-4 sm:grid-cols-2 lg:mt-[52px] lg:gap-7'>
          <Link
            to={props.isAuthenticated ? '/dashboard' : '/sign-up'}
            className='group relative min-h-28 overflow-hidden rounded-full border border-white/15 p-5 text-left transition-[transform,border-color,box-shadow] duration-500 ease-out hover:-translate-y-2 hover:scale-[1.015] hover:border-white/45 hover:shadow-[0_24px_60px_-22px_rgba(133,91,255,0.85)] focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white active:translate-y-0 active:scale-[0.99] motion-reduce:transform-none motion-reduce:transition-none lg:min-h-32'
          >
            <img
              src={LINKAI_HOME_ASSETS.heroEnterpriseButton}
              alt=''
              width={460}
              height={128}
              className='absolute inset-0 h-full w-full object-cover transition-[transform,filter] duration-700 ease-out group-hover:scale-110 group-hover:brightness-110 motion-reduce:transform-none motion-reduce:transition-none'
            />
            <span
              aria-hidden='true'
              className='pointer-events-none absolute inset-y-0 left-[-38%] w-[30%] -skew-x-12 bg-gradient-to-r from-transparent via-white/25 to-transparent transition-transform duration-700 ease-out group-hover:translate-x-[470%] motion-reduce:hidden'
            />
            <span className='relative block text-center text-[clamp(1.25rem,1.9vw,2.25rem)] leading-[1.2] text-white transition-transform duration-500 group-hover:-translate-y-0.5 motion-reduce:transform-none motion-reduce:transition-none'>
              {t('linkaiHome.hero.enterpriseTitle')}
            </span>
            <span className='relative mt-1 block text-center text-xs tracking-[0.04em] text-white/80 transition-transform duration-500 group-hover:translate-y-0.5 motion-reduce:transform-none motion-reduce:transition-none sm:text-sm lg:text-lg'>
              {t('linkaiHome.hero.enterpriseDescription')}
            </span>
          </Link>
          <Link
            to={props.isAuthenticated ? '/chat2link' : '/sign-up'}
            className='group relative min-h-28 overflow-hidden rounded-full border border-white/20 p-5 text-left transition-[transform,border-color,box-shadow] duration-500 ease-out hover:-translate-y-2 hover:scale-[1.015] hover:border-white/45 hover:shadow-[0_24px_60px_-22px_rgba(85,203,255,0.8)] focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white active:translate-y-0 active:scale-[0.99] motion-reduce:transform-none motion-reduce:transition-none lg:min-h-32'
          >
            <img
              src={LINKAI_HOME_ASSETS.heroPersonalButton}
              alt=''
              width={460}
              height={128}
              className='absolute inset-0 h-full w-full object-cover transition-[transform,filter] duration-700 ease-out group-hover:scale-110 group-hover:brightness-110 motion-reduce:transform-none motion-reduce:transition-none'
            />
            <span
              aria-hidden='true'
              className='pointer-events-none absolute inset-y-0 left-[-38%] w-[30%] -skew-x-12 bg-gradient-to-r from-transparent via-white/25 to-transparent transition-transform duration-700 ease-out group-hover:translate-x-[470%] motion-reduce:hidden'
            />
            <span className='relative block text-center text-[clamp(1.25rem,1.9vw,2.25rem)] leading-[1.2] text-white transition-transform duration-500 group-hover:-translate-y-0.5 motion-reduce:transform-none motion-reduce:transition-none'>
              {t('linkaiHome.hero.personalTitle')}
            </span>
            <span className='relative mt-1 block text-center text-xs tracking-[0.04em] text-white/80 transition-transform duration-500 group-hover:translate-y-0.5 motion-reduce:transform-none motion-reduce:transition-none sm:text-sm lg:text-lg'>
              {t('linkaiHome.hero.personalDescription')}
            </span>
          </Link>
        </div>

        <div className='mt-[520px] border-y border-white/[0.18] text-left sm:mt-[660px] lg:relative lg:left-1/2 lg:mt-[792px] lg:w-[95vw] lg:-translate-x-1/2'>
          <dl className='grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-[25.82%_25.88%_30.76%_17.54%]'>
            {stats.map((stat) => (
              <div
                key={stat.label}
                className='min-h-64 border-b border-white/15 px-1 py-9 last:border-b-0 sm:min-h-72 sm:px-6 sm:py-12 lg:min-h-[478px] lg:border-b-0 lg:px-0 lg:py-14 lg:pr-10'
              >
                <dd className='flex items-end gap-2 text-[clamp(4rem,7.7vw,9.25rem)] leading-none font-bold tracking-[-0.06em] text-white'>
                  {stat.value}
                  {stat.suffix ? (
                    <span className='mb-3 text-xl tracking-normal sm:text-2xl lg:mb-5 lg:text-4xl'>
                      {stat.suffix}
                    </span>
                  ) : null}
                </dd>
                <dt className='mt-5 text-2xl font-bold text-white lg:text-4xl'>
                  {stat.label}
                </dt>
                <p className='mt-3 max-w-[280px] text-lg leading-relaxed text-white/80 lg:text-2xl'>
                  {stat.description}
                </p>
              </div>
            ))}
          </dl>
        </div>
      </div>
    </section>
  )
}
