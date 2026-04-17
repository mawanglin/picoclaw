import { useTranslation } from "react-i18next"
import { useQuery } from "@tanstack/react-query"
import dayjs from "dayjs"

import { getCronStats, getCronTrend } from "@/api/cron"
import type { TrendEntry } from "@/api/cron"

function StatCard({
  label,
  value,
  loading,
}: {
  label: string
  value: string
  loading: boolean
}) {
  return (
    <div className="flex-1 rounded-lg border bg-card p-4">
      <div className="text-xs font-medium text-muted-foreground">{label}</div>
      {loading ? (
        <div className="mt-1 h-7 w-16 animate-pulse rounded bg-muted" />
      ) : (
        <div className="mt-1 text-2xl font-semibold tracking-tight">
          {value}
        </div>
      )}
    </div>
  )
}

function TrendChart({
  data,
  loading,
  t,
}: {
  data: TrendEntry[]
  loading: boolean
  t: (key: string) => string
}) {
  if (loading) {
    return (
      <div className="flex h-40 items-end gap-2">
        {Array.from({ length: 7 }).map((_, i) => (
          <div key={i} className="flex-1">
            <div className="h-20 animate-pulse rounded-t bg-muted" />
          </div>
        ))}
      </div>
    )
  }

  const maxCount = Math.max(1, ...data.map((d) => d.ok + d.error))
  const allZero = data.every((d) => d.ok === 0 && d.error === 0)

  if (allZero) {
    return (
      <div className="flex h-40 items-center justify-center text-sm text-muted-foreground">
        {t("pages.cron.stats.noData")}
      </div>
    )
  }

  return (
    <div className="space-y-2">
      <div className="flex h-32 items-end gap-1.5">
        {data.map((entry) => {
          const okH = (entry.ok / maxCount) * 100
          const errH = (entry.error / maxCount) * 100
          return (
            <div
              key={entry.date}
              className="flex flex-1 flex-col items-stretch justify-end"
              title={`${entry.date}: ${entry.ok} ok, ${entry.error} err`}
            >
              <div className="flex flex-col-reverse">
                {entry.ok > 0 && (
                  <div
                    className="rounded-t bg-green-500"
                    style={{ height: `${Math.max(okH, 2)}%`, minHeight: 2 }}
                  />
                )}
                {entry.error > 0 && (
                  <div
                    className="bg-red-500"
                    style={{ height: `${Math.max(errH, 2)}%`, minHeight: 2 }}
                  />
                )}
              </div>
            </div>
          )
        })}
      </div>
      <div className="flex gap-1.5">
        {data.map((entry) => (
          <div
            key={entry.date}
            className="flex-1 text-center text-[10px] text-muted-foreground"
          >
            {dayjs(entry.date).format("MM/DD")}
          </div>
        ))}
      </div>
      <div className="flex items-center gap-4 text-xs text-muted-foreground">
        <span className="flex items-center gap-1">
          <span className="inline-block size-2 rounded-sm bg-green-500" />
          {t("pages.cron.stats.success")}
        </span>
        <span className="flex items-center gap-1">
          <span className="inline-block size-2 rounded-sm bg-red-500" />
          {t("pages.cron.stats.errors")}
        </span>
      </div>
    </div>
  )
}

export function CronStats() {
  const { t } = useTranslation()

  const { data: stats, isLoading: statsLoading } = useQuery({
    queryKey: ["cron-stats"],
    queryFn: getCronStats,
    refetchInterval: 30000,
  })

  const { data: trendData, isLoading: trendLoading } = useQuery({
    queryKey: ["cron-trend"],
    queryFn: () => getCronTrend(7),
    refetchInterval: 30000,
  })

  const trend = trendData?.trend ?? []

  return (
    <div className="space-y-4">
      <div className="flex gap-3">
        <StatCard
          label={t("pages.cron.stats.totalJobs")}
          value={String(stats?.totalJobs ?? 0)}
          loading={statsLoading}
        />
        <StatCard
          label={t("pages.cron.stats.enabledJobs")}
          value={String(stats?.enabledJobs ?? 0)}
          loading={statsLoading}
        />
        <StatCard
          label={t("pages.cron.stats.runs24h")}
          value={String(stats?.runs24h ?? 0)}
          loading={statsLoading}
        />
        <StatCard
          label={t("pages.cron.stats.successRate")}
          value={
            stats ? `${Math.round(stats.successRate24h * 100)}%` : "0%"
          }
          loading={statsLoading}
        />
      </div>

      <div className="rounded-lg border bg-card p-4">
        <div className="mb-3 text-sm font-medium">
          {t("pages.cron.stats.trend7d")}
        </div>
        <TrendChart data={trend} loading={trendLoading} t={t} />
      </div>
    </div>
  )
}
