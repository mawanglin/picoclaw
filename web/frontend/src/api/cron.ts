import { launcherFetch } from "./http"

export interface CronSchedule {
  kind: "at" | "every" | "cron"
  atMs?: number
  everyMs?: number
  expr?: string
  tz?: string
}

export interface CronPayload {
  kind?: string
  message: string
  command?: string
  channel?: string
  to?: string
}

export interface CronJobState {
  nextRunAtMs?: number | null
  lastRunAtMs?: number | null
  lastStatus?: string
  lastError?: string
}

export interface CronJob {
  id: string
  name: string
  enabled: boolean
  schedule: CronSchedule
  payload: CronPayload
  state: CronJobState
  createdAtMs: number
  updatedAtMs: number
  deleteAfterRun: boolean
}

export interface ExecutionRecord {
  id: number
  jobId: string
  jobName: string
  trigger: string
  status: string
  errorMsg: string
  output: string
  durationMs: number
  startedAt: number
  finishedAt: number
}

export interface CronStats {
  totalJobs: number
  enabledJobs: number
  runs24h: number
  success24h: number
  errors24h: number
  successRate24h: number
}

export interface TrendEntry {
  date: string
  ok: number
  error: number
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await launcherFetch(path, options)
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(body.error || res.statusText)
  }
  return res.json()
}

export async function listCronJobs(): Promise<CronJob[]> {
  const data = await request<{ jobs: CronJob[] }>("/api/cron/jobs")
  return data.jobs ?? []
}

export async function createCronJob(job: {
  name: string
  schedule: CronSchedule
  payload: { message: string; command?: string; channel?: string; to?: string }
}): Promise<CronJob> {
  const data = await request<{ job: CronJob }>("/api/cron/jobs", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(job),
  })
  return data.job
}

export async function updateCronJob(
  id: string,
  job: {
    name: string
    schedule: CronSchedule
    payload: {
      message: string
      command?: string
      channel?: string
      to?: string
    }
  },
): Promise<CronJob> {
  const data = await request<{ job: CronJob }>(`/api/cron/jobs/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(job),
  })
  return data.job
}

export async function deleteCronJob(id: string): Promise<void> {
  await request(`/api/cron/jobs/${id}`, { method: "DELETE" })
}

export async function enableCronJob(id: string): Promise<CronJob> {
  const data = await request<{ job: CronJob }>(`/api/cron/jobs/${id}/enable`, {
    method: "POST",
  })
  return data.job
}

export async function disableCronJob(id: string): Promise<CronJob> {
  const data = await request<{ job: CronJob }>(`/api/cron/jobs/${id}/disable`, {
    method: "POST",
  })
  return data.job
}

export async function triggerCronJob(id: string): Promise<void> {
  await request(`/api/cron/jobs/${id}/trigger`, { method: "POST" })
}

export async function getCronHistory(params: {
  page?: number
  size?: number
  job_id?: string
}): Promise<{
  records: ExecutionRecord[]
  total: number
  page: number
  size: number
}> {
  const sp = new URLSearchParams()
  if (params.page) sp.set("page", String(params.page))
  if (params.size) sp.set("size", String(params.size))
  if (params.job_id) sp.set("job_id", params.job_id)
  return request(`/api/cron/history?${sp.toString()}`)
}

export async function getCronStats(): Promise<CronStats> {
  return request("/api/cron/stats")
}

export async function getCronTrend(
  days?: number,
): Promise<{ trend: TrendEntry[] }> {
  const params = days ? `?days=${days}` : ""
  return request(`/api/cron/stats/trend${params}`)
}
