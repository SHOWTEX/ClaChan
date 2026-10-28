# Security policy

Clachan is designed for a **trusted LAN**. Do not expose it directly to the
public internet or port-forward it to the internet. Put a TLS-terminating proxy
in front of it if you need encrypted transport.

## Supported versions

Security fixes are applied to the latest commit on `main` and the most recent
release. Older builds may not receive fixes.

## Reporting a vulnerability

Please report security issues privately through GitHub's **Security** tab →
**Report a vulnerability**. Do not open a public issue for a suspected
vulnerability.

Include:

- The affected version or commit.
- A description of the impact.
- Steps to reproduce, ideally against a disposable database.
- Any relevant configuration, with secrets removed.

Never include OpenRouter API keys, profile passwords, session cookies, or a copy
of a real database in a report. If a key may have leaked, revoke it in the
OpenRouter dashboard immediately.

Reports are handled on a best-effort basis. Please allow time for a fix before
publishing details.

## Security model

- Profiles are real logins, but they separate people who share the same server;
  they do not protect against someone who can sniff plain HTTP traffic.
- All profiles share the server's OpenRouter key and credits.
- The OpenRouter key is server-side only and is never sent to the browser.
- Passwords are hashed with PBKDF2-HMAC-SHA256. Session cookies are `HttpOnly`,
  `SameSite=Lax`, and store only a hash of the session token.
- Every conversation, message, prompt, and setting query is scoped to the
  signed-in profile.
- Model output is rendered through DOM APIs, not injected as raw HTML.
- Release installers download over HTTPS and verify the binary against the
  release's SHA-256 checksum. The Linux installer runs as root because it creates
  the service user and systemd unit; review it before piping it to `sudo bash`.

## Hardening checklist

- Run the server as an unprivileged system user.
- Keep `.env` readable only by that user (or root plus the service group).
- Restrict the listen address or firewall port 8080 to your LAN.
- Back up the SQLite database and keep backups private.
- Rotate `OPENROUTER_API_KEY` if it is ever exposed.
