# Clachan — Project Handoff

Last updated: 2026-10-29

- Repository: <https://github.com/SHOWTEX/ClaChan>
- Default branch: `main`
- Current release: `v1.0.4` (published)
- Default port: `7654`
- Default OpenRouter referer: `https://github.com/SHOWTEX/ClaChan`

This file is a handoff for a new coding agent. Do not put secrets in it.

## What Clachan is

A small, self-hosted AI chat for a trusted LAN. One static Go binary embeds the
vanilla JavaScript frontend and talks to OpenRouter. SQLite stores profiles,
sessions, conversations, messages, prompts, attachments, reasoning, and tool
steps.

It is designed for a 1 GB Raspberry Pi 3B+, but release builds also cover
`linux/amd64` and `windows/amd64`/`arm64`.

## Hard constraints

- Go 1.27+.
- Standard library `net/http`.
- Exactly two direct dependencies: `modernc.org/sqlite` and
  `golang.org/x/time/rate`.
- No cgo; builds must use `CGO_ENABLED=0`.
- Single static binary; frontend is embedded from `static/`.
- No frontend framework, npm, or build step.
- OpenRouter is the only upstream provider.
- Do not expose Clachan directly to the internet. It is LAN-only and has no
  built-in TLS.
- Never commit `.env`, API keys, `data/`, database files, or build binaries.

## Features implemented

- Streaming chat through OpenRouter's OpenResponses API (`POST /responses`).
- Per-profile chats, prompts, settings, persona, default model, ignored
  providers, and browser-scoped UI preferences.
- Real login/profile isolation: PBKDF2-HMAC-SHA256, 30-day HttpOnly sessions,
  owner-only profile creation, per-IP login throttling.
- Authenticated model catalogue from `/models/user` with pricing, context,
  Vision/Files/Tools/Reasoning metadata, search, filters, and sorting.
- Per-conversation provider routing: balanced, price, throughput, latency, or a
  pinned endpoint.
- Server tools: web search, web fetch, date/time, and model-catalogue search.
- Numbered assistant step cards with collapsible reasoning, arguments, results,
  citations, and a final answer card.
- Per-chat reasoning effort menu driven by `supported_efforts` and
  `default_effort`.
- Image, PDF, and text attachments. Images are downscaled in the browser.
  Attachments are stored as data URLs in SQLite.
- Prompt library, built-in system presets, named themes, file cards,
  Markdown/JSON export, JSON import, and browser print/PDF output.
- Cross-platform install/update/uninstall scripts and tagged GitHub releases.

## Architecture

- `main.go` — wiring, embedded static files, HTTP server, graceful shutdown.
- `config.go` — environment and minimal `.env` loader.
- `provider.go` — OpenRouter client, shared HTTP client, model and endpoint
  caches, provider headers.
- `responses.go` — OpenResponses event/item parsing, reasoning, step transcript,
  citations.
- `chat.go` — `POST /api/chat`: stream semaphore, SSE proxy, persistence.
- `tools.go` — server-tool payloads, tool-call limits, citations, summaries.
- `attachments.go` — attachment validation and OpenRouter content parts.
- `store.go` — SQLite schema, migrations, profile-scoped queries.
- `auth.go` — passwords, sessions, login, profile settings, middleware.
- `api.go` — conversation CRUD, prompts, models, health, import.
- `cli.go` — `-version` and `-reset-password`.
- `static/` — `index.html`, `style.css`, `app.js`; embedded, no bundler.
- `install.sh` / `install.ps1` — Linux/Windows installers.
- `.github/workflows/ci.yml` — format, vet, tests, installers, four-target build.
- `.github/workflows/release.yml` — tag-driven release with assets/checksums.

## OpenRouter details

- Chat uses `POST /responses`, not `/chat/completions`, so server-tool output
  and reasoning items are visible.
- Model catalogue uses `/models/user`; it falls back to the public `/models`
  list if the authenticated request fails.
- Requests send `HTTP-Referer: https://github.com/SHOWTEX/ClaChan` and
  `X-Title: Clachan`.
- The API key is a **server-wide** `OPENROUTER_API_KEY`. All profiles currently
  share one key and its credits.
- The browser never receives the key.
- Per-profile OpenRouter keys are **not implemented**. They were discussed as a
  future feature, ideally encrypted at rest with a master key and with model
  caches keyed by key fingerprint. Do not add plaintext key storage casually.

## Profiles and data

- The first profile created is the owner. Only the owner can create profiles.
- Password rules: 8–200 characters.
- Profile/user/assistant names: 64 characters.
- Every conversation, message, prompt, and setting query must be scoped by
  `profile_id`. New features must preserve this.
- SQLite runs in WAL mode with one connection.
- Schema migrations are applied automatically at startup.

## Current limits

These are the limits after v1.0.4:

| Area | Limit |
|---|---|
| Prompt/preset body | 1 MiB |
| Prompt title | 200 characters |
| Generic JSON API body | 2 MiB |
| Chat message | 1 MiB |
| System prompt/persona/user context | 1 MiB |
| Chat request body | 40 MiB |
| Conversation title | 200 runes; auto titles 60 |
| Model ID | 200 bytes |
| Conversation import body | 64 MiB |
| Imported messages | 2,000 |
| Imported attachments | 48 MiB total |
| Attachments per message | 6 |
| Image attachment | 8 MiB |
| PDF attachment | 12 MiB |
| Text/code attachment | 4 MiB |
| Total attachments | 24 MiB |
| Attachment filename | 120 runes |
| Tool calls per reply | 1–30, default 6 |
| Ignored providers | 50 entries |
| Concurrent streams | 2 |
| Chat rate | 1/second, burst 4, global |
| Login rate | 8 burst, then 1 per 15s per IP |
| Stream duration | 15 minutes, 90s idle |
| Tool result display | truncated to 12,000 bytes |
| Citation excerpt | truncated to 1,200 bytes |

Some limits are protective (attachments, imports, concurrency, login rate).
Others are product choices and can be raised if a user asks.

## Install / update / uninstall

Linux, systemd, 64-bit:

```bash
curl -fsSL https://github.com/SHOWTEX/ClaChan/releases/latest/download/install.sh | sudo bash
```

- Installs to `/opt/clachan`.
- Config: `/etc/clachan/clachan.env`.
- Data: `/opt/clachan/data`.
- Health check: `http://127.0.0.1:7654/healthz`.
- Port can be set for a fresh config with `--port`.
- Update by running the same command. Existing config/data are preserved.
- Uninstall keeps config/data by default; use `--uninstall --purge --yes` to
  remove everything.
- The installer checks for port conflicts before creating a new config and
  prints the conflicting listener.

Windows 10/11, x64 or ARM64:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -Command "irm https://github.com/SHOWTEX/ClaChan/releases/latest/download/install.ps1 | iex"
```

- Per-user install in `%LOCALAPPDATA%\Clachan`.
- Config: `%LOCALAPPDATA%\Clachan\.env`.
- Adds Start Menu entries, start-at-login task, and an uninstall entry.
- `-OpenFirewall` requires an elevated shell.
- Update by running the same command.
- `-Uninstall` keeps config/data; `-Uninstall -Purge -Yes` removes everything.
- The Windows binary is unsigned; SmartScreen may warn.

## Build and test

From the repo root:

```powershell
gofmt -l .
go vet ./...
go test -count=1 ./...
node --check static/app.js
.\build.ps1
```

`build.ps1` builds `linux/arm64` and injects the version from `git describe`.
Tagged releases build:

- `clachan-linux-amd64`
- `clachan-linux-arm64`
- `clachan-windows-amd64.exe`
- `clachan-windows-arm64.exe`

## Releases

Tags matching `v*` trigger `.github/workflows/release.yml`.

```bash
git tag -a v1.0.5 -m "v1.0.5"
git push origin main
git push origin v1.0.5
```

The workflow runs tests, builds all four targets, copies the installers,
writes `checksums.txt`, and publishes the release. Update the version in:

- `main.go`
- README version badge and examples
- CONTRIBUTING tag/build examples

Do not move a published tag. Ship a new patch release instead.

## Known quirks and caveats

- Existing configs keep their saved port. Changing the default does not migrate
  old installs. Edit `CLACHAN_ADDR` in the config or set a new port in a fresh
  config.
- `maxJSONBody` affects all API JSON endpoints; keep it above the largest
  prompt/message field if limits change.
- Prompt titles now count characters, not bytes. Keep that behavior.
- Windows uninstall confirmation defaults to yes; purge remains opt-in.
- The app has no built-in TLS. It is for trusted LANs only.
- The OpenRouter key is shared by every profile.
- A key was exposed during development/setup; never restore an exposed key.
  Rotate keys in the OpenRouter dashboard if there is any doubt.
- There is no per-profile key storage, no OAuth login for OpenRouter, and no
  automatic update check inside the running app.
- The running app does not contact GitHub; only the installers do.

## Good next steps if asked

- Optional per-profile OpenRouter keys with encrypted-at-rest storage.
- Configurable limits via environment variables.
- Optional OIDC/OpenRouter OAuth for profiles.
- Better use of usage/cost metadata in the UI.
- More tests around import, limits, and uninstall behavior.
