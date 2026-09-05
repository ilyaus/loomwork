import {useEffect, useState} from "react";

export type ThemePreference = "system" | "light" | "dark";

const storageKey = "loomwork.theme";
const query = "(prefers-color-scheme: dark)";

function readPreference(): ThemePreference {
  const stored = localStorage.getItem(storageKey);
  return stored === "light" || stored === "dark" ? stored : "system";
}

function resolve(preference: ThemePreference): "light" | "dark" {
  if (preference !== "system") return preference;
  return window.matchMedia(query).matches ? "dark" : "light";
}

// useTheme applies the resolved theme to <html data-theme> and follows the OS
// setting while the preference is "system".
export function useTheme() {
  const [preference, setPreference] = useState<ThemePreference>(readPreference);
  const [resolved, setResolved] = useState(() => resolve(preference));

  useEffect(() => {
    localStorage.setItem(storageKey, preference);
    const apply = () => setResolved(resolve(preference));
    apply();
    const media = window.matchMedia(query);
    media.addEventListener("change", apply);
    return () => media.removeEventListener("change", apply);
  }, [preference]);

  useEffect(() => {
    document.documentElement.dataset.theme = resolved;
  }, [resolved]);

  return {preference, resolved, setPreference};
}
