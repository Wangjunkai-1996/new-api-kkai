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
import { LinkAiPublicHeader } from '../components/public-header'
import { DocsArticleContent } from './docs-article'
import { DOCS_ARTICLES } from './docs-data'
import { DocsSidebar } from './docs-sidebar'

export function LinkAiDocsPage(props: {
  articleId?: string
  onArticleChange: (articleId: string) => void
}) {
  const article =
    DOCS_ARTICLES.find((item) => item.id === props.articleId) ||
    DOCS_ARTICLES[0]

  return (
    <div className='min-h-svh bg-black text-white'>
      <LinkAiPublicHeader />
      <main className='mx-auto grid min-h-[calc(100svh-104px)] w-full max-w-[1920px] lg:min-h-[calc(100svh-135px)] lg:grid-cols-[512px_minmax(0,1fr)]'>
        <DocsSidebar activeId={article.id} onSelect={props.onArticleChange} />
        <DocsArticleContent key={article.id} article={article} />
      </main>
    </div>
  )
}
