# dataforseo-cli

DataForSEO CLI. Replaces the `dataforseo` MCP server (npx dataforseo-mcp-server) with a direct wrapper around the v3 REST API using live endpoints only.

## Setup

```
bun install
```

Auth via `DATAFORSEO_USERNAME` / `DATAFORSEO_PASSWORD` (already in `~/.zshrc`).

- `src/cli.ts` — all commands. Basic-auth POSTs to `api.dataforseo.com/v3`, one task per call, live (synchronous) endpoints so every call returns immediately and is billed per request.

## Commands

```
dfs serp <keyword>              Google organic SERP
dfs volume <kw...>              Google Ads search volume
dfs suggestions <keyword>       keyword suggestions with volume + difficulty
dfs ranked <domain>             keywords a domain ranks for
dfs competitors <domain>        competitor domains by keyword overlap
dfs difficulty <kw...>          bulk keyword difficulty
dfs balance                     account balance
```

All keyword/domain commands take `-l/--location` (default "United Kingdom"), `-g/--language` (default "en"), `-n/--limit`, and `--json` for raw output.

## Examples

```
bun run dfs serp "tiktok api" -l "United States"
bun run dfs volume "tiktok api" "instagram scraper" -l "United States"
bun run dfs suggestions "instagram api" -n 50
bun run dfs ranked creatorcrawl.com -l "United States"
bun run dfs competitors creatorcrawl.com
```

## Cost notes

Live SERP advanced ~$0.002/call at depth 10. Labs live endpoints ~$0.01-0.02/call. `volume` (Google Ads) is cheap per batch — pass many keywords in one call.
