# 📖 tDocs User Guide & How It Works
# Panduan Pengguna & Cara Kerja tDocs

[English](#english) &bull; [Bahasa Indonesia](#bahasa-indonesia)

---

<a name="english"></a>
## 🇬🇧 English: Complete User Guide

### 1. What is tDocs?

**tDocs** is an open-source, zero-cost personal cloud storage system that transforms your personal Telegram account into an unlimited, high-speed virtual drive. It provides a modern, Google Drive-like Web Dashboard and a robust CLI, all packaged into a single lightweight binary.

#### 💡 The Core Philosophy: Zero Disk Spooling
Traditional cloud bridges download entire files to your VPS or server disk before forwarding them. If you upload a 2 GB file, your server requires at least 2 GB of free disk space and subjects your SSD to heavy wear.

**tDocs operates differently:**
- **Zero VPS Disk Wear**: Files streamed through the browser or CLI are sliced into small chunks in memory and forwarded directly to Telegram's distributed data centers via native MTProto.
- **Direct Pipelining**: Your server acts purely as a real-time protocol bridge, keeping RAM usage minimal (~30–50 MB) and disk usage limited solely to a small SQLite metadata database.

---

### 2. Storage Capacities & Telegram Limits

tDocs leverages Telegram's official document storage infrastructure:

| Account Type | Maximum File Size | Total Storage Limit | Monthly Bandwidth |
| :--- | :--- | :--- | :--- |
| **Telegram Free** | **2.0 GB** per file | **Unlimited** ♾️ | Free & Unlimited |
| **Telegram Premium** | **4.0 GB** per file | **Unlimited** ♾️ | Free & Unlimited |

> **Note**: While the total number of files and storage capacity is virtually unlimited, Telegram imposes rate limits on aggressive automated transfers. tDocs includes a built-in **Safe Mode** to ensure complete compliance.

---

### 3. How It Works Under the Hood

```
[Browser / Web UI]
       │
       ▼ (5 MB Chunks via HTTP Multipart)
[tDocs Bridge Server]
       │
       ▼ (512 KB Parts via Native MTProto TCP)
[Telegram Cloud Data Centers] ──► Saved in private "tDocs Vault"
```

1. **Client-Side Slicing**: When you upload a file in the web dashboard, the browser reads the file locally and slices it into 5 MB chunks.
2. **MTProto Part Streaming**: The tDocs server receives each chunk and streams it into Telegram's MTProto protocol in 512 KB parts (`upload.saveBigFilePart`).
3. **Metadata Persistence**: Once all parts are uploaded, Telegram returns a unique document handle (`DocumentID`, `AccessHash`, `FileReference`). tDocs saves this metadata along with folder hierarchy into a local SQLite database (`tdocs.db`) running in Write-Ahead Logging (WAL) mode.
4. **Instant Media Streaming (HTTP 206)**: When you stream a video or audio file in the web player, tDocs requests only the required byte offsets from Telegram via MTProto range requests, allowing you to seek through a 2 GB video instantly without waiting for the full file to download.

---

### 4. Step-by-Step Walkthrough

#### Step 1: Initial Login & Telegram Pairing
Before running the web dashboard, link your Telegram account using the interactive terminal wizard:

```bash
# Via npx (recommended for Node.js / npm users):
npx tdocs login

# Or via pre-compiled binary:
./tdocs login
```

<div align="center">
  <img src="assets/teledrive-login.png" alt="tDocs Login Terminal Wizard" width="600" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
  <p><em>Figure 1: tDocs interactive terminal authentication wizard.</em></p>
</div>

1. Enter your `API_ID` and `API_HASH` (obtained for free from [my.telegram.org](https://my.telegram.org)).
2. Enter your phone number in international format (e.g., `+6281234567890`).
3. Enter the 5-digit verification code sent directly to your Telegram app.
4. If Two-Factor Authentication (2FA) is active, enter your Cloud Password.
5. **Storage Channel Discovery & Onboarding**: tDocs automatically scans your Telegram account for an existing `tDocs Vault` channel. If an existing channel is detected, it prompts you to link it and optionally restore your latest database snapshot right away. If no channel exists yet, it creates a new private `tDocs Vault` channel automatically.

#### Step 2: Launching the Web Dashboard

```bash
# Via npx:
npx tdocs server

# Or via binary:
./tdocs server
```

By default, tDocs scans for an available port starting at `8080` and displays both local and LAN IP addresses. Open your web browser at:
👉 **`http://localhost:8080`**

- **Default Admin Password**: `admin123` (Configure via `TDOCS_ADMIN_PASSWORD`).

#### Step 3: Navigating the Dashboard
- **Overview tab** (default): storage analytics (totals, per-type gauge + breakdown), recent files, duplicate-content warnings, and quick actions.
- **Dual View Modes**: Toggle between **Grid View** (visual file cards) and **Table/List View** (sorting by Name, Size, Date, with multi-select checkboxes for bulk actions).
- **Search + filters**: Real-time search with type/size filters across your virtual drive.
- **Upload Manager**: Floating drawer with per-file progress, **pause/resume/retry**, and duplicate-content warnings.
- **Media Preview**: Click any file to preview pictures, stream seekable videos, play audio, view PDFs, or inspect code/text. Right-click any item for the context menu (preview, download, details, share, star, rename, trash).

#### Step 4: Virtual Folder & File Management
- Click **New Folder** to create hierarchical directories.
- **Star** files to pin them under **Favorites**. Open **Details** (eye icon) for metadata, version history, shares and duplicates.
- Deleting moves items to **Trash** (restorable). **Purge** inside Trash deletes forever, including the Telegram copy. Re-uploading an existing name archives the previous copy under **Versions** automatically.
- Bulk-select rows/cards to trash, restore, move, favorite or purge many files at once.

#### Step 5: Public Share Links & QR Code Generator
Need to share a file with someone who doesn't have an account on your server?
1. Open the file context menu (right-click) and select **Share Link**.
2. Optionally set a **password** (Argon2id-hashed), an **expiration timer**, and a **download limit**.
3. Copy the public link (`/s/:token`) or click the **QR Code** button to allow mobile users to scan and download immediately.
4. Audit or revoke active share links anytime under the **Shared Links** tab in the sidebar.

#### Step 5b: Sync Center, Settings & API
- **Sync Center** shows Telegram backend health, every catalog sync run, and one-click recovery.
- **Settings** lets you change the admin password (Argon2id), manage **API tokens** (for scripts/film sites), inspect **active sessions**, set your **public domain with automatic HTTPS**, read the **audit log**, and check **system health**.
- Developers: `GET /api/openapi.json` + Swagger at `/docs`. Scripts should use API tokens (`Authorization: Bearer …`); browser mutations need the `X-CSRF-Token` header.

#### Step 6: Database Snapshots & Point-in-Time Restore
All your folder structures and file references are stored in SQLite. tDocs provides built-in online disaster recovery:
1. Navigate to the **Snapshots** tab in the sidebar.
2. Click **Create Snapshot Now**: tDocs checkpoints SQLite WAL, creates a consistent compressed `.db.gz` snapshot, and uploads it to your Telegram Storage Channel.
3. **Rolling Retention**: tDocs automatically keeps the newest 5 snapshots and deletes older ones to keep your Telegram channel clean.
4. **Point-in-Time Restore**: Click **Restore** on any previous snapshot to safely hot-swap your database without restarting the server.
5. **Offline Backups**: Download `.db.gz` directly to your local computer, or use **Upload & Restore** to recover from an external backup file.

---

### 5. Security & Telegram Safe Mode

tDocs is engineered with strict safeguards to protect your primary Telegram account from bans or restrictions:

1. **Official Telemetry Emulation**: tDocs identifies itself using standard Telegram Desktop client parameters (`PC 64bit`, `Linux/x86_64`, `AppVersion 5.0.0`).
2. **Sequential Safe Queue**: Uploads and downloads are processed sequentially (1 transfer at a time) mimicking natural desktop user behavior.
3. **Pacing Delay**: An adaptive 30ms sleep is enforced between 512 KB chunks to keep connection temperatures low.
4. **Automated Flood Control**: If Telegram issues a `FLOOD_WAIT_X` response, tDocs gracefully pauses until the cooldown elapses without crashing or hammering the API.
5. **Encrypted Session at Rest**: Your MTProto session authentication keys are encrypted in SQLite using **AES-256-GCM** derived from your secret key.
6. **Isolated Private Vault**: All file transfers go into a private storage channel with 0 external members.

---

### 6. Troubleshooting & FAQ

#### Q: Is my data private? Can other people see my files?
No. All files are uploaded into your own private Telegram channel (`tDocs Vault`). Only the authenticated Telegram account has access to this channel. Share links are only accessible if you explicitly generate them.

#### Q: What happens if my server crashes or I move to another PC/VPS?
Because your SQLite metadata database is automatically snapshotted to your Telegram channel (`tdocs backup` or automated snapshots), moving to a new computer is seamless:
1. Run `./tdocs login` on the new machine.
2. tDocs automatically discovers your existing `tDocs Vault` channel and prompts to restore your latest database snapshot.
3. Confirm `[Y]`, and your files, virtual folders, and configuration are restored instantly without extra steps!

#### Q: How do I change the admin dashboard password?
Set the `TDOCS_ADMIN_PASSWORD` environment variable before launching the server:
```bash
export TDOCS_ADMIN_PASSWORD="my_strong_password"
./tdocs server
```

---

### 7. Telegram Connection States & The Setup Wizard

A configured `APP_ID`/`APP_HASH` is not the same as a working connection, so tDocs never guesses. The dashboard badge shows exactly one of:

| State | What it means | What to do |
| :--- | :--- | :--- |
| **NOT CONFIGURED** | No Telegram API credentials on this host | Superuser → Settings → Telegram Storage → enter credentials |
| **CONNECTING** | A live probe is running | Wait a few seconds |
| **AUTHENTICATION REQUIRED** | Credentials exist, session missing or expired | Run the wizard's authentication step (OTP / 2FA) |
| **CONNECTED** | A real network probe succeeded | Nothing — the badge is re-verified at most every 30 s |
| **DISCONNECTED** | Transport or Storage Channel unreachable / timed out | Check network, then **Test Connection** |
| **ERROR** | Authenticated, but Telegram returned an error | Read the message, then retry |

**The wizard (superusers only).** Settings → Telegram Storage walks through 7 steps: what Telegram storage means → credentials (secrets are never sent back to the browser after saving) → authentication → Storage Channel selection with permission verification → **Test Connection** (a real backend probe: reachable, authorised, channel accessible, read permission) → Initial Sync with progress and per-item retry → a verified summary. Both the UI *and* the server enforce the superuser rule: the wizard routes return `403 superuser_required` for anyone else, including API tokens and share guests.

Without Telegram, tDocs runs in **"Telegram Storage — Not Configured"** mode: local folders, favourites, trash, shares, previews and the media player all keep working, while storage-backed screens say so honestly instead of showing an empty list.

### 8. Media Center & Playback Speed

The Media section groups **Music / Videos / Images**, with a persistent mini-player that survives navigation:

- **Audio** — queue/playlist, play/pause, seek, volume, playback speed, previous/next, shuffle, repeat, metadata, and a mini-player that keeps playing while you browse.
- **Video** — play/pause, seek, volume, speed, fullscreen, Picture-in-Picture, previous/next, subtitle tracks when present, and playback position memory.
- **Images** — gallery with fullscreen, previous/next and zoom.

Playback starts fast because two things happen ahead of time: the stream URL is resolved and warmed up *before* you press play (the video element only preloads metadata), and the `file → channel message` lookup is cached for a short TTL, so seeking and resuming do not trigger a fresh Telegram API round-trip on every byte range.

---

<a name="bahasa-indonesia"></a>
## 🇮🇩 Bahasa Indonesia: Panduan Lengkap Pengguna

### 1. Apa Itu tDocs?

**tDocs** adalah sistem penyimpanan awan pribadi (*personal cloud storage*) bersumber terbuka (open-source) yang menyulap akun Telegram Anda menjadi media penyimpanan tanpa batas berkecepatan tinggi. tDocs dilengkapi antarmuka web modern mirip Google Drive serta perintah baris (CLI), yang semuanya dikompilasi ke dalam satu file binary portabel tanpa ketergantungan runtime tambahan.

#### 💡 Filosofi Utama: Zero Disk Spooling (Hemat Hard Disk)
Jembatan cloud konvensional biasanya mengunduh seluruh file ke hard disk VPS/komputer server sebelum dikirimkan ke tujuan. Jika Anda mengunggah file 2 GB, server Anda membutuhkan ruang kosong minimal 2 GB dan menyebabkan keausan fisik (*wear-out*) pada SSD server.

**tDocs bekerja dengan prinsip yang berbeda:**
- **Bebas Beban Disk VPS**: File yang diunggah melalui browser atau CLI dipotong langsung di memori menjadi bagian-bagian kecil dan dialirkan langsung (*pipelined*) ke data center Telegram melalui protokol resmi MTProto.
- **Efisiensi Tinggi**: Server Anda murni bertindak sebagai jembatan protokol real-time. Konsumsi RAM sangat rendah (~30–50 MB) dan penggunaan disk lokal hanya digunakan untuk file database metadata SQLite berukuran beberapa megabyte.

---

### 2. Kapasitas Penyimpanan & Batasan Telegram

tDocs memanfaatkan infrastruktur penyimpanan dokumen resmi Telegram:

| Jenis Akun Telegram | Ukuran Maksimal per File | Batas Total Kapasitas | Bandwidth Bulanan |
| :--- | :--- | :--- | :--- |
| **Akun Reguler (Gratis)** | **2.0 GB** per file | **Tanpa Batas** ♾️ | Gratis & Tanpa Kuota |
| **Akun Telegram Premium** | **4.0 GB** per file | **Tanpa Batas** ♾️ | Gratis & Tanpa Kuota |

> **Catatan**: Meskipun kapasitas total dan jumlah file tidak terbatas, Telegram menerapkan aturan batasan frekuensi transfer data. tDocs dilengkapi fitur bawaan **Safe Mode** untuk menjaga akun Anda tetap aman 100% sesuai aturan resmi.

---

### 3. Cara Kerja Teknis di Balik Layar

```
[Browser Pengguna]
       │
       ▼ (Chunk 5 MB via HTTP Multipart)
[Server tDocs]
       │
       ▼ (Part 512 KB via Protokol Resmi MTProto)
[Pusat Data Telegram] ──► Tersimpan aman di channel privat "tDocs Vault"
```

1. **Pemotongan di Sisi Klien (Client Slicing)**: Saat Anda mengunggah file di web, browser membaca file secara lokal dan memotongnya menjadi chunk 5 MB.
2. **Aliran Data MTProto**: Server tDocs menerima tiap chunk dan mengalirkannya langsung ke server Telegram dalam bagian 512 KB (`upload.saveBigFilePart`).
3. **Penyimpanan Metadata**: Setelah seluruh bagian selesai terunggah, Telegram mengembalikan pointer dokumen (`DocumentID`, `AccessHash`, `FileReference`). tDocs mencatat metadata ini beserta struktur folder ke dalam database SQLite lokal berkecepatan tinggi dengan mode Write-Ahead Logging (WAL).
4. **Streaming Media Seketika (HTTP 206)**: Saat Anda menonton video atau memutar lagu di browser, tDocs hanya meminta rentang byte yang sedang diputar dari Telegram. Anda dapat melompati durasi (*seeking*) video 2 GB dalam hitungan detik tanpa perlu mengunduh seluruh file terlebih dahulu.

---

### 4. Panduan Penggunaan Langkah Demi Langkah

#### Langkah 1: Pasangkan Akun Telegram (Login Pertama Kali)
Sebelum menyalakan server web, hubungkan akun Telegram Anda melalui wizard terminal:

```bash
# Jalankan via npx (direkomendasikan untuk pengguna Node.js / npm):
npx tdocs login

# Atau jalankan binary kompilasi langsung:
./tdocs login
```

<div align="center">
  <img src="assets/teledrive-login.png" alt="Tampilan Terminal Wizard Login tDocs" width="600" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
  <p><em>Gambar 1: Wizard otentikasi interaktif tDocs di terminal.</em></p>
</div>

1. Masukkan `API_ID` dan `API_HASH` Anda (dapat diperoleh gratis dari [my.telegram.org](https://my.telegram.org)).
2. Masukkan nomor telepon Telegram dalam format internasional (contoh: `+6281234567890`).
3. Masukkan kode login 5 digit yang dikirimkan ke aplikasi Telegram Anda.
4. Jika akun Anda menggunakan Two-Factor Authentication (2FA), masukkan Cloud Password Anda.
5. **Deteksi Otomatis & Onboarding Storage Channel**: tDocs secara cerdas memindai dialog akun Telegram Anda mencari channel `tDocs Vault` yang sudah ada. Jika channel lama ditemukan, tDocs menawarkan untuk langsung menyambungkannya dan memulihkan snapshot database terbaru. Jika belum pernah dibuat, tDocs otomatis membuat channel privat baru `tDocs Vault`.

#### Langkah 2: Menjalankan Server Web Dashboard

```bash
# Jalankan via npx:
npx tdocs server

# Atau jalankan via binary:
./tdocs server
```

Secara otomatis, tDocs akan mencari port yang tersedia mulai dari `8080` dan menampilkan alamat akses lokal maupun jaringan Wi-Fi/LAN. Buka browser Anda di:
👉 **`http://localhost:8080`**

- **Password Admin Bawaan**: `admin123` (Dapat diubah melalui variabel `TDOCS_ADMIN_PASSWORD`).

#### Langkah 3: Navigasi Antarmuka Web
- **Pilihan Tampilan (Dual View)**: Pilih antara **Grid View** (kartu file interaktif dengan ikon format) atau **Table/List View** (tabel detail dengan sorting instan Nama, Ukuran, dan Tanggal).
- **Pencarian Cepat**: Cari file secara instan melalui kolom pencarian di bagian atas.
- **Upload Manager**: Drawer melayang di pojok kanan bawah yang menampilkan progres unggahan per-chunk secara transparan (`Chunk 3/8`).
- **Pratinjau Media & Kode**: Klik file apa saja untuk melihat gambar, memutar video MP4/WebM, mendengarkan lagu, membaca dokumen PDF, atau melihat file kode pemrograman (`.go`, `.py`, `.json`, `.txt`).

#### Langkah 4: Manajemen Folder & Berkas Virtual
- Klik tombol **New Folder** untuk membuat subfolder baru.
- Pindahkan file antar folder dengan mudah (dilengkapi proteksi anti siklus agar folder tidak dapat dipindahkan ke dalam dirinya sendiri).
- Ganti nama (*rename*) atau hapus file yang sudah tidak diperlukan.

#### Langkah 5: Berbagi Link Publik & Fitur QR Code
Ingin membagikan file kepada teman tanpa memberi akses akun admin?
1. Buka menu aksi berkas (`⋮`) dan pilih **Create Public Share Link**.
2. Anda dapat menambahkan **password** (diamankan dengan hashing bcrypt) atau mengatur **batas waktu kedaluwarsa** (1 jam, 1 hari, 7 hari, atau selamanya).
3. Salin URL publik (`/s/:token`) atau klik tombol **QR Code** agar teman Anda bisa langsung memindai tautan melalui kamera ponsel.
4. Anda dapat memantau jumlah unduhan atau mencabut (*revoke*) link berbagi kapan saja melalui tab **Shared Links**.

#### Langkah 6: Snapshot Database & Pemulihan Point-in-Time
Seluruh hierarki folder dan penunjuk file tersimpan di database SQLite. tDocs menyediakan sistem pencadangan terintegrasi:
1. Buka menu **Snapshots** di sidebar.
2. Klik **Create Snapshot Now**: tDocs mengunci WAL SQLite, membuat snapshot `.db.gz` yang konsisten, dan mengunggahnya ke channel Telegram Anda.
3. **Retensi Bergulir (Rolling Retention)**: tDocs otomatis mempertahankan **5 snapshot terbaru** dan menghapus cadangan yang lebih lama agar channel tetap rapi dan tidak boros ruang.
4. **Point-in-Time Restore**: Klik tombol **Restore** pada snapshot tanggal tertentu untuk memulihkan seluruh struktur data secara instan tanpa perlu mematikan aplikasi.
5. **Cadangan Offline**: Unduh langsung file `.db.gz` ke laptop/PC Anda, atau gunakan fitur **Upload & Restore** untuk memulihkan database dari file cadangan lokal saat berpindah komputer.

---

### 5. Keamanan & Kepatuhan Safe Mode

tDocs dirancang khusus dengan sistem pertahanan berlapis agar akun utama Telegram Anda tetap aman dan terbebas dari sanksi/banned:

1. **Emulasi Telemetri Resmi**: tDocs menggunakan identitas klien resmi Telegram Desktop (`PC 64bit`, `Linux/x86_64`, `AppVersion 5.0.0`).
2. **Antrean Sekuensial**: Proses upload dan download berjalan strictly 1 antrean dalam satu waktu, persis seperti kebiasaan manusia saat menggunakan aplikasi desktop resmi.
3. **Pacing Delay**: Jeda adaptif sebesar 30ms diterapkan di antara bagian 512 KB untuk menjaga koneksi tetap stabil dan tidak dianggap aktivitas spamming.
4. **Penanganan Otomatis Flood Wait**: Jika Telegram mengirim sinyal `FLOOD_WAIT_X`, tDocs akan otomatis menunggu durasi jeda yang diminta tanpa melakukan serangan permintaan ulang (*hammering*).
5. **Enkripsi Kunci Sesi (AES-256-GCM)**: Kunci otentikasi sesi Telegram MTProto dienkripsi menggunakan AES-256-GCM sebelum disimpan di database lokal.
6. **Channel Pribadi Terisolasi**: File tersimpan di channel private dengan 0 anggota luar, sehingga file Anda tidak dapat diakses atau dicari oleh pengguna Telegram lain.

---

### 6. Tanya Jawab Umum (FAQ)

#### T: Apakah file saya bisa dilihat orang lain di Telegram?
Tidak. Semua file disimpan di channel pribadi milik Anda sendiri (`tDocs Vault`). Tidak ada orang lain yang memiliki akses ke channel tersebut kecuali Anda sendiri atau melalui tautan publik yang sengaja Anda buat.

#### T: Bagaimana jika komputer/VPS saya rusak atau saya ingin pindah ke PC baru?
Sangat mudah dan otomatis! Karena database metadata SQLite Anda dicadangkan ke channel Telegram:
1. Jalankan `./tdocs login` di PC baru.
2. tDocs secara otomatis mendeteksi channel `tDocs Vault` lama Anda dan menawarkan opsi untuk langsung memulihkan snapshot database terbaru.
3. Tekan `[Y]`, seluruh struktur folder dan file Anda akan kembali seperti semula seketika!

#### T: Bagaimana cara mengganti password admin web?
Cukup atur variabel lingkungan `TDOCS_ADMIN_PASSWORD` sebelum menjalankan server:
```bash
export TDOCS_ADMIN_PASSWORD="password_baru_anda"
./tdocs server
```

---

### 7. Status Koneksi Telegram & Setup Wizard

`APP_ID`/`APP_HASH` yang tersimpan hanyalah konfigurasi, bukan bukti koneksi. Karena itu badge dashboard menampilkan salah satu dari status berikut, dan hanya menulis **CONNECTED** setelah probe jaringan nyata berhasil (diverifikasi ulang maksimal tiap 30 detik): `NOT CONFIGURED`, `CONNECTING`, `AUTHENTICATION REQUIRED`, `CONNECTED`, `DISCONNECTED`, `ERROR`.

**Wizard hanya untuk superuser.** Settings → Telegram Storage memandu 7 langkah: penjelasan storage backend → kredensial (secret tidak pernah dikirim kembali ke browser setelah disimpan) → autentikasi (OTP/2FA) → pemilihan Storage Channel + verifikasi izin → **Test Connection** (probe backend sungguhan: API reachable, sesi valid, channel bisa diakses, izin baca tersedia) → Initial Sync dengan progres dan *retry* per item → ringkasan terverifikasi. Aturan superuser ditegakkan di **server**, bukan hanya disembunyikan di UI: rute wizard mengembalikan `403 superuser_required` untuk pengguna biasa, API token, maupun tamu share link.

Tanpa Telegram, tDocs masuk mode **"Telegram Storage — Not Configured"**: folder lokal, favorit, trash, share, preview, dan media player tetap jalan; layar yang bergantung storage mengaku belum siap dengan jujur (bukan daftar kosong palsu).

### 8. Media Center & Kecepatan Putar

Bagian **Media** mengelompokkan **Music / Videos / Images** dengan mini-player yang tetap hidup saat Anda berpindah halaman:

- **Audio** — antrean/playlist, play/pause, seek, volume, kecepatan putar, prev/next, shuffle, repeat, metadata.
- **Video** — play/pause, seek, volume, kecepatan, fullscreen, Picture-in-Picture, prev/next, subtitle bila tersedia, serta ingatan posisi terakhir.
- **Gambar** — galeri fullscreen dengan prev/next dan zoom.

Pemutaran terasa cepat karena dua hal dikerjakan lebih dulu: URL stream di-resolve dan dipanaskan **sebelum** tombol play ditekan (video hanya memuat metadata), dan pemetaan `file → pesan channel` di-cache singkat sehingga seek/resume tidak memicu panggilan Telegram API baru di setiap rentang byte.
