import type { SeriesPoint, SeriesRange, SeriesResponse } from '../types'

const STEP_S: Record<SeriesRange, number> = { '1h': 30, '6h': 120, '24h': 300, '7d': 3600 }
const RANGE_S: Record<SeriesRange, number> = { '1h': 3600, '6h': 21600, '24h': 86400, '7d': 604800 }

// Small seeded PRNG so the demo data is stable between refreshes.
export function mulberry32(seed: number) {
  let a = seed
  return () => {
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

const r1 = (v: number) => Math.round(v * 10) / 10
const clamp = (v: number) => Math.min(100, Math.max(0, v))

export function mockSeries(instanceId: number, range: SeriesRange): SeriesResponse {
  const step = STEP_S[range]
  const end = Math.floor(Date.now() / 1000 / step) * step
  const start = end - RANGE_S[range]
  const rand = mulberry32(instanceId * 7919)
  const phase = rand() * Math.PI * 2
  const base = 25 + rand() * 30
  const points: SeriesPoint[] = []

  for (let ts = start; ts < end; ts += step) {
    const t = new Date(ts * 1000).toISOString()
    const hour = ts / 3600
    // A short outage so the gap rendering is visible.
    const down = range !== '1h' && ts > end - RANGE_S[range] * 0.4 && ts < end - RANGE_S[range] * 0.4 + step * 3
    if (down) {
      points.push({
        t, samples: 0, cpu_avg: null, cpu_max: null, ram_avg: null, ram_max: null, gpu_avg: null,
        gpu_max: null, net_up_avg: null, net_up_max: null, net_down_avg: null, net_down_max: null,
        uptime: null, cpu_avg_smooth: null, fleet_cpu_avg: null, cpu_rank: null,
      })
      continue
    }
    const cpu = clamp(base + 18 * Math.sin(hour / 3 + phase) + (rand() - 0.5) * 10)
    const ram = clamp(45 + 8 * Math.sin(hour / 11 + phase) + (rand() - 0.5) * 3)
    const up = 120 + rand() * 200
    const down2 = 400 + rand() * 900
    points.push({
      t,
      samples: Math.max(1, Math.round(step / 15)),
      cpu_avg: r1(cpu), cpu_max: r1(clamp(cpu + 6 + rand() * 14)),
      ram_avg: r1(ram), ram_max: r1(clamp(ram + 1 + rand() * 3)),
      gpu_avg: null, gpu_max: null,
      net_up_avg: r1(up), net_up_max: r1(up * (1.4 + rand())),
      net_down_avg: r1(down2), net_down_max: r1(down2 * (1.4 + rand())),
      uptime: 800000 + (ts - start),
      cpu_avg_smooth: r1(cpu), fleet_cpu_avg: r1(base), cpu_rank: 1,
    })
  }

  return {
    from: new Date(start * 1000).toISOString(),
    to: new Date(end * 1000).toISOString(),
    step_s: step,
    rollup: step % 300 === 0,
    series: [{ i_id: instanceId, label: `instance-${instanceId}`, points }],
  }
}
