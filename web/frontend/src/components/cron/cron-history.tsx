import * as React from "react"
import { useTranslation } from "react-i18next"
import { useQuery } from "@tanstack/react-query"
import dayjs from "dayjs"

import { getCronHistory, listCronJobs } from "@/api/cron"
import type { ExecutionRecord } from "@/api/cron"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { CronOutputDialog } from "@/components/cron/cron-output-dialog"

const PAGE_SIZE = 20

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  const s = ms / 1000
  if (s < 60) return `${s.toFixed(1)}s`
  const m = Math.floor(s / 60)
  const rem = Math.round(s % 60)
  return `${m}m ${rem}s`
}

export function CronHistory() {
  const { t } = useTranslation()
  const [page, setPage] = React.useState(1)
  const [jobId, setJobId] = React.useState("")
  const [selectedRecord, setSelectedRecord] =
    React.useState<ExecutionRecord | null>(null)

  const { data: jobs = [] } = useQuery({
    queryKey: ["cron-jobs"],
    queryFn: listCronJobs,
  })

  const { data, isLoading } = useQuery({
    queryKey: ["cron-history", page, PAGE_SIZE, jobId],
    queryFn: () =>
      getCronHistory({ page, size: PAGE_SIZE, job_id: jobId || undefined }),
  })

  const records = data?.records ?? []
  const total = data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  // Reset page when filter changes
  const handleFilterChange = React.useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      setJobId(e.target.value)
      setPage(1)
    },
    [],
  )

  if (isLoading) {
    return (
      <div className="flex min-h-[200px] items-center justify-center text-muted-foreground">
        {t("labels.loading")}
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <select
          className="rounded-md border bg-background px-3 py-1.5 text-sm"
          value={jobId}
          onChange={handleFilterChange}
        >
          <option value="">{t("pages.cron.tabs.jobs")} - All</option>
          {jobs.map((job) => (
            <option key={job.id} value={job.id}>
              {job.name}
            </option>
          ))}
        </select>
      </div>

      {records.length === 0 ? (
        <div className="flex min-h-[200px] items-center justify-center text-muted-foreground">
          {t("pages.cron.empty.noHistory")}
        </div>
      ) : (
        <>
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b bg-muted/40">
                  <th className="px-4 py-2.5 text-left font-medium">
                    {t("pages.cron.table.name")}
                  </th>
                  <th className="px-4 py-2.5 text-left font-medium">
                    {t("pages.cron.table.lastRun")}
                  </th>
                  <th className="px-4 py-2.5 text-left font-medium">
                    {t("pages.cron.history.trigger")}
                  </th>
                  <th className="px-4 py-2.5 text-left font-medium">
                    {t("pages.cron.table.status")}
                  </th>
                  <th className="px-4 py-2.5 text-left font-medium">
                    {t("pages.cron.history.duration")}
                  </th>
                  <th className="px-4 py-2.5 text-right font-medium">
                    {t("pages.cron.history.output")}
                  </th>
                </tr>
              </thead>
              <tbody>
                {records.map((record) => (
                  <tr
                    key={record.id}
                    className="border-b last:border-b-0 transition-colors hover:bg-muted/20"
                  >
                    <td className="px-4 py-2.5 font-medium">
                      {record.jobName}
                    </td>
                    <td className="px-4 py-2.5 text-muted-foreground">
                      {dayjs(record.startedAt).format("MM-DD HH:mm:ss")}
                    </td>
                    <td className="px-4 py-2.5">
                      <Badge variant="secondary">
                        {record.trigger === "scheduled"
                          ? t("pages.cron.history.scheduled")
                          : t("pages.cron.history.manual")}
                      </Badge>
                    </td>
                    <td className="px-4 py-2.5">
                      {record.status === "ok" ? (
                        <Badge variant="default">
                          {t("pages.cron.status.ok")}
                        </Badge>
                      ) : (
                        <Badge variant="destructive">
                          {t("pages.cron.status.error")}
                        </Badge>
                      )}
                    </td>
                    <td className="px-4 py-2.5 font-mono text-xs text-muted-foreground">
                      {formatDuration(record.durationMs)}
                    </td>
                    <td className="px-4 py-2.5 text-right">
                      <button
                        type="button"
                        className="text-sm text-primary underline-offset-4 hover:underline"
                        onClick={() => setSelectedRecord(record)}
                      >
                        {t("pages.cron.history.view")}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">
              Page {page} of {totalPages}
            </span>
            <div className="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={page <= 1}
                onClick={() => setPage((p) => p - 1)}
              >
                Previous
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={page >= totalPages}
                onClick={() => setPage((p) => p + 1)}
              >
                Next
              </Button>
            </div>
          </div>
        </>
      )}

      <CronOutputDialog
        record={selectedRecord}
        open={selectedRecord !== null}
        onOpenChange={(open) => {
          if (!open) setSelectedRecord(null)
        }}
      />
    </div>
  )
}
