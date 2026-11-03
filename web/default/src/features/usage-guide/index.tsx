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
import {
  ArrowUpRight,
  BookOpen,
  ChevronRight,
  CircleHelp,
  Download,
  Info,
  Laptop,
  MessageCircle,
  Monitor,
  Shuffle,
  Terminal,
} from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

import { getCCSwitchEndpoint } from '../keys/lib/cc-switch-url'
import { USAGE_TOOLS, type UsagePlatform, type UsageTool } from './catalog'
import { QuickSetupDialog } from './components/quick-setup-dialog'

type SetupPreset = 'gpt' | 'claude-code'
type PlatformFilter = 'all' | UsagePlatform

const TOOL_ICONS: Record<string, typeof Terminal> = {
  'cc-switch': Shuffle,
  'cherry-studio': MessageCircle,
  'codex-cli': Terminal,
  'codex-app': Laptop,
  'claude-code': Terminal,
  'claude-desktop': Monitor,
}

const FEATURED_TOOLS = [
  'cc-switch',
  'codex-app',
  'claude-desktop',
  'cherry-studio',
].flatMap((id) => USAGE_TOOLS.filter((tool) => tool.id === id))

export function UsageGuidePage() {
  const { t } = useTranslation()
  const [preset, setPreset] = useState<SetupPreset | null>(null)
  const [platform, setPlatform] = useState<PlatformFilter>('all')
  const [activeTab, setActiveTab] = useState('start')
  const [showCherryGuide, setShowCherryGuide] = useState(false)

  const visibleTools = useMemo(
    () =>
      USAGE_TOOLS.filter(
        (tool) => platform === 'all' || tool.platforms.includes(platform)
      ),
    [platform]
  )
  const openToolsTab = () => {
    setActiveTab('tools')
  }

  const openCherryGuide = () => {
    setActiveTab('start')
    setShowCherryGuide(true)
  }

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Usage guide')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <div className='mx-auto w-full max-w-[1800px] space-y-6 py-2 sm:space-y-8 sm:py-4'>
            <header className='space-y-2'>
              <h1 className='text-2xl leading-tight font-semibold tracking-tight sm:text-[26px]'>
                {t('Start using AI from here')}
              </h1>
              <p className='text-muted-foreground text-sm leading-relaxed'>
                {t('Choose a tool, finish setup, and send your first request.')}
              </p>
            </header>

            <Tabs value={activeTab} onValueChange={setActiveTab}>
              <TabsList variant='line' className='w-full justify-start gap-5'>
                <TabsTrigger value='start'>{t('Quick start')}</TabsTrigger>
                <TabsTrigger value='tools'>{t('Tool downloads')}</TabsTrigger>
                <TabsTrigger value='faq'>
                  {t('Frequently asked questions')}
                </TabsTrigger>
              </TabsList>

              <TabsContent value='start' className='mt-6 space-y-7'>
                <div className='bg-muted/45 rounded-lg p-3 text-sm sm:hidden'>
                  <p className='text-muted-foreground flex items-start gap-2'>
                    <Info
                      className='mt-0.5 size-4 shrink-0'
                      aria-hidden='true'
                    />
                    {t(
                      'CC Switch setup is completed on a computer. You can read the guide here or use manual setup for another client.'
                    )}
                  </p>
                </div>

                <section
                  aria-labelledby='quick-setup-title'
                  className='space-y-3'
                >
                  <div className='flex flex-wrap items-center justify-between gap-2'>
                    <h2
                      id='quick-setup-title'
                      className='text-lg font-semibold'
                    >
                      {t('Quick setup')}
                    </h2>
                    <span className='text-muted-foreground text-xs'>
                      {t('Configure coding clients with CC Switch')}
                    </span>
                  </div>
                  <div className='grid gap-4 md:grid-cols-2'>
                    <SetupCard
                      preset='gpt'
                      title={t('GPT / Codex')}
                      subtitle={t('Code writing · project development')}
                      description={t(
                        'Prepare a dedicated API key for Codex and import it into CC Switch.'
                      )}
                      group='GPT-稳定'
                      onSelect={setPreset}
                    />
                    <SetupCard
                      preset='claude-code'
                      title={t('Claude Code')}
                      subtitle={t('Code collaboration · complex tasks')}
                      description={t(
                        'Prepare a dedicated API key for Claude Code and import it into CC Switch.'
                      )}
                      group='claudecode-满血企业'
                      onSelect={setPreset}
                    />
                  </div>
                  <Alert className='bg-muted/30'>
                    <Info aria-hidden='true' />
                    <AlertDescription>
                      {t(
                        'The key has no separate quota limit. Usage still follows your account balance, plan, and group permissions.'
                      )}
                    </AlertDescription>
                  </Alert>
                  <div className='flex flex-wrap items-center justify-between gap-2 border-t pt-3'>
                    <span className='text-muted-foreground text-sm'>
                      {t(
                        'Already have an API key? Use it with a supported client.'
                      )}
                    </span>
                    <Button
                      type='button'
                      variant='link'
                      size='sm'
                      className='h-auto px-0'
                      render={<Link to='/keys' />}
                    >
                      {t('Open API Keys')}
                      <ArrowUpRight data-icon='inline-end' />
                    </Button>
                  </div>
                </section>

                <section
                  aria-labelledby='prepare-tools-title'
                  className='space-y-3'
                >
                  <div className='flex flex-wrap items-center justify-between gap-2'>
                    <h2
                      id='prepare-tools-title'
                      className='text-lg font-semibold'
                    >
                      {t('Prepare your tools first')}
                    </h2>
                    <Button
                      type='button'
                      variant='link'
                      size='sm'
                      className='h-auto px-0'
                      onClick={openToolsTab}
                    >
                      {t('All tools')}
                      <ChevronRight data-icon='inline-end' />
                    </Button>
                  </div>
                  <div className='grid gap-3 sm:grid-cols-2'>
                    {FEATURED_TOOLS.map((tool) => (
                      <ToolCard key={tool.id} tool={tool} />
                    ))}
                  </div>
                  {showCherryGuide && <CherryStudioGuide />}
                  <div className='text-muted-foreground flex flex-wrap items-center justify-between gap-2 border-t pt-4 text-xs'>
                    <span>
                      {t(
                        'Just want to chat? Try Cherry Studio without CC Switch.'
                      )}
                    </span>
                    <Button
                      type='button'
                      variant='link'
                      size='sm'
                      className='h-auto px-0 text-xs'
                      onClick={openCherryGuide}
                    >
                      {t('View connection guide')}
                      <ChevronRight data-icon='inline-end' />
                    </Button>
                  </div>
                </section>
              </TabsContent>

              <TabsContent value='tools' className='mt-6 space-y-5'>
                <div className='flex flex-wrap items-end justify-between gap-3'>
                  <div className='space-y-1'>
                    <h2 className='text-lg font-semibold'>
                      {t('Choose a tool')}
                    </h2>
                    <p className='text-muted-foreground text-sm'>
                      {t(
                        'Official entry points, filtered by operating system.'
                      )}
                    </p>
                  </div>
                  <label className='text-muted-foreground flex items-center gap-2 text-sm'>
                    {t('System')}
                    <Select
                      value={platform}
                      onValueChange={(value) =>
                        setPlatform(value as PlatformFilter)
                      }
                    >
                      <SelectTrigger
                        size='sm'
                        aria-label={t('Select operating system')}
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value='all'>{t('All systems')}</SelectItem>
                        <SelectItem value='windows'>{t('Windows')}</SelectItem>
                        <SelectItem value='macos'>{t('macOS')}</SelectItem>
                        <SelectItem value='linux'>{t('Linux')}</SelectItem>
                      </SelectContent>
                    </Select>
                  </label>
                </div>
                <div className='grid gap-3 sm:grid-cols-2'>
                  {visibleTools.map((tool) => (
                    <ToolCard key={tool.id} tool={tool} platform={platform} />
                  ))}
                </div>
                <Alert className='bg-muted/30'>
                  <Info aria-hidden='true' />
                  <AlertDescription>
                    {t(
                      'CC Switch manages configurations. You still need to install the client you want to use. Supported systems and versions follow each official page.'
                    )}
                  </AlertDescription>
                </Alert>
              </TabsContent>

              <TabsContent value='faq' className='mt-6 space-y-3'>
                <h2 className='text-lg font-semibold'>
                  {t('Having trouble connecting?')}
                </h2>
                <FAQ title={t('CC Switch did not open')}>
                  {t(
                    'Install and run CC Switch, allow the browser to open external applications, and click Open again. The created key is kept; you do not need to create another one.'
                  )}
                </FAQ>
                <FAQ title={t('What does no separate key quota mean?')}>
                  {t(
                    'This key has no independent consumption cap. Requests still consume the account balance or plan allowance and follow group permissions.'
                  )}
                </FAQ>
                <FAQ
                  title={t('Are Claude Code and Claude Desktop the same tool?')}
                >
                  {t(
                    'No. They are separate clients. The Claude quick setup is for Claude Code. Claude Desktop is a separate app and does not support directly configuring this site API key.'
                  )}
                </FAQ>
                <FAQ
                  title={t(
                    'I already have a key. Do I need to create another one?'
                  )}
                >
                  {t(
                    'No. Open API Keys and choose an existing key for a supported client such as Cherry Studio. This guide does not rename, change, or claim an existing key.'
                  )}
                </FAQ>
                <FAQ title={t('The key was created but my request failed')}>
                  {t(
                    'For 401, check that the key is complete and active. For 403, check group and model access. For a balance error, check your balance or plan. Use Usage Logs for more detail.'
                  )}
                </FAQ>
                <FAQ title={t('The result is unclear after a network error')}>
                  {t(
                    'Do not click create again immediately. Open API Keys and check for the key by its exact name. A later retry only applies to a confirmed key ID.'
                  )}
                </FAQ>
              </TabsContent>
            </Tabs>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <QuickSetupDialog preset={preset} onClose={() => setPreset(null)} />
    </>
  )
}

function SetupCard(props: {
  preset: SetupPreset
  title: string
  subtitle: string
  description: string
  group: string
  onSelect: (preset: SetupPreset) => void
}) {
  const { t } = useTranslation()

  return (
    <Card size='sm' className='rounded-xl'>
      <CardHeader className='gap-3 p-5'>
        <div className='flex items-center gap-3'>
          <span
            className={cn(
              'bg-muted text-foreground grid size-10 shrink-0 place-items-center rounded-xl',
              props.preset === 'gpt'
                ? 'bg-success/10 text-success'
                : 'bg-warning/10 text-warning'
            )}
          >
            {props.preset === 'gpt' ? (
              <Terminal className='size-5' aria-hidden='true' />
            ) : (
              <BookOpen className='size-5' aria-hidden='true' />
            )}
          </span>
          <div className='min-w-0'>
            <CardTitle>{props.title}</CardTitle>
            <CardDescription className='text-xs'>
              {props.subtitle}
            </CardDescription>
          </div>
        </div>
        <p className='text-muted-foreground min-h-10 text-sm leading-relaxed'>
          {props.description}
        </p>
      </CardHeader>
      <CardContent className='space-y-4 px-5 pb-5'>
        <div className='bg-muted/55 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1.5 rounded-lg px-3 py-2.5 text-xs'>
          <span className='text-muted-foreground'>{t('Group')}</span>
          <span className='min-w-0 truncate font-medium'>{props.group}</span>
          <span className='text-muted-foreground'>{t('Quota')}</span>
          <span>{t('No separate key quota limit')}</span>
        </div>
        <Button
          type='button'
          className='w-full'
          onClick={() => props.onSelect(props.preset)}
        >
          {props.preset === 'gpt'
            ? t('Configure GPT in one click')
            : t('Configure Claude in one click')}
          <ArrowUpRight data-icon='inline-end' />
        </Button>
      </CardContent>
    </Card>
  )
}

function ToolCard(props: { tool: UsageTool; platform?: PlatformFilter }) {
  const { t } = useTranslation()
  const Icon = TOOL_ICONS[props.tool.id] ?? Download
  const downloads = props.tool.downloads.filter(
    (download) =>
      !props.platform ||
      props.platform === 'all' ||
      download.platform === props.platform
  )

  return (
    <Card size='sm' className='rounded-lg'>
      <CardHeader className='gap-2 p-4'>
        <div className='flex items-start gap-3'>
          <span className='bg-muted grid size-8 shrink-0 place-items-center rounded-lg'>
            <Icon className='size-4' aria-hidden='true' />
          </span>
          <div className='min-w-0'>
            <CardTitle className='text-sm'>{t(props.tool.nameKey)}</CardTitle>
            <Badge variant='secondary' className='mt-1 text-[11px]'>
              {t(props.tool.kindKey)}
            </Badge>
          </div>
        </div>
        <CardDescription className='text-xs leading-relaxed'>
          {t(props.tool.descriptionKey)}
        </CardDescription>
      </CardHeader>
      <CardContent className='flex flex-col gap-3 px-4 pb-4'>
        <div className='flex flex-wrap gap-2'>
          {downloads.map((download) => (
            <Button
              key={`${download.platform}-${download.architecture ?? 'default'}`}
              render={
                <a
                  href={download.url}
                  target='_blank'
                  rel='noopener noreferrer'
                />
              }
              variant='outline'
              size='sm'
              className='min-h-9 text-xs'
            >
              <Download data-icon='inline-start' />
              {t(download.labelKey)}
              <ArrowUpRight data-icon='inline-end' />
            </Button>
          ))}
        </div>
        {props.tool.notesKey && (
          <p className='text-muted-foreground text-xs'>
            {t(props.tool.notesKey)}
          </p>
        )}
        <a
          href={props.tool.officialUrl}
          target='_blank'
          rel='noopener noreferrer'
          className='text-muted-foreground hover:text-foreground inline-flex w-fit items-center gap-1 text-xs underline underline-offset-3'
        >
          {t('Open official page')}
          <ArrowUpRight className='size-3' aria-hidden='true' />
        </a>
      </CardContent>
    </Card>
  )
}

function CherryStudioGuide() {
  const { t } = useTranslation()
  const endpoint = getCCSwitchEndpoint('codex')

  return (
    <Card className='border-dashed'>
      <CardHeader className='gap-1 p-4'>
        <CardTitle className='text-base'>
          {t('Cherry Studio connection guide')}
        </CardTitle>
        <CardDescription className='text-sm'>
          {t(
            'Use Cherry Studio directly with an existing API key. CC Switch is not required.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className='grid gap-3 p-4 sm:grid-cols-3'>
        <GuideStep
          number='1'
          title={t('Download and open Cherry Studio')}
          description={t(
            'Install the official desktop app, then open it once.'
          )}
          action={
            <Button
              type='button'
              variant='link'
              size='sm'
              className='h-auto justify-start px-0'
              render={
                <a
                  href='https://www.cherry-ai.com/download'
                  target='_blank'
                  rel='noopener noreferrer'
                />
              }
            >
              {t('Open Cherry Studio download page')}
              <ArrowUpRight data-icon='inline-end' />
            </Button>
          }
        />
        <GuideStep
          number='2'
          title={t('Add the gateway and API key')}
          description={t(
            'Choose the compatible provider, then enter this site endpoint, an existing API key, and a model.'
          )}
          action={
            <div className='space-y-1'>
              <code className='bg-muted text-muted-foreground block rounded px-2 py-1 text-xs break-all'>
                {endpoint}
              </code>
              <Button
                type='button'
                variant='link'
                size='sm'
                className='h-auto justify-start px-0'
                render={<Link to='/keys' />}
              >
                {t('Open API Keys')}
                <ArrowUpRight data-icon='inline-end' />
              </Button>
            </div>
          }
        />
        <GuideStep
          number='3'
          title={t('Choose a model and start chatting')}
          description={t(
            'Select a model available to the key and send your first message. Usage follows normal account rules.'
          )}
          action={
            <p className='text-muted-foreground text-xs'>
              {t('Keep your API key private. Do not share or screenshot it.')}
            </p>
          }
        />
      </CardContent>
    </Card>
  )
}

function GuideStep(props: {
  number: string
  title: string
  description: string
  action?: ReactNode
}) {
  return (
    <div className='space-y-2'>
      <div className='flex items-center gap-2'>
        <span className='bg-primary text-primary-foreground grid size-6 place-items-center rounded-full text-xs font-semibold'>
          {props.number}
        </span>
        <h3 className='text-sm font-medium'>{props.title}</h3>
      </div>
      <p className='text-muted-foreground text-xs leading-relaxed'>
        {props.description}
      </p>
      {props.action}
    </div>
  )
}

function FAQ(props: { title: string; children: string }) {
  return (
    <details className='rounded-lg border px-4 py-3'>
      <summary className='flex cursor-pointer list-none items-center gap-2 text-sm font-medium [&::-webkit-details-marker]:hidden'>
        <CircleHelp
          className='text-muted-foreground size-4 shrink-0'
          aria-hidden='true'
        />
        {props.title}
      </summary>
      <p className='text-muted-foreground pt-3 pl-6 text-sm leading-relaxed'>
        {props.children}
      </p>
    </details>
  )
}

export { USAGE_TOOLS }
