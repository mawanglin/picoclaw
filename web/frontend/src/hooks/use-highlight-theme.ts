import { useEffect } from "react"

import githubDarkCss from "highlight.js/styles/github-dark.css?inline"
import githubLightCss from "highlight.js/styles/github.css?inline"

const THEME_STYLE_ID = "hljs-theme-style"

function getOrCreateThemeStyleElement() {
  let styleElement = document.getElementById(THEME_STYLE_ID)
  if (!styleElement) {
    styleElement = document.createElement("style")
    styleElement.id = THEME_STYLE_ID
    document.head.appendChild(styleElement)
  }
  return styleElement
}

export function useHighlightTheme() {
  useEffect(() => {
    const root = document.documentElement
    const styleElement = getOrCreateThemeStyleElement()

    const applyTheme = () => {
      const nextThemeCss = root.classList.contains("dark")
        ? githubDarkCss
        : githubLightCss
      styleElement.textContent = nextThemeCss
    }

    applyTheme()

    const observer = new MutationObserver(() => {
      applyTheme()
    })

    observer.observe(root, {
      attributes: true,
      attributeFilter: ["class"],
    })

    return () => {
      observer.disconnect()
    }
  }, [])
}
