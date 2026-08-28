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

import { useStatus } from '@/hooks/use-status'

import { LINKAI_HOME_ASSETS } from './assets'

type FooterColumnProps = {
  title: string
  children: React.ReactNode
}

function FooterColumn(props: FooterColumnProps) {
  return (
    <div>
      <h3 className='text-[clamp(1.125rem,1.56vw,1.875rem)] leading-[1.2] font-normal text-white'>
        {props.title}
      </h3>
      <div className='mt-[clamp(1.25rem,2.03vw,2.438rem)] flex flex-col gap-[clamp(0.875rem,1.72vw,2.063rem)] text-[clamp(0.875rem,1.25vw,1.5rem)] leading-[1.2] text-white/75'>
        {props.children}
      </div>
    </div>
  )
}

type TeamCtaProps = {
  isAuthenticated: boolean
}

export function LinkAiTeamCta(props: TeamCtaProps) {
  const { t } = useTranslation()

  return (
    <section className='px-5 py-24 sm:px-8 lg:px-[5vw] lg:pt-[189px] lg:pb-[261px]'>
      <div className='mx-auto max-w-[1561px] text-center'>
        <h2 className='text-[clamp(3rem,5.7vw,6.75rem)] leading-[1.2] font-bold tracking-[-0.055em] text-white'>
          {t('linkaiHome.cta.title')}
        </h2>
        <div className='relative mt-14 overflow-hidden sm:mt-20 lg:mt-[71px]'>
          <img
            src={LINKAI_HOME_ASSETS.teamCta}
            alt={t('linkaiHome.cta.imageAlt')}
            width={1561}
            height={736}
            loading='lazy'
            className='aspect-[1561/736] w-full object-cover'
          />
          <div className='absolute inset-x-0 bottom-7 flex justify-center sm:bottom-12'>
            <Link
              to={props.isAuthenticated ? '/dashboard' : '/sign-up'}
              className='rounded-full border border-white/25 bg-black/55 px-7 py-3 text-sm font-semibold text-white backdrop-blur-md transition hover:bg-white hover:text-black focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white sm:px-10 sm:py-4 sm:text-lg'
            >
              {props.isAuthenticated
                ? t('Go to Dashboard')
                : t('linkaiHome.cta.action')}
            </Link>
          </div>
        </div>
      </div>
    </section>
  )
}

export function LinkAiLandingFooter() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const docsHref =
    (status?.docs_link as string | undefined) || 'https://docs.newapi.pro'
  const currentYear = new Date().getFullYear()

  return (
    <footer className='relative overflow-hidden border-t border-white/[0.18] px-5 pt-16 pb-8 sm:px-8 lg:h-[clamp(300px,20.94vw,402px)] lg:px-[5vw] lg:pt-[clamp(3.25rem,4.69vw,5.625rem)] lg:pb-0'>
      <img
        src={LINKAI_HOME_ASSETS.footerBackground}
        alt=''
        width={1920}
        height={2796}
        className='pointer-events-none absolute inset-0 h-full w-full object-cover object-bottom opacity-80 lg:hidden'
      />
      <div className='relative z-10 mx-auto h-full max-w-[1572px]'>
        <div className='grid gap-14 lg:grid-cols-[minmax(280px,0.8fr)_minmax(0,1.2fr)] lg:gap-[clamp(2rem,4vw,75px)]'>
          <div>
            <img
              src={LINKAI_HOME_ASSETS.brandLogoFooter}
              alt='LinkAI'
              width={303}
              height={52}
              className='h-auto w-[clamp(200px,15.8vw,303px)]'
            />
            <p className='mt-[clamp(1.5rem,2.14vw,2.563rem)] text-[clamp(1rem,1.25vw,1.5rem)] leading-[1.35] text-white'>
              {t('linkaiHome.footer.tagline')}
            </p>
          </div>
          <div className='grid grid-cols-2 gap-10 sm:grid-cols-4 lg:-mt-[5px] lg:grid-cols-4 lg:gap-[clamp(1.25rem,2.4vw,46px)]'>
            <FooterColumn title={t('linkaiHome.footer.products')}>
              <Link to='/pricing' className='hover:text-white'>
                {t('linkaiHome.footer.enterprise')}
              </Link>
              <Link to='/sign-up' className='hover:text-white'>
                {t('linkaiHome.footer.personal')}
              </Link>
            </FooterColumn>
            <FooterColumn title={t('linkaiHome.footer.support')}>
              <a
                href={docsHref}
                target='_blank'
                rel='noreferrer'
                className='hover:text-white'
              >
                {t('linkaiHome.footer.helpCenter')}
              </a>
              <a
                href={docsHref}
                target='_blank'
                rel='noreferrer'
                className='hover:text-white'
              >
                {t('linkaiHome.footer.apiDocs')}
              </a>
            </FooterColumn>
            <FooterColumn title={t('linkaiHome.footer.social')}>
              <a
                href='https://github.com/QuantumNous/new-api'
                target='_blank'
                rel='noopener noreferrer'
                className='hover:text-white'
              >
                GitHub
              </a>
              <span>{t('linkaiHome.footer.wechat')}</span>
              <span>{t('linkaiHome.footer.videoChannel')}</span>
            </FooterColumn>
            <FooterColumn title={t('linkaiHome.footer.compliance')}>
              <Link to='/privacy-policy' className='hover:text-white'>
                {t('Privacy Policy')}
              </Link>
              <Link to='/user-agreement' className='hover:text-white'>
                {t('linkaiHome.footer.terms')}
              </Link>
              <span>{t('linkaiHome.footer.iso')}</span>
            </FooterColumn>
          </div>
        </div>

        <div className='mt-20 flex flex-col gap-3 border-t border-white/20 pt-6 text-xs text-white/45 sm:flex-row sm:items-center sm:justify-between sm:text-sm lg:absolute lg:inset-x-0 lg:bottom-4 lg:mt-0 lg:pt-3'>
          <p>
            © {currentYear} LinkAI. {t('linkaiHome.footer.rights')}
          </p>
          <p>
            {t('linkaiHome.footer.poweredBy')}{' '}
            <a
              href='https://github.com/QuantumNous/new-api'
              target='_blank'
              rel='noopener noreferrer'
              className='text-white/70 hover:text-white'
            >
              QuantumNous/new-api
            </a>
          </p>
        </div>
      </div>
    </footer>
  )
}
