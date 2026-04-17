import * as React from "react"
import { useTranslation } from "react-i18next"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"

import { createCronJob, updateCronJob } from "@/api/cron"
import type { CronJob, CronSchedule } from "@/api/cron"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Label } from "@/components/ui/label"

type ScheduleType = "oneTime" | "recurring" | "cron"

interface CronJobFormProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  job: CronJob | null
}

function inferScheduleType(schedule?: CronSchedule): ScheduleType {
  if (!schedule) return "recurring"
  if (schedule.kind === "at") return "oneTime"
  if (schedule.kind === "cron") return "cron"
  return "recurring"
}

function toLocalDatetime(ms?: number): string {
  if (!ms) return ""
  const d = new Date(ms)
  const offset = d.getTimezoneOffset()
  const local = new Date(d.getTime() - offset * 60000)
  return local.toISOString().slice(0, 16)
}

export function CronJobForm({ open, onOpenChange, job }: CronJobFormProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const isEdit = job !== null

  const [name, setName] = React.useState("")
  const [scheduleType, setScheduleType] = React.useState<ScheduleType>("recurring")
  const [datetime, setDatetime] = React.useState("")
  const [everyValue, setEveryValue] = React.useState("60")
  const [everyUnit, setEveryUnit] = React.useState<"minutes" | "hours" | "days">("minutes")
  const [cronExpr, setCronExpr] = React.useState("")
  const [timezone, setTimezone] = React.useState("")
  const [message, setMessage] = React.useState("")
  const [command, setCommand] = React.useState("")
  const [channel, setChannel] = React.useState("")
  const [to, setTo] = React.useState("")
  const [showAdvanced, setShowAdvanced] = React.useState(false)

  React.useEffect(() => {
    if (open) {
      if (job) {
        setName(job.name)
        setScheduleType(inferScheduleType(job.schedule))
        setDatetime(toLocalDatetime(job.schedule.atMs ?? undefined))
        if (job.schedule.kind === "every" && job.schedule.everyMs) {
          const mins = Math.round(job.schedule.everyMs / 60000)
          if (mins >= 1440 && mins % 1440 === 0) {
            setEveryValue(String(mins / 1440))
            setEveryUnit("days")
          } else if (mins >= 60 && mins % 60 === 0) {
            setEveryValue(String(mins / 60))
            setEveryUnit("hours")
          } else {
            setEveryValue(String(mins))
            setEveryUnit("minutes")
          }
        }
        setCronExpr(job.schedule.expr ?? "")
        setTimezone(job.schedule.tz ?? "")
        setMessage(job.payload.message)
        setCommand(job.payload.command ?? "")
        setChannel(job.payload.channel ?? "")
        setTo(job.payload.to ?? "")
        setShowAdvanced(job.schedule.kind === "cron")
      } else {
        setName("")
        setScheduleType("recurring")
        setDatetime("")
        setEveryValue("60")
        setEveryUnit("minutes")
        setCronExpr("")
        setTimezone("")
        setMessage("")
        setCommand("")
        setChannel("")
        setTo("")
        setShowAdvanced(false)
      }
    }
  }, [open, job])

  function buildSchedule(): CronSchedule {
    if (scheduleType === "oneTime") {
      return { kind: "at", atMs: new Date(datetime).getTime() }
    }
    if (scheduleType === "cron") {
      return {
        kind: "cron",
        expr: cronExpr,
        ...(timezone ? { tz: timezone } : {}),
      }
    }
    const num = Number(everyValue) || 60
    const multiplier =
      everyUnit === "days" ? 86400000 : everyUnit === "hours" ? 3600000 : 60000
    return { kind: "every", everyMs: num * multiplier }
  }

  const mutation = useMutation({
    mutationFn: (payload: {
      name: string
      schedule: CronSchedule
      payload: { message: string; command?: string; channel?: string; to?: string }
    }) => {
      if (isEdit && job) {
        return updateCronJob(job.id, payload)
      }
      return createCronJob(payload)
    },
    onSuccess: () => {
      toast.success(
        isEdit ? t("pages.cron.toast.updated") : t("pages.cron.toast.created")
      )
      queryClient.invalidateQueries({ queryKey: ["cron-jobs"] })
      onOpenChange(false)
    },
    onError: () => toast.error(t("pages.cron.toast.error")),
  })

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim() || !message.trim()) return
    mutation.mutate({
      name: name.trim(),
      schedule: buildSchedule(),
      payload: {
        message: message.trim(),
        ...(command.trim() ? { command: command.trim() } : {}),
        ...(channel.trim() ? { channel: channel.trim() } : {}),
        ...(to.trim() ? { to: to.trim() } : {}),
      },
    })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {isEdit
              ? t("pages.cron.actions.edit")
              : t("pages.cron.actions.create")}
          </DialogTitle>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          {/* Name */}
          <div className="space-y-1.5">
            <Label>{t("pages.cron.form.name")}</Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
            />
          </div>

          {/* Schedule */}
          <div className="space-y-2">
            <Label>{t("pages.cron.form.schedule")}</Label>

            {!showAdvanced ? (
              <>
                <div className="flex gap-2">
                  {(
                    ["oneTime", "recurring", "cron"] as const
                  ).map((st) => (
                    <button
                      key={st}
                      type="button"
                      className={`rounded-md border px-3 py-1.5 text-xs font-medium transition-colors ${
                        scheduleType === st
                          ? "border-foreground bg-foreground text-background"
                          : "border-border text-muted-foreground hover:text-foreground"
                      }`}
                      onClick={() => setScheduleType(st)}
                    >
                      {t(`pages.cron.form.${st}`)}
                    </button>
                  ))}
                </div>

                {scheduleType === "oneTime" && (
                  <Input
                    type="datetime-local"
                    value={datetime}
                    onChange={(e) => setDatetime(e.target.value)}
                    required
                  />
                )}

                {scheduleType === "recurring" && (
                  <div className="flex gap-2">
                    <Input
                      type="number"
                      min="1"
                      className="w-24"
                      value={everyValue}
                      onChange={(e) => setEveryValue(e.target.value)}
                    />
                    <select
                      className="rounded-md border border-border bg-background px-3 py-1.5 text-sm"
                      value={everyUnit}
                      onChange={(e) =>
                        setEveryUnit(
                          e.target.value as "minutes" | "hours" | "days"
                        )
                      }
                    >
                      <option value="minutes">min</option>
                      <option value="hours">hr</option>
                      <option value="days">day</option>
                    </select>
                  </div>
                )}

                {scheduleType === "cron" && (
                  <div className="space-y-2">
                    <Input
                      placeholder={t("pages.cron.form.cronExpr")}
                      value={cronExpr}
                      onChange={(e) => setCronExpr(e.target.value)}
                      className="font-mono"
                      required
                    />
                    <Input
                      placeholder={t("pages.cron.form.timezone")}
                      value={timezone}
                      onChange={(e) => setTimezone(e.target.value)}
                    />
                  </div>
                )}
              </>
            ) : (
              <div className="space-y-2">
                <Input
                  placeholder={t("pages.cron.form.cronExpr")}
                  value={cronExpr}
                  onChange={(e) => setCronExpr(e.target.value)}
                  className="font-mono"
                  required
                />
                <Input
                  placeholder={t("pages.cron.form.timezone")}
                  value={timezone}
                  onChange={(e) => setTimezone(e.target.value)}
                />
              </div>
            )}

            <button
              type="button"
              className="text-xs text-muted-foreground hover:text-foreground transition-colors"
              onClick={() => {
                setShowAdvanced((v) => !v)
                if (!showAdvanced) setScheduleType("cron")
              }}
            >
              {t("pages.cron.form.advanced")}
            </button>
          </div>

          {/* Payload */}
          <div className="space-y-1.5">
            <Label>{t("pages.cron.form.message")}</Label>
            <Textarea
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              rows={3}
              required
            />
          </div>

          <div className="space-y-1.5">
            <Label>{t("pages.cron.form.command")}</Label>
            <Input
              value={command}
              onChange={(e) => setCommand(e.target.value)}
              placeholder={t("pages.cron.form.command")}
            />
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label>{t("pages.cron.form.channel")}</Label>
              <Input
                value={channel}
                onChange={(e) => setChannel(e.target.value)}
              />
            </div>
            <div className="space-y-1.5">
              <Label>{t("pages.cron.form.to")}</Label>
              <Input value={to} onChange={(e) => setTo(e.target.value)} />
            </div>
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
            >
              {t("pages.cron.actions.cancel")}
            </Button>
            <Button type="submit" disabled={mutation.isPending}>
              {t("pages.cron.actions.save")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
