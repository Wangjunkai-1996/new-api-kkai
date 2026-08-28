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

import { LINKAI_HOME_ASSETS } from '@/features/home/components/linkai-home/assets'

const FOOTER_GROUPS = [
  {
    title: 'linkaiHome.footer.products',
    links: [
      { label: 'linkaiHome.footer.enterprise', href: '/pricing' },
      { label: 'linkaiHome.footer.personal', href: '/' },
    ],
  },
  {
    title: 'linkaiHome.footer.support',
    links: [
      { label: 'linkaiHome.footer.helpCenter', href: '/about' },
      { label: 'linkaiHome.footer.apiDocs', href: '/docs' },
    ],
  },
  {
    title: 'linkaiHome.footer.social',
    links: [
      { label: 'GitHub', href: 'https://github.com/QuantumNous/new-api' },
      { label: 'linkaiHome.footer.wechat', href: '/about' },
      { label: 'linkaiHome.footer.videoChannel', href: '/about' },
    ],
  },
  {
    title: 'linkaiHome.footer.compliance',
    links: [
      { label: 'Privacy Policy', href: '/about' },
      { label: 'linkaiHome.footer.terms', href: '/about' },
      { label: 'linkaiHome.footer.iso', href: '/about' },
    ],
  },
] as const

function FooterLink(props: { href: string; label: string }) {
  const { t } = useTranslation()
  const className =
    'block w-fit text-[clamp(0.875rem,1.25vw,1.5rem)] leading-[1.35] text-white/75 transition hover:text-white'

  if (props.href.startsWith('http')) {
    return (
      <a
        href={props.href}
        className={className}
        target='_blank'
        rel='noreferrer'
      >
        {t(props.label)}
      </a>
    )
  }

  return (
    <a href={props.href} className={className}>
      {t(props.label)}
    </a>
  )
}

export function LinkAiPublicFooter() {
  const { t } = useTranslation()
  const currentYear = new Date().getFullYear()

  return (
    <footer className='border-t border-white/10 bg-black text-white'>
      <div className='mx-auto grid w-full max-w-[1576px] gap-10 px-5 py-[clamp(3rem,4.5vw,5.375rem)] sm:px-8 lg:grid-cols-[1.2fr_2fr]'>
        <div>
          <img
            src={LINKAI_HOME_ASSETS.brandLogoFooter}
            alt='LinkAI'
            width={303}
            height={52}
            className='h-auto w-[clamp(200px,15.8vw,303px)]'
          />
          <p className='mt-[clamp(1.5rem,2.1vw,2.5rem)] text-[clamp(1rem,1.25vw,1.5rem)] leading-[1.35]'>
            {t('linkaiHome.footer.tagline')}
          </p>
        </div>

        <div className='grid grid-cols-2 gap-8 sm:grid-cols-4 lg:gap-[clamp(1.5rem,2.1vw,2rem)]'>
          {FOOTER_GROUPS.map((group) => (
            <section key={group.title}>
              <h2 className='text-[clamp(1.125rem,1.56vw,1.875rem)] leading-[1.2] font-medium'>
                {t(group.title)}
              </h2>
              <div className='mt-[clamp(1.25rem,1.67vw,2rem)] space-y-[clamp(0.75rem,1.05vw,1.25rem)]'>
                {group.links.map((link) => (
                  <FooterLink
                    key={link.label}
                    href={link.href}
                    label={link.label}
                  />
                ))}
              </div>
            </section>
          ))}
        </div>
        <div className='border-t border-white/20 pt-6 text-sm text-white/50 lg:col-span-2 lg:flex lg:items-center lg:justify-between'>
          <p>
            © {currentYear} LinkAI. {t('linkaiHome.footer.rights')}
          </p>
          <p className='mt-2 lg:mt-0'>
            {t('linkaiHome.footer.poweredBy')}{' '}
            <a
              href='https://github.com/QuantumNous/new-api'
              target='_blank'
              rel='noopener noreferrer'
              className='text-white/75 hover:text-white'
            >
              QuantumNous/new-api
            </a>
          </p>
        </div>
      </div>
    </footer>
  )
}
