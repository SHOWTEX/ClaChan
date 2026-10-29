# Contributing to Clachan

Thanks for taking the time to improve Clachan. This project is deliberately
small: a single Go binary, SQLite, and a vanilla JavaScript frontend. Please keep
changes in that spirit.

## Ground rules

- **Go 1.27+**, standard-library `net/http`, and `CGO_ENABLED=0`.
- Exactly two direct third-party dependencies:
  `modernc.org/sqlite` and `golang.org/x/time/rate`.
- No frontend build step, framework, or npm dependency. `static/` is embedded
  and shipped as-is.
- OpenRouter is the only upstream provider.
- Data is scoped per profile. Any new query that touches conversations,
  messages, prompts, or settings must include the signed-in profile.
- Never commit real API keys, `.env`, the SQLite database, or screenshots that
  show personal chats.

## Development setup

```powershell
Copy-Item .env.example .env
# edit .env and set OPENROUTER_API_KEY
go run .
```

Open <http://localhost:8080>. The first visit creates the owner profile. If you
want to keep your normal database untouched, point `CLACHAN_DB` at a temporary
file:

```powershell
$env:CLACHAN_DB="$env:TEMP\clachan-dev.db"
go run .
```

The app starts without a key, but chat requests are rejected until one is set.
Model metadata is fetched from OpenRouter at startup and cached in memory.

## Checks

Run these before opening a pull request:

```powershell
gofmt -l .
go vet ./...
go test ./...
node --check static/app.js
```

`node --check` is optional when Node is not installed; the frontend has no build
step. Installer changes should also be checked:

```bash
bash -n install.sh
bash install.sh --help
```

On Windows PowerShell:

```powershell
$errors = $null
[System.Management.Automation.Language.Parser]::ParseFile(
  (Resolve-Path .\install.ps1), [ref]$null, [ref]$errors) | Out-Null
if ($errors.Count) { $errors; exit 1 }
.\install.ps1 -Help
```

For the deployment binary:

```powershell
.\build.ps1
```

On Linux or macOS:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -trimpath -ldflags "-s -w -X main.version=1.0.1" -o clachan .
```

CI runs the same formatting, vet, test, installer, and cross-compile checks on
every push to `main` and every pull request.

## Releases

Tags matching `v*` trigger `.github/workflows/release.yml`. The workflow tests,
builds all four release targets, injects the repository path into the installers,
writes `checksums.txt`, and publishes the GitHub release. Do not commit build
output. To cut a release:

```bash
git tag -a v1.0.1 -m "v1.0.1"
git push origin v1.0.1
```

## Tests

- Store and handler tests use `httptest` and a temporary SQLite database.
- No test may call OpenRouter or another network service.
- Add coverage for new schema migrations, profile scoping, validation, and
  streaming events where applicable.
- Frontend changes should be checked in a browser at desktop and phone widths,
  including keyboard navigation and the existing CSP (no inline script or style).

## UI conventions

- Keep the interface in `static/app.js`, `static/style.css`, and
  `static/index.html`; there is no bundler.
- Escape model output with `textContent`/DOM APIs. Never inject raw HTML.
- Preserve the existing themes, accessibility labels, and mobile safe areas.
- Update `README.md` when a user-visible feature, environment variable, API
  route, or deployment step changes.

## Pull requests

- Keep each pull request focused on one change.
- Describe the user-visible effect and how you verified it.
- Include tests for behaviour changes.
- Do not reformat unrelated files.

By contributing, you agree that your work is licensed under the MIT License in
[LICENSE](LICENSE).
