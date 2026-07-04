#!/usr/bin/env tsx
import { Command } from "commander"

const BASE = "https://api.dataforseo.com/v3"

function auth(): string {
  const user = process.env.DATAFORSEO_USERNAME
  const pass = process.env.DATAFORSEO_PASSWORD
  if (!user || !pass) {
    console.error("Set DATAFORSEO_USERNAME and DATAFORSEO_PASSWORD")
    process.exit(1)
  }
  return Buffer.from(`${user}:${pass}`).toString("base64")
}

async function post(path: string, payload: Record<string, unknown>): Promise<any[]> {
  const res = await fetch(`${BASE}${path}`, {
    method: "POST",
    headers: { Authorization: `Basic ${auth()}`, "Content-Type": "application/json" },
    body: JSON.stringify([payload]),
  })
  const data: any = await res.json()
  const task = data.tasks?.[0]
  if (data.status_code !== 20000 || !task || task.status_code !== 20000) {
    console.error(`API error: ${task?.status_message ?? data.status_message}`)
    process.exit(1)
  }
  return task.result ?? []
}

async function get(path: string): Promise<any[]> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { Authorization: `Basic ${auth()}` },
  })
  const data: any = await res.json()
  return data.tasks?.[0]?.result ?? []
}

function table(rows: Record<string, unknown>[]): void {
  if (rows.length === 0) console.log("no rows")
  else console.table(rows)
}

function locale(opts: { location: string; language: string }) {
  return { location_name: opts.location, language_code: opts.language }
}

function withLocale(cmd: Command): Command {
  return cmd
    .option("-l, --location <name>", "location name", "United Kingdom")
    .option("-g, --language <code>", "language code", "en")
}

const program = new Command()
program.name("dfs").description("DataForSEO CLI")

withLocale(
  program
    .command("serp <keyword>")
    .description("Google organic SERP, live")
    .option("-n, --limit <n>", "results", "10")
    .option("--json", "raw JSON"),
).action(async (keyword: string, opts) => {
  const result = await post("/serp/google/organic/live/advanced", {
    keyword,
    ...locale(opts),
    depth: Number(opts.limit) <= 10 ? 10 : 100,
  })
  const items = (result[0]?.items ?? [])
    .filter((i: any) => i.type === "organic")
    .slice(0, Number(opts.limit))
  if (opts.json) console.log(JSON.stringify(items, null, 2))
  else
    table(
      items.map((i: any) => ({
        pos: i.rank_absolute,
        domain: i.domain,
        title: (i.title ?? "").slice(0, 60),
        url: (i.url ?? "").slice(0, 70),
      })),
    )
})

withLocale(
  program
    .command("volume <keywords...>")
    .description("Google Ads search volume")
    .option("--json", "raw JSON"),
).action(async (keywords: string[], opts) => {
  const result = await post("/keywords_data/google_ads/search_volume/live", {
    keywords,
    ...locale(opts),
  })
  if (opts.json) console.log(JSON.stringify(result, null, 2))
  else
    table(
      result.map((r: any) => ({
        keyword: r.keyword,
        volume: r.search_volume,
        cpc: r.cpc,
        competition: r.competition,
      })),
    )
})

withLocale(
  program
    .command("suggestions <keyword>")
    .description("keyword suggestions (Labs)")
    .option("-n, --limit <n>", "results", "25")
    .option("--json", "raw JSON"),
).action(async (keyword: string, opts) => {
  const result = await post("/dataforseo_labs/google/keyword_suggestions/live", {
    keyword,
    ...locale(opts),
    limit: Number(opts.limit),
    include_seed_keyword: true,
  })
  const items = result[0]?.items ?? []
  if (opts.json) console.log(JSON.stringify(items, null, 2))
  else
    table(
      items.map((i: any) => ({
        keyword: i.keyword,
        volume: i.keyword_info?.search_volume,
        cpc: i.keyword_info?.cpc,
        difficulty: i.keyword_properties?.keyword_difficulty,
      })),
    )
})

withLocale(
  program
    .command("ranked <domain>")
    .description("keywords a domain ranks for (Labs)")
    .option("-n, --limit <n>", "results", "25")
    .option("--json", "raw JSON"),
).action(async (domain: string, opts) => {
  const result = await post("/dataforseo_labs/google/ranked_keywords/live", {
    target: domain,
    ...locale(opts),
    limit: Number(opts.limit),
    order_by: ["keyword_data.keyword_info.search_volume,desc"],
  })
  const items = result[0]?.items ?? []
  if (opts.json) console.log(JSON.stringify(items, null, 2))
  else
    table(
      items.map((i: any) => ({
        keyword: i.keyword_data?.keyword,
        pos: i.ranked_serp_element?.serp_item?.rank_absolute,
        volume: i.keyword_data?.keyword_info?.search_volume,
        url: (i.ranked_serp_element?.serp_item?.relative_url ?? "").slice(0, 50),
      })),
    )
})

withLocale(
  program
    .command("competitors <domain>")
    .description("competitor domains by keyword overlap (Labs)")
    .option("-n, --limit <n>", "results", "15")
    .option("--json", "raw JSON"),
).action(async (domain: string, opts) => {
  const result = await post("/dataforseo_labs/google/competitors_domain/live", {
    target: domain,
    ...locale(opts),
    limit: Number(opts.limit),
  })
  const items = result[0]?.items ?? []
  if (opts.json) console.log(JSON.stringify(items, null, 2))
  else
    table(
      items.map((i: any) => ({
        domain: i.domain,
        overlap: i.intersections,
        keywords: i.full_domain_metrics?.organic?.count,
        etv: Math.round(i.full_domain_metrics?.organic?.etv ?? 0),
      })),
    )
})

withLocale(
  program
    .command("difficulty <keywords...>")
    .description("bulk keyword difficulty (Labs)")
    .option("--json", "raw JSON"),
).action(async (keywords: string[], opts) => {
  const result = await post("/dataforseo_labs/google/bulk_keyword_difficulty/live", {
    keywords,
    ...locale(opts),
  })
  const items = result[0]?.items ?? []
  if (opts.json) console.log(JSON.stringify(items, null, 2))
  else
    table(items.map((i: any) => ({ keyword: i.keyword, difficulty: i.keyword_difficulty })))
})

program
  .command("balance")
  .description("account balance")
  .action(async () => {
    const result = await get("/appendix/user_data")
    console.log(`balance: $${result[0]?.money?.balance}`)
  })

program.parseAsync()
