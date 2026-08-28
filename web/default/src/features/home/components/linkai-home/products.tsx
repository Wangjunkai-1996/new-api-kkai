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

type ProductCardProps = {
  audience: string
  title: string
  description: string
  background: string
  features: Array<{ title: string; description: string }>
  action: string
  href: '/pricing' | '/sign-up' | '/chat2link'
}

function ProductCard(props: ProductCardProps) {
  return (
    <article className='group relative min-h-[650px] overflow-hidden rounded-[2rem] border border-white/10 p-7 sm:min-h-[780px] sm:p-11 lg:min-h-[1064px] lg:px-[47px] lg:pt-[78px] lg:pb-[65px]'>
      <img
        src={props.background}
        alt=''
        width={771}
        height={1064}
        className='absolute inset-0 h-full w-full object-cover transition-transform duration-700 group-hover:scale-[1.025]'
      />
      <div className='absolute inset-0 bg-linear-to-b from-black/0 via-black/5 to-black/25' />
      <div className='relative z-10 flex h-full flex-col text-white'>
        <p className='text-[clamp(1.7rem,3.1vw,3.75rem)] leading-tight font-bold tracking-[-0.04em]'>
          {props.audience}
        </p>
        <h3 className='mt-7 text-[clamp(1.35rem,2.5vw,3rem)] leading-[1.2] font-bold lg:mt-[49px] lg:ml-[18px]'>
          {props.title}
        </h3>
        <p className='mt-3 max-w-[560px] text-base leading-relaxed text-white/85 sm:text-xl lg:mt-[19px] lg:ml-[18px] lg:text-2xl'>
          {props.description}
        </p>
        <dl className='mt-12 space-y-8 sm:mt-16 lg:mt-[114px] lg:space-y-[59px]'>
          {props.features.map((feature) => (
            <div key={feature.title} className='pl-8 sm:pl-12 lg:pl-[70px]'>
              <dt className='text-lg font-bold sm:text-2xl lg:text-3xl'>
                {feature.title}
              </dt>
              <dd className='mt-2 text-sm leading-relaxed text-white/85 sm:text-lg lg:text-2xl'>
                {feature.description}
              </dd>
            </div>
          ))}
        </dl>
        <Link
          to={props.href}
          className='mt-auto flex min-h-16 items-center justify-between rounded-2xl bg-black px-6 text-base font-bold text-white transition hover:bg-black/75 focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white sm:min-h-24 sm:px-10 sm:text-2xl lg:mx-6 lg:min-h-[118px] lg:text-3xl'
        >
          {props.action}
          <img
            src={LINKAI_HOME_ASSETS.arrowCircle}
            alt=''
            width={58}
            height={58}
            className='size-10 object-contain sm:size-[58px]'
          />
        </Link>
      </div>
    </article>
  )
}

export function LinkAiProducts() {
  const { t } = useTranslation()
  const enterpriseFeatures = [
    {
      title: t('linkaiHome.products.enterprise.featureOneTitle'),
      description: t('linkaiHome.products.enterprise.featureOneDescription'),
    },
    {
      title: t('linkaiHome.products.enterprise.featureTwoTitle'),
      description: t('linkaiHome.products.enterprise.featureTwoDescription'),
    },
    {
      title: t('linkaiHome.products.enterprise.featureThreeTitle'),
      description: t('linkaiHome.products.enterprise.featureThreeDescription'),
    },
  ]
  const personalFeatures = [
    {
      title: t('linkaiHome.products.personal.featureOneTitle'),
      description: t('linkaiHome.products.personal.featureOneDescription'),
    },
    {
      title: t('linkaiHome.products.personal.featureTwoTitle'),
      description: t('linkaiHome.products.personal.featureTwoDescription'),
    },
    {
      title: t('linkaiHome.products.personal.featureThreeTitle'),
      description: t('linkaiHome.products.personal.featureThreeDescription'),
    },
  ]

  return (
    <>
      <section
        id='products'
        className='px-5 py-24 sm:px-8 lg:px-[5vw] lg:pt-[167px] lg:pb-0'
      >
        <div className='mx-auto max-w-[1728px]'>
          <h2 className='text-center text-[clamp(3.2rem,5.7vw,6.75rem)] leading-[1.2] font-bold tracking-[-0.055em] text-white'>
            {t('linkaiHome.products.title')}
            <span className='mt-1 block'>
              {t('linkaiHome.shared.ourProducts')}
            </span>
          </h2>
          <div className='mx-auto mt-16 grid gap-7 lg:mt-[45px] lg:max-w-[1569px] lg:grid-cols-2'>
            <ProductCard
              audience={t('linkaiHome.products.enterprise.audience')}
              title={t('linkaiHome.products.enterprise.title')}
              description={t('linkaiHome.products.enterprise.description')}
              background={LINKAI_HOME_ASSETS.enterpriseCard}
              features={enterpriseFeatures}
              action={t('linkaiHome.products.enterprise.action')}
              href='/pricing'
            />
            <ProductCard
              audience={t('linkaiHome.products.personal.audience')}
              title={t('linkaiHome.products.personal.title')}
              description={t('linkaiHome.products.personal.description')}
              background={LINKAI_HOME_ASSETS.personalCard}
              features={personalFeatures}
              action={t('linkaiHome.products.personal.action')}
              href='/sign-up'
            />
          </div>
        </div>
      </section>

      <section className='relative min-h-[720px] overflow-hidden py-20 sm:min-h-[900px] lg:-mt-11 lg:h-[1295px] lg:min-h-0 lg:py-0'>
        <img
          src={LINKAI_HOME_ASSETS.brandBackground}
          alt=''
          width={1920}
          height={1295}
          className='absolute inset-0 h-full w-full object-cover'
        />
        <div className='relative z-10 mx-auto h-full max-w-[1920px] px-5 sm:px-8 lg:px-[5vw]'>
          <img
            src={LINKAI_HOME_ASSETS.brandOrb}
            alt={t('linkaiHome.brand.imageAlt')}
            width={931}
            height={956}
            className='mx-auto mt-8 h-auto w-[min(92vw,930px)] object-contain lg:absolute lg:top-[261px] lg:left-[calc(50%+22px)] lg:mt-0 lg:w-[931px] lg:-translate-x-1/2'
          />
        </div>
        <div className='absolute inset-x-[5vw] top-[334px] hidden lg:block'>
          <span className='absolute top-2 left-0 h-px w-[447px] bg-white/80' />
          <p className='absolute right-0 w-[min(34vw,495px)] text-2xl tracking-[0.22em] text-white/90'>
            AI immediately empowers
          </p>
        </div>
      </section>
    </>
  )
}
