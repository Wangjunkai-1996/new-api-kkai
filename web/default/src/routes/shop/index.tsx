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
import { createFileRoute } from '@tanstack/react-router'

import { CatalogPage } from '@/features/shop/pages/catalog'
import { listSearchSchema } from '@/features/shop/schemas'

export const Route = createFileRoute('/shop/')({
  validateSearch: listSearchSchema,
  component: Page,
})
function Page() {
  const search = Route.useSearch()
  return (
    <CatalogPage
      q={search.q}
      category_id={search.category_id}
      page={search.page}
    />
  )
}
