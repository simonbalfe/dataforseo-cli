# dataforseo-cli

DataForSEO v3 CLI with concise SEO workflows and complete API access driven by DataForSEO's official OpenAPI specification.

## Setup

Private repository install with an authenticated GitHub CLI:

```sh
gh api repos/simonbalfe/dataforseo-cli/contents/install.sh \
  -H "Accept: application/vnd.github.raw+json" | sh
```

For a public repository, the equivalent one-line installer is:

```sh
curl -fsSL https://raw.githubusercontent.com/simonbalfe/dataforseo-cli/main/install.sh | sh
```

The installer downloads the latest native binary for macOS or Linux into `~/.local/bin`. Override the destination or version with `DFS_INSTALL_DIR` and `DFS_VERSION`.

Build from source:

```
go install .
```

Auth via `DATAFORSEO_USERNAME` / `DATAFORSEO_PASSWORD` (already in `~/.zshrc`).

- `main.go` contains the native Go CLI and direct REST client.
- Authentication uses HTTP Basic auth with API credentials.
- Convenience commands use synchronous live endpoints. `dfs api` exposes all live and task-based endpoints.

## Commands

```
dfs serp <keyword>              Google organic SERP
dfs volume <kw...>              Google Ads search volume
dfs suggestions <keyword>       keyword suggestions with volume + difficulty
dfs ranked <domain>             keywords a domain ranks for
dfs competitors <domain>        competitor domains by keyword overlap
dfs difficulty <kw...>          bulk keyword difficulty
dfs local-rank <kw> <target>    find a local Maps rank by coordinate pin or location
dfs local-grid <kw> <target>    run a concurrent local ranking grid
dfs balance                     account balance
dfs api list [query]            search all official OpenAPI operations
dfs api describe <path>         inspect an endpoint definition
dfs api request <method> <path> call any endpoint
```

All keyword/domain commands take `-l/--location` (default "United Kingdom"), `-g/--language` (default "en"), `-n/--limit`, and `--json` for raw output.

## Examples

```
dfs serp "tiktok api" -l "United States"
dfs volume "tiktok api" "instagram scraper" -l "United States"
dfs suggestions "instagram api" -n 50
dfs ranked creatorcrawl.com -l "United States"
dfs competitors creatorcrawl.com
dfs local-rank "dentist" "example dental" -l "London,England,United Kingdom"
dfs local-rank "dentist" "example.com" --pin "51.5074,-0.1278,14"
dfs local-grid "device repair near me" "IT Magic Phone Repairs" --pin "52.4203085,-0.8049749" --size 5 --spacing 1
dfs api list backlinks
dfs api describe /v3/serp/google/maps/live/advanced
dfs api request GET /v3/appendix/user_data
```

`local-rank` searches Google Maps results and matches the target against business title, domain, URL, phone, place ID, or CID. `--pin` accepts `latitude,longitude[,zoom]` and defaults to zoom 17. Otherwise, `--location` must be a full name returned by `/v3/serp/google/locations`.

`local-grid` sends every grid point concurrently through the Live Maps endpoint. A 5×5 grid defaults to 25 simultaneous requests. Use `--concurrency` to lower the request pressure, `--size` for an odd grid width, and `--spacing` for kilometres between points.

The OpenAPI discovery commands load the current specification from the official `dataforseo/OpenApiDocumentation` repository. Pass `--spec file.yaml` for a pinned local copy.

## Cost notes

Live SERP advanced ~$0.002/call at depth 10. Labs live endpoints ~$0.01-0.02/call. `volume` (Google Ads) is cheap per batch — pass many keywords in one call.
