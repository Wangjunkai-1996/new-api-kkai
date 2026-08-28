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
import { LinkAiHero } from './hero'
import { LinkAiLandingFooter, LinkAiTeamCta } from './landing-footer'
import { LinkAiLandingHeader } from './landing-header'
import { LinkAiProducts } from './products'
import { LinkAiQuickStart } from './quick-start'
import { LinkAiTrustAndCases } from './trust-and-cases'

type LinkAiHomeProps = {
  isAuthenticated: boolean
}

export function LinkAiHome(props: LinkAiHomeProps) {
  return (
    <div
      className='relative min-h-screen overflow-x-clip bg-black text-white'
      style={{
        backgroundImage:
          "linear-gradient(rgb(0 0 0 / 0.72), rgb(0 0 0 / 0.72)), url('/figma/linkai-home/page-background.png')",
        backgroundPosition: 'top center',
        backgroundRepeat: 'repeat-y',
        backgroundSize: '100% auto',
        fontFamily:
          "'Noto Sans SC', 'Noto Sans CJK SC', 'Microsoft YaHei', sans-serif",
      }}
    >
      <LinkAiLandingHeader />
      <div className='origin-top lg:[zoom:0.86]'>
        <main>
          <LinkAiHero isAuthenticated={props.isAuthenticated} />
          <LinkAiQuickStart />
          <LinkAiProducts />
          <LinkAiTrustAndCases />
          <LinkAiTeamCta isAuthenticated={props.isAuthenticated} />
        </main>
        <LinkAiLandingFooter />
      </div>
    </div>
  )
}
