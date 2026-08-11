# Sanmon — panduan repo

Gateway LLM self-hosted. Baca `.docs/PRD.md` sebelum ngerjain apa pun —
di situ ada tujuan, bukan-tujuan, milestone, dan aturan keras.
Untuk apa pun yang menyentuh UI, baca `.docs/DESIGN.md` dulu.

## Status

Belum ada kode. Baru dokumen.

## Tujuan utama

**Mendalami Go**, bukan sekadar bikin fitur jalan. Kalau ada dua cara dan yang satu
lebih ngajarin Go (goroutine, channel, context, io) — pilih itu, meski lebih panjang.
Ini satu-satunya tempat aturan "kode paling sedikit menang" boleh kalah.

## Aturan keras

Delapan hal ini kalau dilanggar bikin fitur bohong diam-diam atau layanan mati.
Detailnya di PRD §10.

1. **Jalur proxy tidak pernah nunggu DB.** Kirim ke channel log pakai `select` +
   `default`. Antrian penuh atau Postgres mati → log dibuang, request tetap jalan
2. **Uang integer micro-USD**, bukan float
3. Model tanpa harga di config → `cost_unknown` + peringatan. Nol yang jujur,
   bukan nol yang bohong
4. Gateway nyisipin `stream_options.include_usage` kalau klien nggak minta
5. **Maksimal 1 retry.** Request gagal tetap makan kuota OpenRouter
6. API key provider **tidak pernah** masuk tabel log
7. **Auth ada sejak M1**, bukan M4
8. Rute SPA punya catch-all ke `index.html`

## Batasan teknis

- **stdlib `net/http`, bukan Gin.** Alasannya di PRD §4 — jangan diubah tanpa
  baca itu dulu
- **Tidak ada abstraksi buat satu implementasi.** Nggak ada interface repository,
  nggak ada factory, nggak ada config buat nilai yang nggak pernah berubah
- Tidak ada timeout global di jalur proxy (generasi LLM bisa menit-menitan);
  `ReadHeaderTimeout` tetap dipasang
- Harga model di config YAML, bukan di kode

## Rute

- `/v1/*` — proxy, auth pakai virtual key
- `/admin/*` — dashboard, auth pakai admin token
- `/healthz` — tanpa auth

## Port

Gateway **8777**, dashboard dev **8778**.
Jangan pernah ganti diam-diam atau fallback ke port lain — tanya dulu.

## Git

- Satu commit = satu perubahan logis. Jangan bundle banyak file
- Bikin branch baru, jangan push ke `main`
- **Jangan pernah bikin Pull Request** — Rakha yang bikin sendiri

## Log sesi

`~/Documents/Nexus/02-Areas/Personal/sanmon/log.md` — entri terbaru di atas.
