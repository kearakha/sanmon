# DESIGN — Sanmon

Wajib dibaca sebelum nulis komponen. Kalau ragu, pilih yang lebih polos.

---

## Arah

**Bahasa visual: instrumen ukur.** Rujukannya panel kokpit, osiloskop, alat ukur
laboratorium — bukan "produk AI". Sanmon itu alat pantau; tampilannya harus terasa
seperti alat, bukan seperti landing page startup.

Kerapatan informasi tinggi, garis tipis, angka monospace, satu warna aksen.

## Palet

| Peran | Nilai |
|---|---|
| Aksen | `#E8541E` (ember) |
| Latar | hitam pekat |
| Teks | putih pudar, abu untuk sekunder |
| Status | sukses / gagal / partial — pakai kekuatan aksen & abu, bukan hijau-merah-kuning penuh |

Konsisten dengan Agentic Cockpit. Gaya yang sama lintas project kebaca sebagai punya
selera, bukan ganti tema tiap repo.

## Larangan

Ini yang bikin sebuah UI kelihatan "AI banget":

- Gradien ungu–biru
- Glassmorphism / blur di mana-mana
- Glow, neon
- Ikon sparkles, emoji di UI
- Font Inter bawaan
- Semua kartu `rounded-2xl` seragam
- Hijau sebagai warna utama

## Angka

Semua angka (token, biaya, latency) pakai **monospace + `tabular-nums`**.
Angka yang bergeser posisinya saat berubah bikin dashboard terasa murahan — dan bikin
susah dibaca saat live feed jalan.

Biaya disimpan integer micro-USD, dikonversi hanya saat ditampilkan.

## HeroUI: sampai mana

> **HeroUI dipakai buat komponen yang ribet perilakunya** — dialog, dropdown, tabel,
> toast, tooltip, fokus & keyboard nav.
>
> **Yang jadi wajah project ditulis tangan** — live feed, tampilan angka, panel detail.
>
> Tema HeroUI di-override ke palet ember + monospace `tabular-nums`.

Alasannya: HeroUI "cakep dari sananya", dan itu justru lawan dari tujuan kita. Ambil
perilakunya, tolak tampilannya.

## Animasi — cuma di 3 tempat

Dibatasi di depan, pola yang sama dengan daftar Bukan Tujuan di PRD.

1. **Baris request masuk** ke live feed
2. **Angka berubah** — token, biaya, latency
3. **Transisi baris → panel detail**

Sisanya polos. Hover cukup ganti warna.

**Dilarang menyentuh animasi sebelum M2.**

### Tekniknya

| Kebutuhan | Pakai |
|---|---|
| Baris → panel detail | **View Transitions API** — native, cross-browser, 0 KB JS. Rasio hasil-per-baris-kode paling tinggi |
| Angka nge-roll (odometer) | **GSAP** — sudah dikuasai dari porto-web-2, gratis termasuk semua plugin |
| Baris masuk | GSAP atau **Motion** (React-first) |
| Landing page: kinetic typography, scroll | CSS scroll-driven (`animation-timeline: scroll()`) + GSAP + Lenis |

Yang bikin live feed terasa "wah" bukan animasinya — tapi karena datanya **memang**
mengalir sungguhan. Angka token naik karena token beneran datang, bukan karena
di-`setInterval`. Kejujuran itu yang nggak bisa ditiru template.

## Halaman

1. **Live feed** — wajah project. Aliran request masuk realtime
2. **Riwayat** — tabel, filter, pagination
3. **Detail** — satu request: request/response penuh, token, biaya, timeline
4. **Keys** (M4) — kelola virtual key, kuota, budget

**Tidak ada halaman Settings.** Konfigurasi ada di file YAML.

## Landing page

Dikerjakan **paling akhir**, setelah 6 kriteria selesai terpenuhi.

Alasannya jujur: landing page nol pelajaran Go, dan itu persis jenis pekerjaan yang
nyangkutin `stillwork` dan `porto-web-2`. Taruh di akhir — sekalian jadi bahan konten.

## Aksesibilitas

Tidak dikorbankan meski solo project: fokus terlihat jelas, kontras cukup, live feed
pakai `aria-live` yang sopan (jangan bacakan tiap baris), tabel pakai markup tabel
beneran. HeroUI (React Aria) sudah menangani sebagian besar ini — itu alasan utama
dipakai.
