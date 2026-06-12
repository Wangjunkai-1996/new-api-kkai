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
import dayjs from 'dayjs'
import { formatCurrencyUSD } from '@/lib/format'
import type { RebateRecord } from '../types'

export interface ChartDataPoint {
  date: string // YYYY-MM-DD
  amount: number // system USD amount
  count: number // 记录数
}

/**
 * 聚合返利记录为图表数据（按日期分组）
 * @param records 返利记录列表
 * @param days 最近 N 天（默认 30）
 * @returns 图表数据点数组
 */
export function aggregateRebateData(
  records: RebateRecord[],
  days: number = 30
): ChartDataPoint[] {
  // 1. 生成最近 N 天的日期数组（填充缺失日期为 0）
  const dateMap = new Map<string, ChartDataPoint>()
  const today = dayjs()

  for (let i = days - 1; i >= 0; i--) {
    const date = today.subtract(i, 'day').format('YYYY-MM-DD')
    dateMap.set(date, { date, amount: 0, count: 0 })
  }

  // 2. 聚合返利记录
  records.forEach((record) => {
    // createdAt 是 ISO 字符串格式
    const date = dayjs(record.createdAt).format('YYYY-MM-DD')
    const existing = dateMap.get(date)

    if (existing) {
      existing.amount += record.rebateAmount / 100 // 分转元
      existing.count += 1
    }
  })

  // 3. 转换为数组并排序
  return Array.from(dateMap.values()).sort((a, b) =>
    a.date.localeCompare(b.date)
  )
}

/**
 * 格式化日期显示（简化月-日）
 */
export function formatChartDate(date: string): string {
  return dayjs(date).format('MM-DD')
}

/**
 * 格式化金额显示（保留 2 位小数）
 */
export function formatChartAmount(amount: number): string {
  return formatCurrencyUSD(amount)
}
