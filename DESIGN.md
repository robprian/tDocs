# DESIGN.md

## Produk
Arsip pribadi Telegram. Tenang dan cepat. Dashboard profesional.
Untuk pemilik tunggal, bukan tim. Pekerjaan utama: cek status koneksi Telegram, jelajah arsip, pantau penyimpanan.

## Gaya: Neumorphism tenang
- Permukaan lembut timbul/tenggelam (soft extrude), bukan flat steril, bukan glow AI.
- Light-first sebagai default. Dark mode ikut, kedua mode wajib lolos kontras.
- Font sistem (system-ui stack). Alasan: cepat, tanpa unduh, cocok arsip pribadi.
- Radius konsisten: kartu 16px, tombol/input 12px, pil hanya untuk status kecil.
- Satu aksen: biru `#2f6bff` untuk aksi primer dan status terhubung. Sisanya netral.
- Bayangan neumorphism hanya pada kartu dan tombol primer. Bukan di semua elemen (dose cap antislop R-10/R-12).

## Batasan neumorphism vs aksesibilitas
- Teks timbul (emboss) dilarang untuk teks isi. Semua teks wajib lolos WCAG AA 4.5:1 (R-25).
- Efek lembut hanya dekorasi permukaan. Informasi status selalu ada teks + ikon, bukan warna saja.

## Dial
`ENERGY 1 / RHYTHM 2 / MOTION 1`
- ENERGY 1: tenang, linear, seperti arsip. Satu focal point per layar (status Telegram di overview).
- RHYTHM 2: konsisten dengan sedikit variasi. Overview (kartu metrik + gauge) beda ritme dari tabel Drive.
- MOTION 1: hover saja. Tanpa loop, tanpa serial animasi template.

## Motif identitas
Kartu lembut timbul dengan lekuk dalam (inset) untuk area angka/metrik. Diulang di stat cards, gauge, wizard step.

## Prioritas
Semua paralel: wizard Telegram, metrik, responsif. Tidak ada yang dikorbankan.
- Wizard: selesai 3 langkah tanpa bingung (telepon, kode, verifikasi). Resend + error jelas.
- Metrik: angka real dari `/api/stats` saja. Tanpa delta palsu, tanpa feed palsu.
- Responsif: 360px sampai 1440px. Sidebar drawer, tabel scroll horizontal, wizard fullscreen di HP.
