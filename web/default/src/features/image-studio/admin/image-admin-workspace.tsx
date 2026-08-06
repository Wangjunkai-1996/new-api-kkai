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
export function ImageAdminWorkspace(props: {
  list: React.ReactNode
  editor: React.ReactNode
}) {
  return (
    <div className='border-border grid min-h-[34rem] overflow-hidden rounded-lg border md:grid-cols-[280px_minmax(0,1fr)]'>
      <aside className='bg-muted/20 min-h-0 border-b md:border-r md:border-b-0'>
        {props.list}
      </aside>
      <section className='bg-background min-h-0'>{props.editor}</section>
    </div>
  )
}
