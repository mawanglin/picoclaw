import * as React from "react"
import { useTranslation } from "react-i18next"

import type { CronJob } from "@/api/cron"
import { CronHistory } from "@/components/cron/cron-history"
import { CronJobForm } from "@/components/cron/cron-job-form"
import { CronJobList } from "@/components/cron/cron-job-list"
import { CronStats } from "@/components/cron/cron-stats"

export function CronPage() {
  const { t } = useTranslation()
  const [activeTab, setActiveTab] = React.useState<"jobs" | "history">("jobs")
  const [formOpen, setFormOpen] = React.useState(false)
  const [editingJob, setEditingJob] = React.useState<CronJob | null>(null)

  const handleNew = React.useCallback(() => {
    setEditingJob(null)
    setFormOpen(true)
  }, [])

  const handleEdit = React.useCallback((job: CronJob) => {
    setEditingJob(job)
    setFormOpen(true)
  }, [])

  const handleFormClose = React.useCallback((open: boolean) => {
    setFormOpen(open)
    if (!open) {
      setEditingJob(null)
    }
  }, [])

  return (
    <div className="mx-auto w-full max-w-4xl space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">
          {t("pages.cron.title")}
        </h1>
      </div>

      <CronStats />

      <div className="flex gap-1 border-b">
        <button
          type="button"
          className={`px-4 py-2 text-sm font-medium transition-colors ${
            activeTab === "jobs"
              ? "border-foreground text-foreground border-b-2"
              : "text-muted-foreground hover:text-foreground"
          }`}
          onClick={() => setActiveTab("jobs")}
        >
          {t("pages.cron.tabs.jobs")}
        </button>
        <button
          type="button"
          className={`px-4 py-2 text-sm font-medium transition-colors ${
            activeTab === "history"
              ? "border-foreground text-foreground border-b-2"
              : "text-muted-foreground hover:text-foreground"
          }`}
          onClick={() => setActiveTab("history")}
        >
          {t("pages.cron.tabs.history")}
        </button>
      </div>

      {activeTab === "jobs" && (
        <CronJobList onEdit={handleEdit} onNew={handleNew} />
      )}

      {activeTab === "history" && <CronHistory />}

      <CronJobForm
        open={formOpen}
        onOpenChange={handleFormClose}
        job={editingJob}
      />
    </div>
  )
}
