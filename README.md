# Sanmon

> 山門 — gerbang utama kuil: yang di depan, yang wajib dilewati.

Gateway self-hosted buat trafik LLM. Aplikasi nembak ke Sanmon, Sanmon nerusin ke
provider, dan setiap request kecatat: model apa, berapa token, berapa biaya, berapa
lama, sukses atau gagal.

Perubahan di sisi aplikasi cuma satu: `base_url`.

```
aplikasi ──→ [ SANMON ] ──→ Gemini / OpenRouter
                  │
                  ↓
             PostgreSQL ──→ dashboard
```

## Status

🚧 **Belum mulai.** Rencana lengkap ada di [`.docs/PRD.md`](.docs/PRD.md).

| Milestone | Isi | Target |
|---|---|---|
| M0 | Fondasi + kulit dashboard | 25 Agu 2026 |
| M1 | Proxy streaming + auth | 8 Sep 2026 |
| M2 | Live feed SSE | 22 Sep 2026 |
| M3 | Penyimpanan + hitung biaya | 5 Okt 2026 |
| M4 | Virtual key + kuota | 19 Okt 2026 |
| M5 | Beres-beres + deploy | 2 Nov 2026 |

## Stack

**Backend** Go 1.26, stdlib `net/http` (bukan framework), PostgreSQL, `goose`
**Frontend** Vite + React + HeroUI v3 + Tailwind v4, ditanam ke binary via `embed.FS`
**Provider** Gemini (utama), OpenRouter (cadangan)

## Kenapa dibangun

Bukan karena belum ada — LiteLLM, Bifrost, Portkey, Helicone semua sudah ada.
Tapi membangun ulang barang yang sudah terbukti adalah cara paling aman buat mendalami
Go, dan masalahnya nyata: aplikasi lain saya pernah kepentok biaya satu provider dan
harus ngoprek kode buat pindah.

## Dokumen

- [`.docs/PRD.md`](.docs/PRD.md) — tujuan, bukan-tujuan, milestone, kriteria selesai, aturan keras
- [`.docs/DESIGN.md`](.docs/DESIGN.md) — arah visual, palet, batas animasi

## Lisensi

MIT
