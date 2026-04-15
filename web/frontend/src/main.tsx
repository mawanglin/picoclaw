import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { RouterProvider, createRouter } from "@tanstack/react-router"
import { StrictMode } from "react"
import ReactDOM from "react-dom/client"

import { useHighlightTheme } from "./hooks/use-highlight-theme"
import "./i18n"
import "./index.css"
import { routeTree } from "./routeTree.gen"

const queryClient = new QueryClient()

const router = createRouter({
  routeTree,
  context: {
    queryClient,
  },
})

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router
  }
}

function AppProviders() {
  useHighlightTheme()

  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

const rootElement = document.getElementById("root")!
if (!rootElement.innerHTML) {
  const root = ReactDOM.createRoot(rootElement)
  root.render(
    <StrictMode>
      <AppProviders />
    </StrictMode>,
  )
}
