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
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { RichContent } from '@/components/rich-content'
import { getAboutContent } from '@/features/about/api'
import { isHttpUrl, isLikelyHtml } from '@/lib/content-format'
import { cn } from '@/lib/utils'

import { LinkAiPublicFooter } from '../components/public-footer'
import { LinkAiPublicHeader } from '../components/public-header'

const ABOUT_SECTIONS = [
  { id: 'about', label: 'About LinkAI' },
  { id: 'disclaimer', label: 'Disclaimer' },
  { id: 'privacy', label: 'Privacy Policy' },
  { id: 'terms', label: 'Terms of Service' },
] as const

type AboutSectionId = (typeof ABOUT_SECTIONS)[number]['id']

function LegalArticle(props: { section: Exclude<AboutSectionId, 'about'> }) {
  const { t } = useTranslation()
  let title = 'Disclaimer'
  let paragraphs = [
    'Welcome to the official Omnitoken & LinkAI website. Before using this website and its services, please read and fully understand this disclaimer. By accessing, browsing, or using any part of this website, you confirm that you have read, understood, and accepted this statement.',
  ]

  if (props.section === 'privacy') {
    title = 'Privacy Policy'
    paragraphs = [
      'We collect only the account and service data required to provide, secure, and improve LinkAI. Your prompts and credentials are handled according to the selected service and deployment mode.',
      'You may request access, correction, or deletion of eligible personal data through the support channel. Legal retention and security obligations may still apply.',
    ]
  }

  if (props.section === 'terms') {
    title = 'Terms of Service'
    paragraphs = [
      'Use LinkAI lawfully and do not attempt to disrupt the service, bypass access controls, or infringe the rights of others. You are responsible for activity performed with your account and API keys.',
      'Model availability, pricing, and rate limits may change as upstream services evolve. The current console configuration and transaction quotation take precedence.',
    ]
  }

  return (
    <article>
      <h1 className='text-4xl font-bold text-[#eee]'>{t(title)}</h1>
      <p className='mt-2 text-sm text-[#606060]'>{t('Updated: August 2026')}</p>
      <div className='mt-8 space-y-8 text-lg leading-9 text-[#eee] sm:text-xl'>
        {paragraphs.map((paragraph) => (
          <p key={paragraph}>{t(paragraph)}</p>
        ))}
        {props.section === 'disclaimer' && (
          <>
            <h2 className='text-2xl font-bold'>
              {t('1. About information content')}
            </h2>
            <ol className='list-decimal space-y-4 pl-6'>
              <li>
                {t(
                  'All information published on this website, including product introductions, technical parameters, pricing descriptions, and industry cases, is for reference only and does not constitute an offer, commitment, or commercial guarantee.'
                )}
              </li>
              <li>
                {t(
                  'We make every effort to keep information accurate, complete, and timely, but cannot guarantee that every item is error-free. Decisions made in reliance on this website are at your own risk.'
                )}
              </li>
              <li>
                {t(
                  'Model prices and token rates may be adjusted according to market conditions. The quotation at the time of the actual transaction shall prevail.'
                )}
              </li>
            </ol>
            <h2 className='text-2xl font-bold'>
              {t('2. About external links')}
            </h2>
            <p>
              {t(
                'This website may contain links to third-party websites. They are provided for convenience and do not represent an endorsement or guarantee of third-party content, products, or services.'
              )}
            </p>
          </>
        )}
      </div>
    </article>
  )
}

function AboutArticle(props: { rawContent: string; loading: boolean }) {
  const { t } = useTranslation()

  if (props.loading) {
    return (
      <div className='space-y-5' aria-label={t('Loading')}>
        <div className='skeleton-shimmer h-12 w-64 rounded-lg' />
        <div className='skeleton-shimmer h-5 w-40 rounded-lg' />
        <div className='skeleton-shimmer mt-10 h-5 w-full rounded-lg' />
        <div className='skeleton-shimmer h-5 w-11/12 rounded-lg' />
      </div>
    )
  }

  if (props.rawContent && isHttpUrl(props.rawContent)) {
    return (
      <iframe
        src={props.rawContent}
        className='h-[720px] w-full rounded-xl border border-[#181818]'
        title={t('About')}
        sandbox='allow-forms allow-popups allow-popups-to-escape-sandbox allow-scripts'
      />
    )
  }

  if (props.rawContent) {
    const mode = isLikelyHtml(props.rawContent) ? 'html' : 'markdown'
    return (
      <article>
        <h1 className='mb-8 text-4xl font-bold text-[#eee]'>
          {t('About LinkAI')}
        </h1>
        <RichContent
          mode={mode}
          htmlVariant={mode === 'html' ? 'isolated' : undefined}
          content={props.rawContent}
          className='prose-invert max-w-none text-[#eee]'
        />
      </article>
    )
  }

  return (
    <article>
      <h1 className='text-4xl font-bold text-[#eee]'>{t('About LinkAI')}</h1>
      <p className='mt-2 text-sm text-[#606060]'>{t('Updated: August 2026')}</p>
      <div className='mt-8 space-y-8 text-lg leading-9 text-[#eee] sm:text-xl'>
        <p>
          {t(
            'LinkAI brings enterprise-grade model access and an intelligent personal assistant into one connected AI experience.'
          )}
        </p>
        <p>{t('linkaiHome.hero.description')}</p>
        <h2 className='text-2xl font-bold'>{t('Our mission')}</h2>
        <p>
          {t(
            'Make reliable AI infrastructure and capable assistants available on demand, with transparent control over models, costs, and data.'
          )}
        </p>
      </div>
    </article>
  )
}

export function LinkAiAboutPage() {
  const { t } = useTranslation()
  const [section, setSection] = useState<AboutSectionId>('about')
  const aboutQuery = useQuery({
    queryKey: ['about-content'],
    queryFn: getAboutContent,
  })
  const rawContent = aboutQuery.data?.data?.trim() || ''

  return (
    <div className='min-h-svh bg-black text-white'>
      <LinkAiPublicHeader />
      <main className='mx-auto grid w-full max-w-[1394px] gap-10 px-5 py-12 sm:px-8 lg:grid-cols-[328px_minmax(0,1fr)] lg:py-[100px] xl:px-0'>
        <aside>
          <p className='px-4 text-base text-[#eee]'>{t('Information')}</p>
          <label className='mt-4 block lg:hidden'>
            <span className='sr-only'>{t('About navigation')}</span>
            <select
              value={section}
              onChange={(event) =>
                setSection(event.target.value as AboutSectionId)
              }
              className='h-12 w-full rounded-[9px] border border-[#181818] bg-[#262626] px-4 text-white outline-none'
            >
              {ABOUT_SECTIONS.map((item) => (
                <option key={item.id} value={item.id}>
                  {t(item.label)}
                </option>
              ))}
            </select>
          </label>
          <nav
            className='mt-4 hidden space-y-1 lg:block'
            aria-label={t('About navigation')}
          >
            {ABOUT_SECTIONS.map((item) => (
              <button
                key={item.id}
                type='button'
                onClick={() => setSection(item.id)}
                className={cn(
                  'block min-h-[49px] w-full rounded-[9px] px-4 text-left text-lg text-[#eee] transition hover:bg-white/10',
                  section === item.id && 'bg-[#262626]'
                )}
              >
                {t(item.label)}
              </button>
            ))}
          </nav>
        </aside>

        <div className='min-w-0'>
          {section === 'about' ? (
            <AboutArticle
              rawContent={rawContent}
              loading={aboutQuery.isLoading}
            />
          ) : (
            <LegalArticle section={section} />
          )}
        </div>
      </main>
      <LinkAiPublicFooter />
    </div>
  )
}
