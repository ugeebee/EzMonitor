import type { InstanceStatus } from '../types'

export function StatusBadge({ status, enabled }: { status: InstanceStatus; enabled: boolean }) {
  if (!enabled) return <span className="badge badge-disabled">disabled</span>
  return <span className={`badge badge-${status}`}>{status}</span>
}
