#!/usr/bin/env node
// Lighthouse audit against a local production build of the landing
// (deliverable 4, Phase 6 T6 / roadmap "Design pass ... Lighthouse >= 90").
//
// 1. builds the production bundle (`vite build`)
// 2. starts `vite preview` on :3100 (the same SSR artifact `test:e2e` runs against)
// 3. launches the Playwright-managed chromium (headless) with a fixed CDP
//    port, so this never downloads or depends on a separately installed
//    Chrome — `lighthouse` itself stays a devDependency only, never a
//    runtime one
// 4. audits the home page (`/uz`) and one product page, mobile emulation
//    (Lighthouse's own default form factor — no override needed)
// 5. prints performance/SEO/accessibility/best-practices scores per page
//    and writes an HTML report per page to `web/.lighthouse/` (gitignored)
// 6. exits 1 if performance or SEO is below 90 on either page (the
//    Done-when bar)
import { spawn } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { chromium } from "@playwright/test";
import lighthouse from "lighthouse";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(__dirname, "..");

const PORT = 3100;
const BASE_URL = `http://localhost:${PORT}`;
const API_URL = process.env.API_URL ?? "http://localhost:8080/v1";
const REMOTE_DEBUGGING_PORT = 9223;
const THRESHOLDS = { performance: 90, seo: 90 };
const REPORT_DIR = path.join(webRoot, ".lighthouse");

/** Runs a command to completion, inheriting stdio, rejecting on a non-zero exit. */
function run(command, args, env) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd: webRoot,
      stdio: "inherit",
      env: { ...process.env, ...env },
    });
    child.on("exit", (code) => {
      if (code === 0) resolve();
      else reject(new Error(`${command} ${args.join(" ")} exited with code ${code}`));
    });
    child.on("error", reject);
  });
}

/** Starts a long-running command in the background; caller kills it when done. */
function spawnServer(command, args, env) {
  const child = spawn(command, args, {
    cwd: webRoot,
    stdio: "inherit",
    env: { ...process.env, ...env },
  });
  return child;
}

async function waitForServer(url, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(url);
      if (res.ok) return;
    } catch {
      // not up yet
    }
    await new Promise((resolve) => setTimeout(resolve, 300));
  }
  throw new Error(`Server at ${url} did not become ready within ${timeoutMs}ms`);
}

/** Discovers one real product URL from the seeded catalogue instead of
 * hard-coding a slug, same independence rule as the e2e specs. */
async function discoverProductPath() {
  const res = await fetch(`${API_URL}/public/products?limit=1`, {
    headers: { "Accept-Language": "uz" },
  });
  if (!res.ok) {
    throw new Error(`GET ${API_URL}/public/products failed: ${res.status}`);
  }
  const body = await res.json();
  const slug = body.items?.[0]?.slug;
  if (!slug) {
    throw new Error("no product found in the seeded catalogue to audit");
  }
  return `/uz/p/${slug}`;
}

async function auditPage(browserPort, pathname, label) {
  const url = `${BASE_URL}${pathname}`;
  const result = await lighthouse(url, {
    port: browserPort,
    output: "html",
    logLevel: "error",
  });
  if (!result) {
    throw new Error(`lighthouse produced no result for ${url}`);
  }
  const { lhr, report } = result;
  const scores = {
    performance: Math.round((lhr.categories.performance?.score ?? 0) * 100),
    accessibility: Math.round((lhr.categories.accessibility?.score ?? 0) * 100),
    bestPractices: Math.round((lhr.categories["best-practices"]?.score ?? 0) * 100),
    seo: Math.round((lhr.categories.seo?.score ?? 0) * 100),
  };
  await mkdir(REPORT_DIR, { recursive: true });
  const reportPath = path.join(REPORT_DIR, `${label}.html`);
  await writeFile(reportPath, Array.isArray(report) ? report[0] : report);
  return { url, scores, reportPath };
}

async function main() {
  console.log("Building production bundle...");
  await run("pnpm", ["run", "build"], { API_URL, SITE_URL: BASE_URL });

  console.log("Starting preview server on :3100...");
  const server = spawnServer(
    "pnpm",
    ["exec", "vite", "preview", "--port", String(PORT), "--strictPort"],
    {
      API_URL,
      SITE_URL: BASE_URL,
    },
  );

  let browser;
  try {
    await waitForServer(BASE_URL, 30_000);

    const productPath = await discoverProductPath();

    console.log("Launching headless chromium (Playwright install)...");
    browser = await chromium.launch({
      headless: true,
      args: [`--remote-debugging-port=${REMOTE_DEBUGGING_PORT}`],
    });
    // Give Chromium a moment to open the CDP port before lighthouse connects.
    await new Promise((resolve) => setTimeout(resolve, 500));

    const pages = [
      { pathname: "/uz", label: "home" },
      { pathname: productPath, label: "product" },
    ];

    const results = [];
    for (const page of pages) {
      console.log(`Auditing ${page.pathname}...`);
      const result = await auditPage(REMOTE_DEBUGGING_PORT, page.pathname, page.label);
      results.push({ ...page, ...result });
    }

    console.log("\nLighthouse scores (mobile):");
    let failed = false;
    for (const result of results) {
      console.log(`\n${result.pathname} (${result.url})`);
      console.log(`  performance:     ${result.scores.performance}`);
      console.log(`  seo:             ${result.scores.seo}`);
      console.log(`  accessibility:   ${result.scores.accessibility}`);
      console.log(`  best-practices:  ${result.scores.bestPractices}`);
      console.log(`  report:          ${path.relative(webRoot, result.reportPath)}`);
      if (
        result.scores.performance < THRESHOLDS.performance ||
        result.scores.seo < THRESHOLDS.seo
      ) {
        failed = true;
      }
    }

    if (failed) {
      console.error(
        `\nFAIL: performance and SEO must both be >= ${THRESHOLDS.performance} on every page audited.`,
      );
      process.exitCode = 1;
    } else {
      console.log(
        `\nOK: performance and SEO are both >= ${THRESHOLDS.performance} on every page audited.`,
      );
    }
  } finally {
    if (browser) {
      await browser.close();
    }
    server.kill();
  }
}

main().catch((err) => {
  console.error(err);
  process.exitCode = 1;
});
