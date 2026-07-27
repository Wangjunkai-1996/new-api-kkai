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

import { VideoModelAdmin } from './admin/video-model-admin'
import { VideoSampleAdmin } from './admin/video-sample-admin'

export function VideoAdminPage() {
  const { t } = useTranslation()
  const [section, setSection] = useState<'models' | 'samples'>('models')

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('videoStudio.admin.title')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          <Tabs
            value={section}
            onValueChange={(value) => setSection(value as 'models' | 'samples')}
          >
            <TabsList>
              <TabsTrigger value='models'>
                {t('videoStudio.admin.models')}
              </TabsTrigger>
              <TabsTrigger value='samples'>
                {t('videoStudio.admin.samples')}
              </TabsTrigger>
            </TabsList>
          </Tabs>
          {section === 'models' ? <VideoModelAdmin /> : <VideoSampleAdmin />}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
