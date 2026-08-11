# PRD — Sanmon

> Gateway self-hosted buat trafik LLM. Satu pintu masuk, semua kecatat.

- **Dibuat:** 2026-08-11
- **Status:** disetujui, belum mulai
- **Pemilik:** Rakha (solo, side project)
- **Target MVP selesai:** 2 November 2026

---

## 1. Ringkasan

Sanmon (山門 — gerbang utama kuil) adalah HTTP gateway yang duduk di antara aplikasi
kamu dan provider LLM. Aplikasi nembak ke Sanmon, Sanmon nerusin ke provider, dan
setiap request kecatat: model apa, berapa token, berapa biaya, berapa lama, sukses
atau gagal.

Perubahan di sisi aplikasi idealnya cuma satu: `base_url`.

**Kenapa dibangun:** bukan karena belum ada (LiteLLM, Bifrost, Portkey, Helicone udah
ada). Tapi karena membangun ulang barang yang udah terbukti adalah cara paling aman
buat mendalami Go — dan karena masalahnya nyata: Lumen pernah kepentok biaya OpenAI,
pindah ke Gemini, dan itu butuh ngoprek kode.

---

## 2. Tujuan

**G1 — Mendalami Go serius.** Ini tujuan utama, bukan sampingan. Tiap milestone
dipetakan ke satu konsep Go konkret. Fitur adalah alasan supaya konsepnya kepakai
beneran, bukan latihan kosong.

**G2 — Ganti provider tanpa ngoprek aplikasi.** Peta alias model di config; ganti
tujuan = edit satu baris + restart.

**G3 — Semua trafik LLM kelihatan.** Satu dashboard: request apa, token berapa, biaya
berapa, latency berapa, gagal atau nggak.

**G4 — Punya garis finish yang bisa dibuktikan.** Selesai artinya selesai, bukan
"lanjut kalau sempat".

## 3. Bukan Tujuan

Daftar ini bukan "fitur jelek" — semuanya fitur bagus. Justru **karena** menarik makanya
ditulis di depan. Semua di bawah ini masuk project lain, atau project ini di masa depan.

| Yang dikeluarkan | Alasan |
|---|---|
| Multi-tenant, akun user, billing | Masalah bisnis SaaS, bukan masalah Go |
| Tracing bersarang (span tree), prompt playground, labeling dataset | Butuh SDK terpasang di tiap aplikasi → jadi maintain library PHP/JS/Python |
| Dialek API ketiga | Satu dialek (format OpenAI) sudah nyampe ke semua provider target |
| Semantic cache (kemiripan embedding) | Ambang kemiripan meleset = user dikasih jawaban orang lain, rusaknya diam-diam |
| Kubernetes, HA, clustering | Satu user, satu VPS. Satu binary Go sanggup ribuan koneksi |
| Guardrails / PII redaction / content filter | Masalah kualitas deteksi, bukan masalah gateway. **Parkir**, bisa jadi Fase 4 |
| SDK klien buat bahasa lain | Merusak nilai jual sendiri — intinya justru nggak install apa-apa |
| **MCP support & agent orchestration** | Ini *runtime*, bukan *proxy*. Arsitekturnya beda total. **Kalau mau, mulai repo baru** |
| SSO / RBAC di dashboard | Satu user. Satu admin token cukup |
| Model image / video / audio | Teks + embedding saja |

**Aturan pengaman:** kalau sebuah ide butuh tabel database baru yang nggak ada di §6,
itu tanda dia masuk daftar ini.

---

## 4. Stack & alasannya

| Lapisan | Pilihan | Kenapa, dan apa yang ditolak |
|---|---|---|
| Bahasa | **Go 1.26** | Terverifikasi terpasang (1.26.5) |
| HTTP | **stdlib `net/http` + ServeMux** | **Bukan Gin.** Sanmon itu proxy — bagian tersulitnya `Flusher`, `io.Copy`, context cancellation, dan Gin membungkus `ResponseWriter` justru di situ. Dua jasa utama Gin (routing, binding JSON) nggak relevan: ServeMux 1.22+ udah bisa `POST /path/{id}`, dan proxy sebagian besar nggak mbaca body. Gin sudah dipakai di Klora → nol skill baru. Bukan pintu satu arah: handler stdlib `http.HandlerFunc`, Gin bisa membungkusnya |
| Log | **`log/slog`** | Stdlib, terstruktur, bikin ngedebug streaming waras |
| DB | **PostgreSQL** + `goose` | Sudah dikuasai (Klora, Lumen) → jatah belajar habis di Go, bukan kebagi. Database `sanmon` **terpisah**, jangan numpang DB Lumen |
| Frontend | **Vite + React + HeroUI v3 + Tailwind v4** | **Bukan Next.js** — dashboard cuma SPA yang nembak API Go; Next nambah server Node yang nggak perlu, servernya sudah Go. HeroUI v3 React-only (dibangun di atas React Aria), jadi Vue/Nuxt gugur. Konsekuensi diterima sadar: nol skill frontend baru, tapi waktunya balik ke Go |
| Distribusi | **`embed.FS`** | Hasil build React ditanam ke binary → deploy tetap `scp` satu file |
| Dialek API | **Format OpenAI saja** | Gemini punya endpoint OpenAI-compat (`/v1beta/openai/`, dukung chat + embeddings + `stream_options.include_usage`), OpenRouter native OpenAI. Satu dialek nyampe ke semua target |
| Provider | **Gemini** (utama) + **OpenRouter** (cadangan) | Tidak punya Anthropic API key (langganan Claude Code ≠ API key). Dialek Anthropic-native → Fase 2, hanya kalau Claude Code mau dilewatkan |

---

## 5. Arsitektur

```
CLI lamaran ─┐
             ├──→ [ SANMON :8777 ] ──→ Gemini (OpenAI-compat)
Lumen ───────┘          │          └──→ OpenRouter (cadangan, Fase 2)
                        │
                   channel (non-blocking)
                        ↓
                  worker goroutine
                        ↓
                   PostgreSQL          ←── dashboard React (embedded, :8778 saat dev)
```

**Prinsip nomor satu: jalur proxy tidak boleh nunggu database.**
Kiriman ke channel log pakai `select` + `default`. Antrian penuh atau Postgres ngambek
→ **log dibuang, request tetap jalan.** Log boleh hilang, layanan tidak boleh.
Tanpa aturan ini, Sanmon berubah dari satu titik gagal jadi dua — dan Lumen ikut mati.

---

## 6. Ruang lingkup fungsional

### Endpoint

**Proxy** — auth: virtual key, header `Authorization: Bearer sk-sanmon-...`
- `POST /v1/chat/completions` (streaming & non-streaming)
- `POST /v1/embeddings`
- `GET  /v1/models`

**Admin** — auth: admin token
- `GET /admin/requests` (daftar, filter, pagination)
- `GET /admin/requests/{id}`
- `GET /admin/stream` (SSE live feed)
- `GET /admin/stats` (agregat harian)
- `GET|POST|DELETE /admin/keys` (M4)

**Publik**
- `GET /healthz`

### Tabel

**`requests`** — `id`, `created_at`, `key_id`, `model_requested`, `provider`,
`model_resolved`, `stream`, `status_code`, `error`, `latency_ms`, `ttfb_ms`,
`tokens_in`, `tokens_out`, `cost_micro_usd`, `cost_unknown`, `partial`,
`request_body` (jsonb, nullable), `response_body` (text, nullable)

**`keys`** — `id`, `name`, `token_hash`, `monthly_budget_micro_usd`, `rpm_limit`,
`log_bodies`, `disabled`, `created_at`

### Config (YAML + env untuk rahasia)

Peta alias model → provider + nama model asli + harga per 1 juta token (in/out).
Harga di config, **bukan di kode** — harga provider berubah terus.

---

## 7. Milestone

Estimasi: **5 jam/minggu**. Tiap milestone punya bukti yang bisa dicek sendiri.

### M0 — Fondasi + kulit dashboard · ~7 jam · target **25 Agu 2026**
- `net/http` + ServeMux, config YAML + env, `/healthz`
- Graceful shutdown: `signal.NotifyContext` + `Server.Shutdown`
- `slog` terstruktur
- Migrasi `goose`, Postgres via Docker Compose (lokal)
- **Kulit dashboard statis pakai data palsu — dibatasi keras 3 jam**

> **Pelajaran Go:** stdlib http, signal handling, shutdown rapi
> **Bukti:** `curl /healthz` → 200 · Ctrl-C mati bersih tanpa panic · dashboard kebuka dan identitas visualnya sudah kelihatan
> **Aturan:** dilarang nyentuh animasi sebelum M2

### M1 — Proxy streaming · ~12 jam · target **8 Sep 2026**
- `POST /v1/chat/completions` + `/v1/embeddings`, teruskan ke Gemini
- Streaming SSE tanpa numpuk di memori
- Klien putus → request ke upstream ikut dibatalin
- **Token statis wajib dari sini** (bukan M4) — tanpa ini, begitu naik VPS proxy-nya telanjang
- Nyisipin `stream_options.include_usage` ke request kalau klien nggak minta
- Kebijakan error di tengah stream: header sudah terkirim → kirim event error via SSE + tandai `partial`
- Timeout: **tidak ada** timeout global di jalur proxy; `ReadHeaderTimeout` tetap dipasang

> **Pelajaran Go:** `io.Copy`, `http.Flusher`, context propagation & cancellation, `RoundTripper`
> **Bukti:** `curl -N` keluar token per token · Ctrl-C di klien → log nunjukin upstream ikut batal · request tanpa token ditolak 401

### M2 — Dashboard live feed · ~10 jam · target **22 Sep 2026**
- `GET /admin/stream` — SSE fan-out: satu hub goroutine, banyak channel pelanggan
- Bersih-bersih pas tab ditutup (`select` + `context`)
- Dashboard React nyambung ke SSE, data palsu diganti data asli
- **CLI lamaran magang** — konsumen pertama sekaligus sumber trafik. Batas keras: satu file, satu perintah, tanpa DB, tanpa UI
- Animasi boleh mulai, cuma di 3 tempat (lihat DESIGN.md)

> **Pelajaran Go:** fan-out hub, channel, `select`, cegah goroutine bocor
> **Bukti:** buka 2 tab → dua-duanya update barengan · tutup 1 tab → goroutine-nya bersih (dicek `runtime.NumGoroutine()`)

### M3 — Penyimpanan + biaya · ~8 jam · target **5 Okt 2026**
- Channel ber-buffer + worker goroutine → Postgres, **non-blocking send**
- Drain antrian pas shutdown
- Hitung biaya dari tabel harga di config
- Auto-hapus data > 30 hari (`time.Ticker` + context)
- Halaman riwayat + detail di dashboard

> **Pelajaran Go:** producer/consumer, non-blocking send, `time.Ticker`, `go test -race`
> **Bukti:** `go test -race ./...` hijau · **matikan Postgres → request tetap jalan, cuma log-nya hilang** · biaya cocok sama tagihan provider

### M4 — Virtual key + kuota · ~8 jam · target **19 Okt 2026**
- Virtual key (disimpan sebagai hash), CRUD via `/admin/keys`
- Rate limit per key (`x/time/rate`), batas budget bulanan
- **Lumen dipindah lewat Sanmon** — ubah `LlmClient.php` + `Embedder.php` ke endpoint OpenAI-compat Gemini, base URL jadi bisa diatur lewat env
- **Wajib:** env var di Lumen buat balik nembak Gemini langsung dalam satu baris

> **Pelajaran Go:** middleware chaining, token bucket, `sync`/`atomic`
> **Bukti:** key yang budget-nya habis ditolak, **key lain tetap jalan** · Lumen jalan normal lewat gateway · saklar balik terbukti bisa dipakai

### M5 — Beres-beres + naik VPS · ~8 jam · target **2 Nov 2026**
- Dashboard lengkap: filter, agregat harian, kelola key
- Build React → `embed.FS`, catch-all rute SPA balikin `index.html`
- Deploy VPS: binary + systemd, dengerin **localhost saja**, nginx di depan, `.env` chmod 600
- README + bagian trade-off
- Repo dijadikan **public**

> **Bukti:** `scp` satu binary → jalan · dashboard kebuka lewat domain/nginx · refresh di halaman detail tidak 404

---

## 8. Kriteria Selesai (MVP)

Selesai kalau **enam-enamnya** terpenuhi:

1. Lumen jalan lewat Sanmon; perubahan di Lumen cuma URL + env, bukan logika
2. Dashboard nampilin tiap request: model, token, biaya, latency, status
3. Key yang budget-nya habis ditolak, **key lain tidak ikut kena**
4. `go test -race ./...` hijau, dengan test untuk: streaming passthrough, context cancel, parsing usage, hitung biaya, rate limit
5. **Postgres dimatiin → request tetap jalan**
6. Dipakai 2 konsumen (CLI lamaran + Lumen) selama **1 minggu** tanpa restart darurat

Setelah itu: tulis README + trade-off → repo dijadikan public → **berhenti.**

> **Fase 2 dan 3 sifatnya opsional. Project ini sah dinyatakan selesai setelah MVP.**

---

## 9. Fase lanjutan (opsional)

**Fase 2 — Cache & fallback (~1 minggu).** Exact-hash cache + `singleflight`; fallback
Gemini → OpenRouter kalau kena kuota.
Ini bukan fitur pamer: Lumen sudah punya `GeminiQuotaExceededException` yang kerjanya
cuma bilang "coba lagi besok". Fallback nambal bug yang exception-nya sudah ditulis.
**Batas keras: maksimal 1 kali retry.** Request gagal tetap menghabiskan kuota
OpenRouter (50/hari tanpa kredit) — retry yang bocor jadi loop bikin cadangan mati
justru pas dibutuhin.

**Fase 3 — Replay & eval (~2 minggu).** Ambil request tersimpan → jadikan dataset →
ulang ke model/prompt lain → bandingkan biaya, latency, dan hasilnya.
Ini alasan `request_body` disimpan penuh — tidak bisa mengulang yang tidak disimpan.

---

## 10. Aturan keras

Delapan hal yang kalau dilanggar bikin fitur bohong diam-diam atau layanan mati:

1. **Jalur proxy tidak pernah nunggu DB.** `select` + `default`, log dibuang kalau penuh
2. **Uang disimpan integer micro-USD**, bukan float. `0.1 + 0.2 != 0.3` bikin total meleset pelan-pelan tanpa ada yang sadar
3. **Model tanpa harga di config → tandai `cost_unknown` + peringatan di dashboard.** Nol yang jujur, bukan nol yang bohong
4. **Gateway nyisipin `stream_options.include_usage`.** Kalau tidak, streaming pulang tanpa angka token → biaya nol tanpa error apa pun
5. **Maksimal 1 retry.** Request gagal tetap makan kuota
6. **API key provider tidak pernah masuk tabel log**
7. **Auth ada sejak M1**, bukan M4
8. **Rute SPA punya catch-all ke `index.html`**, kalau tidak refresh di halaman detail 404

## 11. Risiko

| Risiko | Mitigasi |
|---|---|
| Sanmon mati → Lumen ikut mati | Env var di Lumen buat balik langsung ke Gemini. Diuji sebelum deploy, bukan sesudah |
| Postgres mati → gateway ikut mati | Aturan keras #1 |
| Kuota OpenRouter habis gara-gara retry | Aturan keras #5 |
| Keasyikan ngecat UI, backend mandek | Alarm: **kalau satu sesi lewat tanpa nambah endpoint Go, UI-nya sudah mulai makan** |
| Magang keburu dapat, jam turun ke ~2/minggu | Garis finish resmi dipotong ke **M0–M3 + README**. Tetap terhitung selesai, bukan mangkrak |
| Scope bocor lewat ide menarik | §3, plus aturan pengaman "butuh tabel baru = masuk daftar Bukan Tujuan" |
| HeroUI v3 masih sangat baru | Dipakai cuma buat komponen berperilaku ribet; wajah project ditulis tangan (DESIGN.md) |

## 12. Catatan operasional

- **Port:** gateway `8777`, dashboard dev `8778`. Dipilih karena `8000` bentrok dengan
  purely-grocery & fineiro, `3000` bentrok dengan fineiro, purely, agentic-cockpit
- **Repo:** private dulu → public saat 6 kriteria terpenuhi. Repo publik berisi 3 commit
  scaffolding tanpa README lebih jelek daripada tidak ada repo
- **Log sesi:** `~/Documents/Nexus/02-Areas/Personal/sanmon/log.md`
- **Lisensi:** MIT (saat dijadikan public)

## 13. Keputusan tercatat

| Keputusan | Alasan |
|---|---|
| stdlib, bukan Gin | Gin membungkus `ResponseWriter` persis di bagian tersulit; jasa utamanya tidak relevan buat proxy; sudah dipakai di Klora |
| Postgres, bukan SQLite | Sudah dikuasai → jatah belajar habis di Go. Pola channel+worker tetap dipakai (alasannya jalur proxy tidak boleh nunggu DB, bukan keterbatasan SQLite) |
| React + HeroUI, bukan Nuxt | HeroUI v3 React-only. Nol skill frontend baru, ditukar waktu tambahan buat Go |
| Satu dialek, bukan dua | Gemini & OpenRouter dua-duanya OpenAI-compat. Hemat ~1 minggu |
| Gemini + OpenRouter, bukan Anthropic | Tidak punya Anthropic API key |
| Dashboard di M0 (kulit) & M2 (nyata) | Motivasi itu kendala nyata: 7 repo mangkrak di initial commit. 3 minggu tanpa hasil terlihat itu terlalu lama |
| CLI lamaran jadi konsumen pertama, Lumen menyusul M4 | Lumen sudah hidup di VPS — jangan diutak-atik sampai gateway terbukti jalan |
| Simpan prompt penuh | Fase 3 mustahil tanpa itu. Dipagari: saklar `LOG_BODIES`, auto-hapus 30 hari, key tidak ikut tersimpan |
