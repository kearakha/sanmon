# Sanmon

> 山門 — gerbang utama kuil: yang di depan, yang wajib dilewati.

Gateway self-hosted buat trafik LLM. Aplikasi nembak ke Sanmon, Sanmon nerusin ke
provider, dan setiap request kecatat: model apa, berapa token, berapa biaya, berapa
lama, sukses atau gagal.

Perubahan di sisi aplikasi cuma satu: `base_url`.

## Status

M0–M5 kelar — proxy streaming, auth virtual key, live feed SSE, penyimpanan +
hitung biaya, kuota per key, dashboard lengkap, dan bundel React ditanam ke binary.
Sisa: naik VPS.

| Milestone | Isi | Status |
|---|---|---|
| M0 | Fondasi + kulit dashboard | ✅ |
| M1 | Proxy streaming + auth | ✅ |
| M2 | Live feed SSE | ✅ |
| M3 | Penyimpanan + hitung biaya | ✅ |
| M4 | Virtual key + kuota | ✅ |
| M5 | Beres-beres + deploy | 🚧 deploy VPS |

Rencana lengkap, kriteria selesai, dan aturan keras ada di [`.docs/PRD.md`](.docs/PRD.md).

## Arsitektur

```
CLI lamaran ─┐
             ├──→ [ SANMON :8777 ] ──→ Gemini (OpenAI-compat)
Lumen ───────┘          │
                   channel (non-blocking)
                        ↓
                  worker goroutine
                        ↓
                   PostgreSQL          ←── dashboard React (embedded, :8778 saat dev)
```

**Jalur proxy tidak pernah nunggu DB.** Kiriman ke channel log pakai `select` +
`default` — antrian penuh atau Postgres mati, log dibuang, request tetap jalan.
Log boleh hilang, layanan tidak boleh.

## Stack

**Backend** Go 1.26, stdlib `net/http` + ServeMux (bukan framework), `log/slog`,
PostgreSQL via `pgx`, migrasi `goose`
**Frontend** Vite + React + Tailwind v4 (ditulis tangan) + GSAP, ditanam ke binary
via `embed.FS`
**Provider** Gemini (utama, endpoint OpenAI-compat), OpenRouter (cadangan, Fase 2)

## Menjalankan (dev)

```sh
# 1. Postgres lokal (port 5435)
docker compose up -d

# 2. Migrasi
goose -dir migrations postgres "$SANMON_DATABASE_URL" up

# 3. Rahasia
cp .env.example .env   # isi GEMINI_API_KEY + SANMON_ADMIN_TOKEN

# 4. Gateway (:8777)
go run .

# 5. Dashboard dev (:8778) — buka http://localhost:8778/?token=<SANMON_ADMIN_TOKEN>
cd web && npm run dev
```

`go test -race ./...` buat semua test.

## Build & deploy

Bundel React di-commit ke repo (`web/dist`) supaya `//go:embed` bisa build tanpa
langkah CI — deploy tetap "scp satu binary".

```sh
# tiap ubah frontend:
cd web && npm run build && git add dist && cd ..

# binary Linux:
GOOS=linux GOARCH=amd64 go build -o sanmon .

# ke VPS: scp sanmon + config.yaml + .env (chmod 600)
```

Di VPS: systemd unit dengerin `127.0.0.1:8777` saja, nginx di depan (TLS +
`proxy_pass` ke localhost). Dashboard diakses `https://domain/?token=<TOKEN>`.

## Config

`config.yaml` — peta alias model → provider + nama model asli + harga per 1 juta
token (satuan micro-USD, `750000` = $0.75). Harga di config, bukan di kode, karena
harga provider berubah terus. Model tanpa harga → ditandai `cost_unknown`, bukan
biaya nol yang bohong.

Rahasia lewat env (`.env`), tidak pernah masuk YAML:

| Env | Isi |
|---|---|
| `GEMINI_API_KEY` | API key provider — tidak pernah masuk tabel log |
| `SANMON_ADMIN_TOKEN` | token dashboard + endpoint `/admin/*` |
| `SANMON_DATABASE_URL` | DSN Postgres |
| `SANMON_PORT` | opsional, override `port` di YAML |

## Endpoint

| Rute | Auth |
|---|---|
| `POST /v1/chat/completions`, `POST /v1/embeddings`, `GET /v1/models` | `Authorization: Bearer <virtual key>` |
| `GET /admin/requests`, `/admin/requests/{id}`, `/admin/stream` (SSE), `/admin/stats` | `?token=<admin token>` |
| `GET\|POST\|DELETE /admin/keys` | `?token=<admin token>` |
| semua rute non-file lain | — (catch-all → `index.html`) |
| `GET /healthz` | — |

Virtual key dibikin lewat `POST /admin/keys` (disimpan sebagai hash), bukan env var.
Tiap key punya batas rpm + budget bulanan; key yang budgetnya habis ditolak, key
lain tetap jalan.

## Kenapa dibangun

Bukan karena belum ada — LiteLLM, Bifrost, Portkey, Helicone semua sudah ada. Tapi
membangun ulang barang yang sudah terbukti adalah cara paling aman buat mendalami
Go, dan masalahnya nyata: aplikasi lain saya pernah kepentok biaya satu provider dan
harus ngoprek kode buat pindah.

## Trade-off

Keputusan yang diambil sadar, plus yang ditolak:

| Pilihan | Alasan |
|---|---|
| stdlib `net/http`, bukan Gin | Bagian tersulit proxy justru `Flusher` / `io.Copy` / context cancel — persis yang Gin bungkus. Routing + binding JSON nggak relevan buat proxy |
| PostgreSQL, bukan SQLite | Sudah dikuasai → jatah belajar habis di Go. Pola channel+worker tetap dipakai karena aturan "proxy nggak nunggu DB", bukan keterbatasan SQLite |
| React + Tailwind tangan, bukan Nuxt / component lib | Server sudah Go — Next/Nuxt nambah runtime Node yang nggak perlu. Nol skill frontend baru, ditukar waktu balik ke Go |
| Satu dialek (format OpenAI), bukan dua | Gemini & OpenRouter dua-duanya OpenAI-compat. Hemat ~1 minggu |
| Gemini + OpenRouter, bukan Anthropic | Tidak punya Anthropic API key (langganan Claude Code ≠ API key) |
| Dashboard dua fase: kulit di M0, data asli di M2 | 7 repo mangkrak di initial commit — 3 minggu tanpa hasil terlihat kelamaan |
| Simpan `request_body` penuh | Fase 3 (replay & eval) mustahil tanpa itu. Dipagari: saklar `log_bodies` per key, auto-hapus 30 hari, key nggak ikut tersimpan |
| `web/dist` di-commit ke repo | Solo project tanpa CI — "scp satu binary" tetap utuh. Konsekuensi: tiap ubah frontend harus `npm run build` + commit `dist` |
| Token dashboard di `?token=` runtime, bukan build-time env | Bundel yang di-embed + repo publik nggak boleh nyimpen token. `?token=` sudah dipakai di semua call API + SSE, jadi bukan pola auth baru |

## Dokumen

- [`.docs/PRD.md`](.docs/PRD.md) — tujuan, bukan-tujuan, milestone, kriteria selesai, aturan keras
- [`.docs/DESIGN.md`](.docs/DESIGN.md) — arah visual, palet, batas animasi

## Lisensi

MIT
