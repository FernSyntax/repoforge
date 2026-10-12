import { defineConfig } from "astro/config";

// Minimal production config. No adapters, no integrations: the output
// is a static site. Canonical URLs need a `site` value at deploy time;
// set it when the deployment hostname is known.
export default defineConfig({});
