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
import { Check, Copy } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { DocsArticle } from './docs-data'

export function DocsArticleContent(props: { article: DocsArticle }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)

  const copyArticleLink = async () => {
    await navigator.clipboard.writeText(
      `${window.location.origin}/docs?article=${props.article.id}`
    )
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1600)
  }

  return (
    <div className='grid min-w-0 lg:grid-cols-[minmax(0,1fr)_300px] xl:grid-cols-[minmax(0,1fr)_390px]'>
      <article className='relative min-w-0 px-5 py-10 sm:px-10 lg:px-[76px] lg:py-10'>
        <button
          type='button'
          onClick={copyArticleLink}
          className='absolute top-7 right-5 flex size-[52px] items-center justify-center rounded-xl border border-[#222] bg-[#0f0f0f] text-white transition hover:border-white/35 hover:bg-[#151515] sm:right-10 lg:right-[76px]'
          aria-label={t('Copy article link')}
        >
          {copied ? (
            <Check className='size-6' aria-hidden />
          ) : (
            <Copy className='size-6' aria-hidden />
          )}
        </button>

        <h1 className='pr-16 text-[clamp(1.8rem,2.4vw,2.25rem)] font-bold text-[#eee]'>
          {t(props.article.title)}
        </h1>
        <p className='mt-9 text-[26px] text-[#eee] sm:text-[30px]'>
          {t(props.article.subtitle)}
        </p>

        <div className='mt-8 space-y-10 text-lg leading-8 text-[#eee] sm:text-xl sm:leading-9'>
          {props.article.sections.map((section, sectionIndex) => (
            <section key={section.id} id={`docs-${section.id}`}>
              {sectionIndex > 0 && (
                <h2 className='mb-5 text-2xl font-bold'>{t(section.title)}</h2>
              )}
              <div className='space-y-5'>
                {section.paragraphs.map((paragraph) => (
                  <p key={paragraph}>{t(paragraph)}</p>
                ))}
                {section.numbered && (
                  <ol className='list-decimal space-y-4 pl-6'>
                    {section.numbered.map((paragraph) => (
                      <li key={paragraph} className='pl-2'>
                        {t(paragraph)}
                      </li>
                    ))}
                  </ol>
                )}
              </div>
            </section>
          ))}
        </div>
      </article>

      <aside className='hidden px-6 py-[58px] lg:block'>
        <div className='sticky top-[193px]'>
          <h2 className='flex items-center gap-3 text-xl text-[#eee]'>
            <span className='h-2.5 w-4 rounded-sm bg-[#eee]' />
            {t('On this page')}
          </h2>
          <nav className='mt-4 space-y-2' aria-label={t('On this page')}>
            {props.article.sections.map((section, index) => (
              <a
                key={section.id}
                href={`#docs-${section.id}`}
                className={
                  index === 0
                    ? 'block text-lg leading-9 text-[#eee]'
                    : 'block text-lg leading-9 text-[#8c8c8c] hover:text-[#eee]'
                }
              >
                {t(section.title)}
              </a>
            ))}
          </nav>
        </div>
      </aside>
    </div>
  )
}
