import { useTranslation } from "react-i18next"
import dayjs from "dayjs"

import type { ExecutionRecord } from "@/api/cron"
import { Badge } from "@/components/ui/badge"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

interface CronOutputDialogProps {
  record: ExecutionRecord | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function CronOutputDialog({
  record,
  open,
  onOpenChange,
}: CronOutputDialogProps) {
  const { t } = useTranslation()

  if (!record) return null

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{record.jobName}</DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <span className="text-muted-foreground">
              {dayjs(record.startedAt).format("YYYY-MM-DD HH:mm:ss")}
            </span>
            <Badge variant="secondary">
              {record.trigger === "scheduled"
                ? t("pages.cron.history.scheduled")
                : t("pages.cron.history.manual")}
            </Badge>
            {record.status === "ok" ? (
              <Badge variant="default">{t("pages.cron.status.ok")}</Badge>
            ) : (
              <Badge variant="destructive">
                {t("pages.cron.status.error")}
              </Badge>
            )}
            <span className="text-muted-foreground">
              {formatDuration(record.durationMs)}
            </span>
          </div>

          {record.status === "error" && record.errorMsg && (
            <div className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">
              {record.errorMsg}
            </div>
          )}

          {record.output && (
            <pre className="max-h-80 overflow-auto rounded-md bg-muted p-3 font-mono text-xs leading-relaxed">
              {record.output}
            </pre>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  const s = ms / 1000
  if (s < 60) return `${s.toFixed(1)}s`
  const m = Math.floor(s / 60)
  const rem = Math.round(s % 60)
  return `${m}m ${rem}s`
}
