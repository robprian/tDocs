# tDocs Security & Abuse Mitigation Guide

This document defines the security architecture, threat model, cryptographic practices, and Telegram Terms of Service (ToS) abuse mitigation strategies for tDocs.

---

## 1. Threat Model & Asset Protection

| Asset | Primary Threat | Mitigation |
| :--- | :--- | :--- |
| **MTProto Session (`auth_key`)** | Extraction from database leads to total Telegram account takeover. | Encrypted at rest via **AES-256-GCM**. Master key held in environment variable or restricted keyfile. |
| **Telegram API Credentials** | Public exposure of `API_ID` and `API_HASH`. | Loaded via environment variables (`TDOCS_TG_APP_ID`, `TDOCS_TG_APP_HASH`). Excluded from repository. |
| **Stored User Files** | Telegram inspects media or unauthorized parties view files. | Files stored in **Private Channels**; optional transparent **AES-256-GCM chunk encryption** before upload. |
| **Admin Dashboard** | Unauthorized web access to drive and upload endpoints. | Bcrypt password hashing (`cost=12`), secure HTTP session cookies (`HttpOnly`, `SameSite=Lax`). |
| **Public Share Links** | Brute-force scanning of share URLs or guessable passwords. | 128-bit high-entropy random tokens, bcrypt password hashing, IP-based rate limiting on unlock attempts. |
| **Telegram Account Status** | Account ban or permanent freeze triggered by Telegram SpamBot. | Strict concurrency caps, dedicated secondary phone number, and automated `FLOOD_WAIT` backoff. |

---

## 2. Session Encryption at Rest (AES-256-GCM)

The MTProto session string contains the cryptographic authorization key negotiated with Telegram data centers. tDocs enforces encryption before storing this string in SQLite:

```
[Plaintext MTProto Session]
            │
            ▼
[AES-256-GCM Encryptor] ◄── Key derived from TDOCS_SECRET_KEY (SHA-256)
            │
            ├─► 12-byte random cryptographic nonce
            ├─► Ciphertext
            └─► 16-byte authentication tag
            │
            ▼
[Base64 Combined Payload] ──► Stored in SQLite `settings` table
```

### Key Management Rules:
1. Master secret MUST be provided via `TDOCS_SECRET_KEY` environment variable (`ROBDOCS_SECRET_KEY` / `TELEDRIVE_SECRET_KEY` still accepted as legacy fallbacks).
2. If no secret env is provided, tDocs reuses `.tdocs.key` (then `.robdocs.key`, `.teledrive.key`); otherwise it generates a 32-byte cryptographically secure key, writes it to `.tdocs.key` with Unix file permissions `0600` (read/write only by owner), and logs a security warning.
3. If the database is backed up to Telegram, the `.tdocs.key` is **never included** in the backup snapshot.

---

## 3. Telegram ToS Compliance & Primary Account "Safe Mode"

If using your **primary personal Telegram account**, tDocs MUST operate under **Safe Mode** to ensure 100% compliance with Telegram's API Terms of Service (Section 1.4) and avoid triggering automated anti-abuse or SpamBot penalties.

### 3.1 Five Cardinal Rules for Primary Account Safety

1. **Exclusive Use of Personal API Credentials**:
   - You MUST generate your own `API_ID` and `API_HASH` directly from [my.telegram.org](https://my.telegram.org).
   - **Never** use public or shared API keys found in open-source projects or online tutorials. Telegram periodically mass-bans accounts attached to blacklisted or revoked API application keys.

2. **Strictly Personal Storage (No Public / High-Traffic CDN Proxying)**:
   - Telegram's ToS strictly prohibits using MTProto as an automated public content distribution network (CDN).
   - When using a primary account, **Share Links must remain private** (intended for personal backup access or sharing specific files with family/colleagues). Do not post share links to public forums or use tDocs to host high-traffic downloads for strangers.

3. **Sequential Single-Worker Uploads (Zero Aggressive Multi-Threading)**:
   - tDocs caps upload concurrency to **1 active upload stream** at a time.
   - 512 KB Parts are transferred sequentially, exactly mimicking the network pattern of the official Telegram Desktop client.
   - Do not enable multi-threaded connection pooling when connected to a primary account.

4. **Organic Client Fingerprinting**:
   - The MTProto connection explicitly sets legitimate desktop client identity metadata (`DeviceModel: "PC 64bit"`, `AppVersion: "5.0.0"`, `LangCode: "en"`).
   - tDocs never sends empty or generic library strings that could be flagged by Telegram DC heuristics as an automated scraper.

5. **Channel Isolation & Zero External Exposure**:
   - The Storage Channel must remain **strictly private** with zero external members.
   - Never invite third-party bots or external users into the Storage Channel.

---

### 3.2 Automated `FLOOD_WAIT` Backoff Protocol

Telegram returns `FLOOD_WAIT_X` (where `X` is cooldown in seconds) when request frequency exceeds server-side thresholds:
1. **Immediate Queue Freeze**: The upload/download queue for that account enters `PAUSED` state immediately.
2. **Deterministic Sleep**: The worker pauses execution for `X + 5` seconds (adding a 5-second defensive margin).
3. **Zero Retry Spamming**: tDocs will NEVER make alternative API calls during a cooldown. Violating or ignoring `FLOOD_WAIT` is the #1 cause of escalating penalties to permanent account bans.
4. **UI Notification**: The Web Dashboard displays a clear, calm status indicator: `"Telegram Cooldown Active: Paused for Xs"`.

---

### 3.3 Adaptive Pacing Delay

To ensure network traffic matches normal human desktop usage:
- tDocs introduces an adaptive **20ms–50ms pacing delay** between consecutive 512 KB `upload.saveBigFilePart` requests.
- Batch deletions or metadata updates are paced with a minimum 200ms delay between API calls to avoid spam burst detection.

---

## 4. File Data Privacy Scope (honest boundaries)

tDocs does **not** claim end-to-end encryption of file bytes, and no such mode
exists in this codebase:

* File bytes travel to Telegram over MTProto (transport-encrypted) and rest on
  Telegram's infrastructure under your account's access control. Anyone holding
  your Telegram session — or Telegram itself — can read them.
* What tDocs **does** encrypt at rest with AES-256-GCM: the MTProto session /
  auth key in SQLite. API tokens are stored as SHA-256 hashes, admin and share
  passwords as Argon2id hashes (legacy bcrypt still verifies).
* Do not store material that requires zero-knowledge guarantees (e.g. secrets
  usable without your Telegram account) in tDocs. For such data, encrypt files
  client-side yourself before uploading.

---

## 5. Web Application Security Controls (implemented & tested)

* **Session authentication**: dashboard login mints a random 256-bit token kept
  server-side (30-day sliding expiry, listed/revocable under Settings →
  Sessions). Cookies are `HttpOnly`, `SameSite=Lax` (+`Secure` on TLS). The old
  static `authenticated` cookie value is rejected — stealing the cookie jar
  from an old install grants nothing.
* **CSRF Protection**: cookie-authenticated `POST`/`PUT`/`DELETE`/`PATCH`
  require the `X-CSRF-Token` header matching the session (token exposed via the
  readable `tdocs_csrf` cookie). Bearer/API-token callers are exempt — they
  carry no ambient authority. The public share-unlock form is exempt by design.
* **Rate limiting** (per IP, in-memory): dashboard login 15/min, general API
  600/min, share-unlock failures 5/min per token.
* **Secure headers** on every response: strict CSP (no third-party scripts;
  inline handlers allowed), `frame-ancestors 'self'` + `X-Frame-Options:
  SAMEORIGIN`, `nosniff`, strict referrer policy, restrictive
  permissions policy, HSTS whenever TLS is active.
* **Password hashing**: Argon2id for admin and new share passwords; legacy
  bcrypt share hashes still verify. API tokens are 256-bit random secrets
  stored as SHA-256, shown once at creation, revocable.
* **Signed expiring downloads**: `POST /api/files/{id}/ticket` mints
  HMAC-SHA256 URLs (default 1h, max 24h) usable without a login session.
* **Share enforcement**: expiry timestamps and `max_downloads` are checked
  server-side (410 Gone when exhausted), not just displayed.
* **Range Request Boundary Validation**: `Range: bytes=start-end` strictly
  validated against file size (416 on violation).
* **Path Traversal Defense**: folders/files addressed by UUID keys; display
  names are validated (no separators, `..`, or control chars) and HTML-escaped
  in the UI; all SQL is parameterized.
* **Audit logging**: logins, uploads, trash/purge, shares, snapshots, syncs,
  tokens, sessions and password changes land in `audit_logs` with actor + IP,
  viewable under Settings.
* **Secret hygiene**: the admin key is never printed to logs; Telegram
  credentials live in env/`.env` (git-ignored), the session encrypted in SQLite.
* **TLS**: set `TDOCS_TLS_CERT_FILE` + `TDOCS_TLS_KEY_FILE` for native HTTPS;
  behind a reverse proxy, forward `X-Forwarded-Proto` so cookies and HSTS
  behave.
