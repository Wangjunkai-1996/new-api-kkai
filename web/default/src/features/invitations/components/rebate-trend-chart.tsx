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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from 'recharts'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  aggregateRebateData,
  formatChartDate,
  formatChartAmount,
} from '../lib/chart-utils'
import type { RebateRecord } from '../types'

interface RebateTrendChartProps {
  records: RebateRecord[]
  days?: number
}

export function RebateTrendChart({
  records,
  days = 30,
}: RebateTrendChartProps) {
  const { t } = useTranslation()

  // 使用 useMemo 缓存聚合数据
  const chartData = useMemo(
    () => aggregateRebateData(records, days),
    [records, days]
  )

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          {t('Rebate Trend (Last {{days}} Days)', { days })}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {chartData.length === 0 || chartData.every((d) => d.amount === 0) ? (
          <div className='text-muted-foreground flex h-[300px] items-center justify-center'>
            {t('No data available')}
          </div>
        ) : (
          <>
            <ResponsiveContainer
              width='100%'
              height={300}
              className='md:h-[300px] lg:h-[350px]'
            >
              <LineChart data={chartData}>
                <CartesianGrid strokeDasharray='3 3' />
                <XAxis
                  dataKey='date'
                  tickFormatter={formatChartDate}
                  tick={{ fontSize: 12 }}
                  angle={-45}
                  textAnchor='end'
                  height={60}
                />
                <YAxis
                  tick={{ fontSize: 12 }}
                  tickFormatter={(value) =>
                    formatChartAmount(Number(value) || 0)
                  }
                  label={{
                    value: t('Amount'),
                    angle: -90,
                    position: 'insideLeft',
                  }}
                />
                <Tooltip
                  formatter={(value: unknown) => {
                    const numValue = typeof value === 'number' ? value : 0
                    return [formatChartAmount(numValue), t('Amount')]
                  }}
                  labelFormatter={(label: unknown) => {
                    const strLabel =
                      typeof label === 'string' ? label : String(label)
                    return formatChartDate(strLabel)
                  }}
                />
                <Line
                  type='monotone'
                  dataKey='amount'
                  stroke='#8884d8'
                  strokeWidth={2}
                  dot={{ r: 3 }}
                  activeDot={{ r: 5 }}
                />
              </LineChart>
            </ResponsiveContainer>

            <div className='text-muted-foreground mt-4 text-sm'>
              {t('Total Records')}: {records.length}
            </div>
          </>
        )}
      </CardContent>
    </Card>
  )
}
