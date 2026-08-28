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
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import { LINKAI_HOME_ASSETS } from './assets'

type TrustCardProps = {
  title: string
  description: string
  image: string
  className?: string
}

function TrustCard(props: TrustCardProps) {
  return (
    <article
      className={cn(
        'relative min-h-[360px] overflow-hidden rounded-[2rem] border border-white/10 p-7 sm:min-h-[420px] sm:p-9 lg:h-[420px] lg:pt-[85px]',
        props.className
      )}
    >
      <img
        src={props.image}
        alt=''
        width={548}
        height={420}
        className='absolute inset-0 h-full w-full object-cover'
      />
      <div className='absolute inset-0 bg-linear-to-b from-black/10 via-black/20 to-black/55' />
      <div className='relative z-10'>
        <h3 className='text-2xl font-bold text-white sm:text-3xl'>
          {props.title}
        </h3>
        <p className='mt-8 max-w-[430px] text-base leading-relaxed text-white/85 sm:text-xl lg:mt-[53px]'>
          {props.description}
        </p>
      </div>
    </article>
  )
}

export function LinkAiTrustAndCases() {
  const { t } = useTranslation()
  const trustCards = [
    {
      title: t('linkaiHome.trust.modelsTitle'),
      description: t('linkaiHome.trust.modelsDescription'),
      image: LINKAI_HOME_ASSETS.trustModels,
      className: 'lg:mt-[34px]',
    },
    {
      title: t('linkaiHome.trust.complianceTitle'),
      description: t('linkaiHome.trust.complianceDescription'),
      image: LINKAI_HOME_ASSETS.trustCompliance,
      className: 'lg:mt-[159px]',
    },
    {
      title: t('linkaiHome.trust.collaborationTitle'),
      description: t('linkaiHome.trust.collaborationDescription'),
      image: LINKAI_HOME_ASSETS.trustCollaboration,
      className: 'lg:mt-[34px]',
    },
  ]
  const cases = [
    {
      image: LINKAI_HOME_ASSETS.caseEnterprise,
      alt: t('linkaiHome.cases.enterpriseImageAlt'),
      title: t('linkaiHome.cases.enterpriseTitle'),
      description: t('linkaiHome.cases.enterpriseDescription'),
    },
    {
      image: LINKAI_HOME_ASSETS.caseCreator,
      alt: t('linkaiHome.cases.creatorImageAlt'),
      title: t('linkaiHome.cases.creatorTitle'),
      description: t('linkaiHome.cases.creatorDescription'),
    },
  ]

  return (
    <>
      <section
        id='why-linkai'
        className='relative px-5 py-24 sm:px-8 lg:-mt-[34px] lg:px-[5vw] lg:pt-0 lg:pb-0'
      >
        <div className='mx-auto max-w-[1728px]'>
          <h2 className='text-center text-[clamp(3.2rem,5.7vw,6.75rem)] leading-[1.2] font-bold tracking-[-0.055em] text-white'>
            {t('linkaiHome.trust.title')}
            <span className='mt-1 block'>{t('linkaiHome.trust.subtitle')}</span>
          </h2>
          <div className='relative mt-16 lg:mt-[51px] lg:min-h-[720px]'>
            <img
              src={LINKAI_HOME_ASSETS.trustCenter}
              alt=''
              width={919}
              height={613}
              className='pointer-events-none absolute top-0 left-1/2 hidden w-[min(58vw,919px)] -translate-x-1/2 object-contain lg:block'
            />
            <div className='relative z-10 grid gap-6 lg:w-full lg:grid-cols-3 lg:gap-[clamp(1.5rem,2.55vw,49px)]'>
              {trustCards.map((card) => (
                <TrustCard
                  key={card.title}
                  title={card.title}
                  description={card.description}
                  image={card.image}
                  className={card.className}
                />
              ))}
            </div>
          </div>
        </div>
      </section>

      <section
        id='cases'
        className='relative px-5 py-24 sm:px-8 lg:px-[5vw] lg:pt-[72px] lg:pb-0'
      >
        <img
          src={LINKAI_HOME_ASSETS.footerBackground}
          alt=''
          width={1920}
          height={2796}
          className='pointer-events-none absolute inset-x-0 top-[72px] hidden h-[2796px] w-full object-fill lg:block'
        />
        <div className='relative z-10 mx-auto max-w-[1728px]'>
          <h2 className='text-center text-[clamp(3.2rem,5.7vw,6.75rem)] leading-[1.2] font-bold tracking-[-0.055em] text-white'>
            {t('linkaiHome.cases.title')}
            <span className='mt-1 block'>{t('linkaiHome.cases.subtitle')}</span>
          </h2>
          <div className='mx-auto mt-16 grid gap-8 lg:mt-[22px] lg:max-w-[1570px] lg:grid-cols-2 lg:gap-[clamp(2rem,3.85vw,74px)]'>
            {cases.map((item) => (
              <article key={item.title}>
                <img
                  src={item.image}
                  alt={item.alt}
                  width={748}
                  height={534}
                  loading='lazy'
                  className='aspect-[748/534] w-full rounded-3xl object-cover'
                />
                <h3 className='mt-8 text-2xl font-bold text-white sm:text-3xl lg:mt-[68px] lg:text-4xl'>
                  {item.title}
                </h3>
                <p className='mt-4 text-base leading-relaxed text-white/45 sm:text-xl lg:mt-[23px] lg:text-2xl lg:leading-[1.2]'>
                  {item.description}
                </p>
              </article>
            ))}
          </div>
        </div>
      </section>
    </>
  )
}
