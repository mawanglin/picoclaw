import { createFileRoute } from "@tanstack/react-router"

import { CronPage } from "@/components/cron/cron-page"

export const Route = createFileRoute("/config/cron")({
  component: CronPage,
})
