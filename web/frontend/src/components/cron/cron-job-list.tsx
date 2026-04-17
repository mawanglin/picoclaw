import * as React from "react"
import { useTranslation } from "react-i18next"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import {
  IconPlayerPlay,
  IconPencil,
  IconTrash,
  IconToggleLeft,
  IconToggleRight,
  IconPlus,
} from "@tabler/icons-react"
import dayjs from "dayjs"
import relativeTime from "dayjs/plugin/relativeTime"
import { toast } from "sonner"

import {
  listCronJobs,
  deleteCronJob,
  enableCronJob,
  disableCronJob,
  triggerCronJob,
} from "@/api/cron"
import type { CronJob, CronSchedule } from "@/api/cron"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"

dayjs.extend(relativeTime)

function formatSchedule(schedule: CronSchedule): string {
  if (schedule.kind === "cron" && schedule.expr) {
    return schedule.expr
  }
  if (schedule.kind === "at" && schedule.atMs) {
    return dayjs(schedule.atMs).format("YYYY-MM-DD HH:mm")
  }
  if (schedule.kind === "every" && schedule.everyMs) {
    const mins = Math.round(schedule.everyMs / 60000)
    if (mins < 60) return `Every ${mins}m`
    const hrs = Math.round(mins / 60)
    if (hrs < 24) return `Every ${hrs}h`
    return `Every ${Math.round(hrs / 24)}d`
  }
  return "-"
}

interface CronJobListProps {
  onEdit: (job: CronJob) => void
  onNew: () => void
}

export function CronJobList({ onEdit, onNew }: CronJobListProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [deleteTarget, setDeleteTarget] = React.useState<CronJob | null>(null)

  const { data: jobs = [], isLoading } = useQuery({
    queryKey: ["cron-jobs"],
    queryFn: listCronJobs,
  })

  const invalidate = React.useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ["cron-jobs"] })
  }, [queryClient])

  const deleteMutation = useMutation({
    mutationFn: deleteCronJob,
    onSuccess: () => {
      toast.success(t("pages.cron.toast.deleted"))
      invalidate()
    },
    onError: () => toast.error(t("pages.cron.toast.error")),
  })

  const enableMutation = useMutation({
    mutationFn: enableCronJob,
    onSuccess: () => {
      toast.success(t("pages.cron.toast.enabled"))
      invalidate()
    },
    onError: () => toast.error(t("pages.cron.toast.error")),
  })

  const disableMutation = useMutation({
    mutationFn: disableCronJob,
    onSuccess: () => {
      toast.success(t("pages.cron.toast.disabled"))
      invalidate()
    },
    onError: () => toast.error(t("pages.cron.toast.error")),
  })

  const triggerMutation = useMutation({
    mutationFn: triggerCronJob,
    onSuccess: () => {
      toast.success(t("pages.cron.toast.triggered"))
      invalidate()
    },
    onError: () => toast.error(t("pages.cron.toast.error")),
  })

  if (isLoading) {
    return (
      <div className="flex min-h-[200px] items-center justify-center text-muted-foreground">
        {t("labels.loading")}
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-end">
        <Button size="sm" onClick={onNew}>
          <IconPlus className="mr-1.5 size-4" />
          {t("pages.cron.actions.create")}
        </Button>
      </div>

      {jobs.length === 0 ? (
        <div className="flex min-h-[200px] items-center justify-center text-muted-foreground">
          {t("pages.cron.empty.noJobs")}
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b bg-muted/40">
                <th className="px-4 py-2.5 text-left font-medium">
                  {t("pages.cron.table.name")}
                </th>
                <th className="px-4 py-2.5 text-left font-medium">
                  {t("pages.cron.table.schedule")}
                </th>
                <th className="px-4 py-2.5 text-left font-medium">
                  {t("pages.cron.table.channel")}
                </th>
                <th className="px-4 py-2.5 text-left font-medium">
                  {t("pages.cron.table.lastRun")}
                </th>
                <th className="px-4 py-2.5 text-left font-medium">
                  {t("pages.cron.table.status")}
                </th>
                <th className="px-4 py-2.5 text-right font-medium">
                  {t("pages.cron.table.actions")}
                </th>
              </tr>
            </thead>
            <tbody>
              {jobs.map((job) => (
                <tr
                  key={job.id}
                  className="border-b last:border-b-0 hover:bg-muted/20 transition-colors"
                >
                  <td className="px-4 py-2.5">
                    <div className="flex items-center gap-2">
                      <span
                        className={`inline-block size-2 rounded-full ${
                          job.enabled ? "bg-green-500" : "bg-gray-400"
                        }`}
                      />
                      <span className="font-medium">{job.name}</span>
                    </div>
                  </td>
                  <td className="px-4 py-2.5 font-mono text-xs text-muted-foreground">
                    {formatSchedule(job.schedule)}
                  </td>
                  <td className="px-4 py-2.5">
                    {job.payload.channel ? (
                      <Badge variant="secondary">{job.payload.channel}</Badge>
                    ) : (
                      <span className="text-muted-foreground">-</span>
                    )}
                  </td>
                  <td className="px-4 py-2.5 text-muted-foreground">
                    {job.state.lastRunAtMs
                      ? dayjs(job.state.lastRunAtMs).fromNow()
                      : "-"}
                  </td>
                  <td className="px-4 py-2.5">
                    {job.state.lastStatus === "ok" ? (
                      <Badge variant="default">
                        {t("pages.cron.status.ok")}
                      </Badge>
                    ) : job.state.lastStatus === "error" ? (
                      <Badge variant="destructive">
                        {t("pages.cron.status.error")}
                      </Badge>
                    ) : (
                      <span className="text-muted-foreground">-</span>
                    )}
                  </td>
                  <td className="px-4 py-2.5">
                    <div className="flex items-center justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        onClick={() => triggerMutation.mutate(job.id)}
                        title={t("pages.cron.actions.trigger")}
                      >
                        <IconPlayerPlay className="size-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        onClick={() => onEdit(job)}
                        title={t("pages.cron.actions.edit")}
                      >
                        <IconPencil className="size-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        onClick={() =>
                          job.enabled
                            ? disableMutation.mutate(job.id)
                            : enableMutation.mutate(job.id)
                        }
                        title={
                          job.enabled
                            ? t("pages.cron.actions.disable")
                            : t("pages.cron.actions.enable")
                        }
                      >
                        {job.enabled ? (
                          <IconToggleRight className="size-4 text-green-500" />
                        ) : (
                          <IconToggleLeft className="size-4" />
                        )}
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        onClick={() => setDeleteTarget(job)}
                        title={t("pages.cron.actions.delete")}
                      >
                        <IconTrash className="size-4" />
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <AlertDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("pages.cron.actions.delete")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {deleteTarget?.name}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (deleteTarget) {
                  deleteMutation.mutate(deleteTarget.id)
                  setDeleteTarget(null)
                }
              }}
            >
              {t("pages.cron.actions.confirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
