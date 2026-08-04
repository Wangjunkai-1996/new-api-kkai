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

import { SectionPageLayout } from '@/components/layout'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { ImageModelAdmin } from './admin/image-model-admin'
import { ImagePricingAdmin } from './admin/image-pricing-admin'
import { ImageSampleAdmin } from './admin/image-sample-admin'

type ImageAdminSection = 'models' | 'samples' | 'pricing'

export function ImageAdminPage() {
  const { t } = useTranslation()
  const [section, setSection] = useState<ImageAdminSection>('models')
  const isSuperAdmin = useAuthStore(
    (state) => state.auth.user?.role === ROLE.SUPER_ADMIN
  )

  let content = <ImageModelAdmin />
  if (section === 'samples') {
    content = <ImageSampleAdmin />
  } else if (section === 'pricing' && isSuperAdmin) {
    content = <ImagePricingAdmin />
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('imageStudio.admin.title')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          <Tabs
            value={section}
            onValueChange={(value) => setSection(value as ImageAdminSection)}
          >
            <div className='max-w-full overflow-x-auto'>
              <TabsList>
                <TabsTrigger value='models'>
                  {t('imageStudio.admin.models')}
                </TabsTrigger>
                <TabsTrigger value='samples'>
                  {t('imageStudio.admin.samples')}
                </TabsTrigger>
                {isSuperAdmin && (
                  <TabsTrigger value='pricing'>
                    {t('imageStudio.admin.pricing')}
                  </TabsTrigger>
                )}
              </TabsList>
            </div>
          </Tabs>
          {content}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
