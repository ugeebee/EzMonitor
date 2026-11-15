import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { formatClock } from '../format'

export interface ChartSeries {
  key: string
  label: string
  color: string
  dashed?: boolean
}

export type ChartRow = { t: string } & Record<string, number | string | null>

interface Props {
  title: string
  data: ChartRow[]
  series: ChartSeries[]
  format: (v: number) => string
  yMax?: number
  xFormat?: (iso: string) => string
}

export function MetricChart({ title, data, series, format, yMax, xFormat = formatClock }: Props) {
  return (
    <section className="chart-card">
      <h3>{title}</h3>
      <ResponsiveContainer width="100%" height={220}>
        <LineChart data={data} margin={{ top: 6, right: 12, bottom: 0, left: 0 }}>
          <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" />
          <XAxis dataKey="t" tickFormatter={xFormat} minTickGap={48} />
          <YAxis domain={[0, yMax ?? 'auto']} width={64} tickFormatter={(v: number) => format(v)} />
          <Tooltip
            labelFormatter={(l) => new Date(String(l)).toLocaleString()}
            formatter={(v) => (typeof v === 'number' ? format(v) : String(v ?? ''))}
            contentStyle={{ background: 'var(--surface)', border: '1px solid var(--border)' }}
          />
          {series.length > 1 && <Legend />}
          {series.map((s) => (
            <Line
              key={s.key}
              type="monotone"
              dataKey={s.key}
              name={s.label}
              stroke={s.color}
              dot={false}
              strokeWidth={2}
              isAnimationActive={false}
              strokeDasharray={s.dashed ? '5 4' : undefined}
              connectNulls={false}
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
    </section>
  )
}
