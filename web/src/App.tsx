import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { flushSync } from "react-dom";
import gsap from "gsap";

// Cocok sama Event di hub.go. Token & biaya belum ada — nyusul M3, jangan
// diisi 0 palsu (aturan keras #3).
type LiveEvent = {
  id: number;
  time: string;
  model: string;
  stream: boolean;
  status_code: number;
  latency_ms: number;
  partial?: boolean;
  error?: string;
};

// Cocok sama RequestRow di admin.go (GET /admin/requests + /{id}). Field yang
// bisa NULL di tabel dibikin nullable di sini juga.
type RequestRow = {
  id: number;
  created_at: string;
  model_requested: string;
  provider: string;
  model_resolved: string | null;
  stream: boolean;
  status_code: number | null;
  error: string | null;
  latency_ms: number | null;
  tokens_in: number | null;
  tokens_out: number | null;
  cost_micro_usd: number | null;
  cost_unknown: boolean;
  partial: boolean;
  // cuma keisi di endpoint detail; NULL sampai log_bodies aktif di M4
  ttfb_ms?: number | null;
  request_body?: string | null;
  response_body?: string | null;
};

// Cocok sama DailyStat di stats.go (GET /admin/stats). cost_micro_usd cuma
// baris cost_unknown=false; baris tanpa harga kehitung di cost_unknown_count.
type DailyStat = {
  day: string;
  requests: number;
  tokens_in: number;
  tokens_out: number;
  cost_micro_usd: number;
  cost_unknown_count: number;
  errors: number;
};

// Cocok sama KeyRow di keys.go (GET /admin/keys). Field pointer di Go →
// nullable di sini.
type KeyRow = {
  id: number;
  name: string;
  monthly_budget_micro_usd: number | null;
  rpm_limit: number | null;
  log_bodies: boolean;
  disabled: boolean;
  created_at: string;
};

const GATEWAY_URL = "http://localhost:8777";
// Token dibaca dari ?token= saat runtime (deploy: buka dashboard dengan
// ?token=...), fallback ke env buat dev. Bundel yang di-embed ke binary
// nggak boleh nyimpen token — repo publik.
const ADMIN_TOKEN =
  new URLSearchParams(location.search).get("token") ||
  (import.meta.env.VITE_SANMON_ADMIN_TOKEN as string | undefined);
const MAX_ROWS = 50;
const PAGE_SIZE = 50;

function statusLabel(ev: {
  partial?: boolean | null;
  error?: string | null;
  status_code: number | null;
}): "OK" | "ERR" | "PARTIAL" {
  if (ev.partial) return "PARTIAL";
  if (ev.error || (ev.status_code ?? 0) >= 400) return "ERR";
  return "OK";
}

function formatTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleTimeString("id-ID", { hour12: false });
}

// Biaya disimpan integer micro-USD (aturan keras #2), dikonversi cuma pas
// ditampilkan (DESIGN.md §Angka).
function formatCost(row: {
  cost_micro_usd: number | null;
  cost_unknown: boolean;
}): string {
  if (row.cost_unknown) return "?";
  if (row.cost_micro_usd == null) return "-";
  return `$${(row.cost_micro_usd / 1_000_000).toFixed(6)}`;
}

// Budget bulanan per key disimpan integer micro-USD (aturan keras #2).
function formatBudget(micro: number | null): string {
  if (micro == null) return "-";
  return `$${(micro / 1_000_000).toFixed(2)}`;
}

// Titik 1 & 2: baris masuk (fade + geser) dibarengin angka latency ngeroll
// dari 0 ke nilai asli — satu animasi GSAP, bukan dua yang terpisah.
function animateRowIn(
  row: HTMLTableRowElement,
  latencyEl: HTMLElement,
  latency: number,
) {
  const counter = { value: 0 };
  gsap.fromTo(
    row,
    { opacity: 0, y: -8 },
    { opacity: 1, y: 0, duration: 0.25, ease: "power1.out" },
  );
  gsap.to(counter, {
    value: latency,
    duration: 0.25,
    ease: "power1.out",
    onUpdate: () => {
      latencyEl.textContent = `${Math.round(counter.value)}ms`;
    },
  });
}

function DetailPanel({ ev, onClose }: { ev: LiveEvent; onClose: () => void }) {
  return (
    <div
      style={{ viewTransitionName: "detail-panel" }}
      className="border border-[var(--border)] bg-[var(--panel)] p-6"
    >
      <div className="flex items-baseline justify-between mb-4">
        <h2 className="text-sm tracking-widest text-[var(--text-dim)] uppercase">
          Detail request
        </h2>
        <button
          onClick={onClose}
          className="text-xs text-[var(--text-dim)] hover:text-[var(--text)]"
        >
          tutup
        </button>
      </div>
      <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 font-mono-nums text-sm">
        <dt className="text-[var(--text-dim)]">waktu</dt>
        <dd>{formatTime(ev.time)}</dd>
        <dt className="text-[var(--text-dim)]">model</dt>
        <dd>{ev.model}</dd>
        <dt className="text-[var(--text-dim)]">stream</dt>
        <dd>{ev.stream ? "ya" : "-"}</dd>
        <dt className="text-[var(--text-dim)]">latency</dt>
        <dd>{ev.latency_ms}ms</dd>
        <dt className="text-[var(--text-dim)]">status</dt>
        <dd
          className={
            statusLabel(ev) === "OK"
              ? "text-[var(--text-dim)]"
              : "text-[var(--accent)]"
          }
        >
          {statusLabel(ev)}
        </dd>
        {ev.error && (
          <>
            <dt className="text-[var(--text-dim)]">error</dt>
            <dd className="text-[var(--accent)]">{ev.error}</dd>
          </>
        )}
      </dl>
    </div>
  );
}

function Row({
  ev,
  animatedIds,
  onSelect,
}: {
  ev: LiveEvent;
  animatedIds: React.RefObject<Set<number>>;
  onSelect: () => void;
}) {
  const rowRef = useRef<HTMLTableRowElement>(null);
  const latencyRef = useRef<HTMLTableCellElement>(null);
  // Baris cuma dianimasikan sekali seumur hidup id-nya. Tanpa ini, buka/tutup
  // panel detail bikin table remount dan baris yang sama kena animasi masuk
  // lagi padahal bukan baris baru.
  const shouldAnimate = !animatedIds.current.has(ev.id);

  useLayoutEffect(() => {
    if (shouldAnimate && rowRef.current && latencyRef.current) {
      animatedIds.current.add(ev.id);
      animateRowIn(rowRef.current, latencyRef.current, ev.latency_ms);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <tr
      ref={rowRef}
      onClick={onSelect}
      style={{ viewTransitionName: `row-${ev.id}` }}
      className="border-b border-[var(--border)] cursor-pointer hover:bg-[var(--panel)]"
    >
      <td className="py-2 text-[var(--text-dim)]">{formatTime(ev.time)}</td>
      <td className="py-2">{ev.model}</td>
      <td className="py-2 text-right">{ev.stream ? "ya" : "-"}</td>
      <td ref={latencyRef} className="py-2 text-right">
        {shouldAnimate ? "0ms" : `${ev.latency_ms}ms`}
      </td>
      <td
        className={`py-2 text-right ${
          statusLabel(ev) === "OK"
            ? "text-[var(--text-dim)]"
            : "text-[var(--accent)]"
        }`}
      >
        {statusLabel(ev)}
      </td>
    </tr>
  );
}

// RequestDetail ambil satu request lengkap dari GET /admin/requests/{id}.
// State switch polos, bukan View Transitions — perlu fetch async dulu, dan
// titik animasi #3 (baris -> panel) udah kepenuhin di live feed.
function RequestDetail({ id, onClose }: { id: number; onClose: () => void }) {
  const [row, setRow] = useState<RequestRow | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!ADMIN_TOKEN) return;
    let alive = true;
    fetch(
      `${GATEWAY_URL}/admin/requests/${id}?token=${encodeURIComponent(ADMIN_TOKEN)}`,
    )
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json();
      })
      .then((data: RequestRow) => {
        if (alive) setRow(data);
      })
      .catch(() => {
        if (alive) setError("gagal memuat detail");
      });
    return () => {
      alive = false;
    };
  }, [id]);

  return (
    <div className="border border-[var(--border)] bg-[var(--panel)] p-6">
      <div className="flex items-baseline justify-between mb-4">
        <h2 className="text-sm tracking-widest text-[var(--text-dim)] uppercase">
          Detail request #{id}
        </h2>
        <button
          onClick={onClose}
          className="text-xs text-[var(--text-dim)] hover:text-[var(--text)]"
        >
          tutup
        </button>
      </div>

      {error && <p className="text-sm text-[var(--accent)]">{error}</p>}
      {!row && !error && (
        <p className="font-mono-nums text-xs text-[var(--text-dim)]">
          memuat...
        </p>
      )}

      {row && (
        <>
          <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 font-mono-nums text-sm">
            <dt className="text-[var(--text-dim)]">waktu</dt>
            <dd>
              {new Date(row.created_at).toLocaleString("id-ID", {
                hour12: false,
              })}
            </dd>
            <dt className="text-[var(--text-dim)]">model diminta</dt>
            <dd>{row.model_requested}</dd>
            <dt className="text-[var(--text-dim)]">model asli</dt>
            <dd>{row.model_resolved ?? "-"}</dd>
            <dt className="text-[var(--text-dim)]">provider</dt>
            <dd>{row.provider}</dd>
            <dt className="text-[var(--text-dim)]">stream</dt>
            <dd>{row.stream ? "ya" : "-"}</dd>
            <dt className="text-[var(--text-dim)]">status</dt>
            <dd
              className={
                statusLabel(row) === "OK"
                  ? "text-[var(--text-dim)]"
                  : "text-[var(--accent)]"
              }
            >
              {statusLabel(row)} {row.status_code ?? ""}
            </dd>
            <dt className="text-[var(--text-dim)]">latency</dt>
            <dd>{row.latency_ms ?? 0}ms</dd>
            <dt className="text-[var(--text-dim)]">ttfb</dt>
            <dd>{row.ttfb_ms != null ? `${row.ttfb_ms}ms` : "-"}</dd>
            <dt className="text-[var(--text-dim)]">token in/out</dt>
            <dd>
              {row.tokens_in ?? 0} / {row.tokens_out ?? 0}
            </dd>
            <dt className="text-[var(--text-dim)]">biaya</dt>
            <dd>
              {formatCost(row)}
              {row.cost_unknown ? " (tak diketahui)" : ""}
            </dd>
            <dt className="text-[var(--text-dim)]">partial</dt>
            <dd>{row.partial ? "ya" : "-"}</dd>
            {row.error && (
              <>
                <dt className="text-[var(--text-dim)]">error</dt>
                <dd className="text-[var(--accent)]">{row.error}</dd>
              </>
            )}
          </dl>

          <div className="mt-6 grid gap-4">
            <div>
              <div className="mb-1 text-xs tracking-widest text-[var(--text-dim)] uppercase">
                request body
              </div>
              <pre className="overflow-x-auto border border-[var(--border)] p-3 font-mono-nums text-xs text-[var(--text-dim)]">
                {row.request_body ?? "— (belum direkam)"}
              </pre>
            </div>
            <div>
              <div className="mb-1 text-xs tracking-widest text-[var(--text-dim)] uppercase">
                response body
              </div>
              <pre className="overflow-x-auto border border-[var(--border)] p-3 font-mono-nums text-xs text-[var(--text-dim)]">
                {row.response_body ?? "— (belum direkam)"}
              </pre>
            </div>
          </div>
        </>
      )}
    </div>
  );
}

// HistoryPage: tabel riwayat dari GET /admin/requests. Pagination keyset
// (before_id) — "muat lebih", bukan nomor halaman. Filter model/status/tanggal
// di-debounce biar nggak nembak tiap ketikan. initialSince/initialUntil diisi
// pas dateng dari klik baris di halaman Harian.
function HistoryPage({
  initialSince = "",
  initialUntil = "",
}: {
  initialSince?: string;
  initialUntil?: string;
}) {
  const [rows, setRows] = useState<RequestRow[]>([]);
  const [nextBeforeId, setNextBeforeId] = useState<number | null>(null);
  const [loading, setLoading] = useState(false);
  const [model, setModel] = useState("");
  const [status, setStatus] = useState("");
  const [since, setSince] = useState(initialSince);
  const [until, setUntil] = useState(initialUntil);
  const [selectedId, setSelectedId] = useState<number | null>(null);

  function buildURL(beforeId: number | null): string {
    const p = new URLSearchParams({
      token: ADMIN_TOKEN ?? "",
      limit: String(PAGE_SIZE),
    });
    if (model.trim()) p.set("model", model.trim());
    if (status.trim()) p.set("status", status.trim());
    if (since) p.set("since", since);
    if (until) p.set("until", until);
    if (beforeId != null) p.set("before_id", String(beforeId));
    return `${GATEWAY_URL}/admin/requests?${p}`;
  }

  async function fetchPage(reset: boolean) {
    if (!ADMIN_TOKEN) return;
    setLoading(true);
    try {
      const res = await fetch(buildURL(reset ? null : nextBeforeId));
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = (await res.json()) as {
        requests: RequestRow[];
        next_before_id: number | null;
      };
      setRows((prev) => (reset ? data.requests : [...prev, ...data.requests]));
      setNextBeforeId(data.next_before_id);
    } catch {
      // biarin — tabel nggak nambah, bukan crash
    } finally {
      setLoading(false);
    }
  }

  // Refetch dari awal tiap filter berubah, debounce biar nggak spam.
  useEffect(() => {
    const t = setTimeout(() => fetchPage(true), 250);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [model, status, since, until]);

  if (!ADMIN_TOKEN) {
    return (
      <p className="font-mono-nums text-xs text-[var(--text-dim)]">
        VITE_SANMON_ADMIN_TOKEN belum diset
      </p>
    );
  }

  if (selectedId != null) {
    return (
      <RequestDetail id={selectedId} onClose={() => setSelectedId(null)} />
    );
  }

  return (
    <div>
      <div className="mb-4 flex gap-3 font-mono-nums text-xs">
        <input
          value={model}
          onChange={(e) => setModel(e.target.value)}
          placeholder="filter model"
          className="border border-[var(--border)] bg-[var(--panel)] px-2 py-1 text-[var(--text)] outline-none placeholder:text-[var(--text-dim)] focus:border-[var(--accent)]"
        />
        <input
          value={status}
          onChange={(e) => setStatus(e.target.value)}
          placeholder="status"
          inputMode="numeric"
          className="w-24 border border-[var(--border)] bg-[var(--panel)] px-2 py-1 text-[var(--text)] outline-none placeholder:text-[var(--text-dim)] focus:border-[var(--accent)]"
        />
        <input
          type="date"
          value={since}
          onChange={(e) => setSince(e.target.value)}
          aria-label="dari tanggal"
          className="border border-[var(--border)] bg-[var(--panel)] px-2 py-1 text-[var(--text)] outline-none focus:border-[var(--accent)]"
        />
        <input
          type="date"
          value={until}
          onChange={(e) => setUntil(e.target.value)}
          aria-label="sampai tanggal"
          className="border border-[var(--border)] bg-[var(--panel)] px-2 py-1 text-[var(--text)] outline-none focus:border-[var(--accent)]"
        />
      </div>

      <table className="w-full border-collapse text-sm">
        <thead>
          <tr className="border-b border-[var(--border)] text-left text-[var(--text-dim)]">
            <th className="py-2 font-normal">waktu</th>
            <th className="py-2 font-normal">model</th>
            <th className="py-2 font-normal text-right">stream</th>
            <th className="py-2 font-normal text-right">tok in/out</th>
            <th className="py-2 font-normal text-right">biaya</th>
            <th className="py-2 font-normal text-right">latency</th>
            <th className="py-2 font-normal text-right">status</th>
          </tr>
        </thead>
        <tbody className="font-mono-nums">
          {rows.map((row) => (
            <tr
              key={row.id}
              onClick={() => setSelectedId(row.id)}
              className="border-b border-[var(--border)] cursor-pointer hover:bg-[var(--panel)]"
            >
              <td className="py-2 text-[var(--text-dim)]">
                {formatTime(row.created_at)}
              </td>
              <td className="py-2">{row.model_requested}</td>
              <td className="py-2 text-right">{row.stream ? "ya" : "-"}</td>
              <td className="py-2 text-right">
                {row.tokens_in ?? 0}/{row.tokens_out ?? 0}
              </td>
              <td className="py-2 text-right">{formatCost(row)}</td>
              <td className="py-2 text-right">{row.latency_ms ?? 0}ms</td>
              <td
                className={`py-2 text-right ${
                  statusLabel(row) === "OK"
                    ? "text-[var(--text-dim)]"
                    : "text-[var(--accent)]"
                }`}
              >
                {statusLabel(row)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {rows.length === 0 && !loading && (
        <p className="py-4 font-mono-nums text-xs text-[var(--text-dim)]">
          belum ada request
        </p>
      )}

      {nextBeforeId != null && (
        <button
          onClick={() => fetchPage(false)}
          disabled={loading}
          className="mt-4 border border-[var(--border)] px-3 py-1 font-mono-nums text-xs text-[var(--text-dim)] hover:border-[var(--accent)] hover:text-[var(--text)] disabled:opacity-50"
        >
          {loading ? "memuat..." : "muat lebih"}
        </button>
      )}
    </div>
  );
}

// DailyPage: agregat harian dari GET /admin/stats, terbaru dulu. Klik baris →
// buka Riwayat yang udah ke-filter ke tanggal itu (since = until = hari itu).
// Tanpa animasi — DESIGN.md batasin gerak cuma di 3 tempat, ini bukan salah
// satunya.
function DailyPage({ onPickDay }: { onPickDay: (day: string) => void }) {
  const [days, setDays] = useState<DailyStat[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!ADMIN_TOKEN) return;
    let alive = true;
    fetch(`${GATEWAY_URL}/admin/stats?token=${encodeURIComponent(ADMIN_TOKEN)}`)
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json();
      })
      .then((data: { days: DailyStat[] }) => {
        if (alive) setDays(data.days);
      })
      .catch(() => {
        if (alive) setError("gagal memuat agregat");
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, []);

  if (!ADMIN_TOKEN) {
    return (
      <p className="font-mono-nums text-xs text-[var(--text-dim)]">
        VITE_SANMON_ADMIN_TOKEN belum diset
      </p>
    );
  }

  return (
    <div>
      <table className="w-full border-collapse text-sm">
        <thead>
          <tr className="border-b border-[var(--border)] text-left text-[var(--text-dim)]">
            <th className="py-2 font-normal">tanggal</th>
            <th className="py-2 font-normal text-right">request</th>
            <th className="py-2 font-normal text-right">tok in/out</th>
            <th className="py-2 font-normal text-right">biaya</th>
            <th className="py-2 font-normal text-right">error</th>
          </tr>
        </thead>
        <tbody className="font-mono-nums">
          {days.map((d) => (
            <tr
              key={d.day}
              onClick={() => onPickDay(d.day)}
              className="border-b border-[var(--border)] cursor-pointer hover:bg-[var(--panel)]"
            >
              <td className="py-2 text-[var(--text-dim)]">{d.day}</td>
              <td className="py-2 text-right">{d.requests}</td>
              <td className="py-2 text-right">
                {d.tokens_in}/{d.tokens_out}
              </td>
              <td className="py-2 text-right">
                ${(d.cost_micro_usd / 1_000_000).toFixed(4)}
                {d.cost_unknown_count > 0 && (
                  <span className="text-[var(--accent)]">
                    {" "}
                    +{d.cost_unknown_count}?
                  </span>
                )}
              </td>
              <td
                className={`py-2 text-right ${
                  d.errors > 0
                    ? "text-[var(--accent)]"
                    : "text-[var(--text-dim)]"
                }`}
              >
                {d.errors}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {days.length === 0 && !loading && !error && (
        <p className="py-4 font-mono-nums text-xs text-[var(--text-dim)]">
          belum ada data
        </p>
      )}
      {error && <p className="py-4 text-sm text-[var(--accent)]">{error}</p>}
    </div>
  );
}

// KeysPage: kelola virtual key (DESIGN.md §Halaman #4). List dari
// GET /admin/keys, buat lewat POST (token plaintext cuma balik sekali —
// ditahan di kotak sampai ditutup manual), nonaktifin lewat DELETE
// (soft delete; klik-dua karena mutus akses konsumen & nggak ada jalan
// balik dari UI). Tanpa animasi — bukan 1 dari 3 titik DESIGN.md.
function KeysPage() {
  const [keys, setKeys] = useState<KeyRow[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const [name, setName] = useState("");
  const [rpm, setRpm] = useState("");
  const [budget, setBudget] = useState("");
  const [logBodies, setLogBodies] = useState(false);
  const [creating, setCreating] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [newToken, setNewToken] = useState<{
    name: string;
    token: string;
  } | null>(null);

  // Baris yang lagi nunggu klik konfirmasi kedua buat dinonaktifin.
  const [confirmingId, setConfirmingId] = useState<number | null>(null);

  async function load() {
    if (!ADMIN_TOKEN) return;
    try {
      const res = await fetch(
        `${GATEWAY_URL}/admin/keys?token=${encodeURIComponent(ADMIN_TOKEN)}`,
      );
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = (await res.json()) as { keys: KeyRow[] };
      setKeys(data.keys);
      setError(null);
    } catch {
      setError("gagal memuat keys");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function create(e: React.FormEvent) {
    e.preventDefault();
    if (!ADMIN_TOKEN || !name.trim()) return;
    setFormError(null);

    const body: Record<string, unknown> = {
      name: name.trim(),
      log_bodies: logBodies,
    };
    if (rpm.trim()) {
      const n = Number(rpm);
      if (!Number.isInteger(n) || n < 1) {
        setFormError("rpm harus bilangan bulat >= 1");
        return;
      }
      body.rpm_limit = n;
    }
    if (budget.trim()) {
      const d = Number(budget);
      if (!Number.isFinite(d) || d < 0) {
        setFormError("budget harus angka >= 0");
        return;
      }
      body.monthly_budget_micro_usd = Math.round(d * 1_000_000);
    }

    setCreating(true);
    try {
      // tanpa header Content-Type → body jadi text/plain → simple request,
      // nggak kena preflight. Handler Go nggak ngecek Content-Type.
      const res = await fetch(
        `${GATEWAY_URL}/admin/keys?token=${encodeURIComponent(ADMIN_TOKEN)}`,
        { method: "POST", body: JSON.stringify(body) },
      );
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as {
          error?: string;
        } | null;
        throw new Error(j?.error ?? `HTTP ${res.status}`);
      }
      const data = (await res.json()) as { name: string; token: string };
      setNewToken({ name: data.name, token: data.token });
      setName("");
      setRpm("");
      setBudget("");
      setLogBodies(false);
      load();
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "gagal bikin key");
    } finally {
      setCreating(false);
    }
  }

  async function deactivate(id: number) {
    if (!ADMIN_TOKEN) return;
    if (confirmingId !== id) {
      setConfirmingId(id);
      return;
    }
    try {
      await fetch(
        `${GATEWAY_URL}/admin/keys/${id}?token=${encodeURIComponent(ADMIN_TOKEN)}`,
        { method: "DELETE" },
      );
    } catch {
      // biarin — load() di bawah bakal nunjukin state sebenernya
    } finally {
      setConfirmingId(null);
      load();
    }
  }

  if (!ADMIN_TOKEN) {
    return (
      <p className="font-mono-nums text-xs text-[var(--text-dim)]">
        VITE_SANMON_ADMIN_TOKEN belum diset
      </p>
    );
  }

  const inputCls =
    "border border-[var(--border)] bg-[var(--panel)] px-2 py-1 text-[var(--text)] outline-none placeholder:text-[var(--text-dim)] focus:border-[var(--accent)]";

  return (
    <div>
      {newToken && (
        <div className="mb-4 border border-[var(--accent)] bg-[var(--panel)] p-4">
          <div className="mb-2 flex items-baseline justify-between">
            <span className="text-xs tracking-widest text-[var(--accent)] uppercase">
              token buat “{newToken.name}”
            </span>
            <button
              onClick={() => setNewToken(null)}
              className="text-xs text-[var(--text-dim)] hover:text-[var(--text)]"
            >
              tutup
            </button>
          </div>
          <code className="block font-mono-nums text-sm break-all text-[var(--text)]">
            {newToken.token}
          </code>
          <p className="mt-2 font-mono-nums text-xs text-[var(--text-dim)]">
            simpan sekarang — nggak akan muncul lagi.
          </p>
        </div>
      )}

      <form
        onSubmit={create}
        className="mb-6 flex flex-wrap items-center gap-3 font-mono-nums text-xs"
      >
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="nama key"
          required
          className={inputCls}
        />
        <input
          value={rpm}
          onChange={(e) => setRpm(e.target.value)}
          placeholder="rpm (opsional)"
          inputMode="numeric"
          className={`w-36 ${inputCls}`}
        />
        <input
          value={budget}
          onChange={(e) => setBudget(e.target.value)}
          placeholder="budget $/bln (opsional)"
          inputMode="decimal"
          className={`w-44 ${inputCls}`}
        />
        <label className="flex items-center gap-1 text-[var(--text-dim)]">
          <input
            type="checkbox"
            checked={logBodies}
            onChange={(e) => setLogBodies(e.target.checked)}
          />
          rekam body
        </label>
        <button
          type="submit"
          disabled={creating || !name.trim()}
          className="border border-[var(--border)] px-3 py-1 text-[var(--text-dim)] hover:border-[var(--accent)] hover:text-[var(--text)] disabled:opacity-50"
        >
          {creating ? "membuat..." : "buat key"}
        </button>
        {formError && <span className="text-[var(--accent)]">{formError}</span>}
      </form>

      <table className="w-full border-collapse text-sm">
        <thead>
          <tr className="border-b border-[var(--border)] text-left text-[var(--text-dim)]">
            <th className="py-2 font-normal">id</th>
            <th className="py-2 font-normal">nama</th>
            <th className="py-2 font-normal text-right">rpm</th>
            <th className="py-2 font-normal text-right">budget/bln</th>
            <th className="py-2 font-normal text-right">body</th>
            <th className="py-2 font-normal text-right">dibuat</th>
            <th className="py-2 font-normal text-right">aksi</th>
          </tr>
        </thead>
        <tbody className="font-mono-nums">
          {keys.map((k) => (
            <tr
              key={k.id}
              className={`border-b border-[var(--border)] ${
                k.disabled ? "opacity-40" : ""
              }`}
            >
              <td className="py-2 text-[var(--text-dim)]">{k.id}</td>
              <td className="py-2">{k.name}</td>
              <td className="py-2 text-right">{k.rpm_limit ?? "-"}</td>
              <td className="py-2 text-right">
                {formatBudget(k.monthly_budget_micro_usd)}
              </td>
              <td className="py-2 text-right">{k.log_bodies ? "ya" : "-"}</td>
              <td className="py-2 text-right text-[var(--text-dim)]">
                {new Date(k.created_at).toLocaleDateString("id-ID")}
              </td>
              <td className="py-2 text-right">
                {k.disabled ? (
                  <span className="text-[var(--text-dim)]">nonaktif</span>
                ) : (
                  <button
                    onClick={() => deactivate(k.id)}
                    onBlur={() =>
                      setConfirmingId((cur) => (cur === k.id ? null : cur))
                    }
                    className="text-xs text-[var(--text-dim)] hover:text-[var(--accent)]"
                  >
                    {confirmingId === k.id ? "yakin?" : "nonaktifkan"}
                  </button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {keys.length === 0 && !loading && !error && (
        <p className="py-4 font-mono-nums text-xs text-[var(--text-dim)]">
          belum ada key
        </p>
      )}
      {error && <p className="py-4 text-sm text-[var(--accent)]">{error}</p>}
    </div>
  );
}

function App() {
  const [view, setView] = useState<"live" | "history" | "daily" | "keys">(
    "live",
  );
  const [events, setEvents] = useState<LiveEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  // Tanggal yang dibawa dari klik baris di Harian ke Riwayat. Tombol "riwayat"
  // di header nge-reset ini biar nggak nyangkut ngefilter diam-diam.
  const [histDates, setHistDates] = useState({ since: "", until: "" });
  const nextId = useRef(0);
  const animatedIds = useRef<Set<number>>(new Set());

  function openHistory(since: string, until: string) {
    setHistDates({ since, until });
    setView("history");
  }

  useEffect(() => {
    if (!ADMIN_TOKEN) return;

    const source = new EventSource(
      `${GATEWAY_URL}/admin/stream?token=${encodeURIComponent(ADMIN_TOKEN)}`,
    );
    source.onopen = () => setConnected(true);
    source.onerror = () => setConnected(false);
    source.onmessage = (msg) => {
      const ev = JSON.parse(msg.data) as Omit<LiveEvent, "id">;
      const id = nextId.current++;
      setEvents((prev) => [{ ...ev, id }, ...prev.slice(0, MAX_ROWS - 1)]);
    };

    return () => source.close();
  }, []);

  // Titik 3: baris -> panel detail, morph pakai View Transitions API kalau
  // didukung browser; kalau nggak, ganti state polos tanpa transisi. Browser
  // boleh skip/abort transisi kapan aja (tab pindah fokus, transisi baru
  // nyusul duluan, dll) — itu perilaku normal, bukan error, jadi promise-nya
  // wajib di-catch biar gak jadi unhandled rejection di console.
  function withViewTransition(update: () => void) {
    if (typeof document.startViewTransition !== "function") {
      update();
      return;
    }
    const transition = document.startViewTransition(() => flushSync(update));
    transition.ready.catch(() => {});
    transition.finished.catch(() => {});
  }

  function selectRow(id: number) {
    withViewTransition(() => setSelectedId(id));
  }

  function closeDetail() {
    withViewTransition(() => setSelectedId(null));
  }

  const selected = events.find((ev) => ev.id === selectedId) ?? null;

  return (
    <div className="min-h-svh flex flex-col">
      <header className="border-b border-[var(--border)] px-6 py-4 flex items-baseline justify-between">
        <h1 className="text-sm tracking-widest text-[var(--text-dim)] uppercase">
          Sanmon <span className="text-[var(--accent)]">/</span>{" "}
          <button
            onClick={() => setView("live")}
            className={
              view === "live"
                ? "text-[var(--accent)]"
                : "hover:text-[var(--text)]"
            }
          >
            live feed
          </button>{" "}
          <button
            onClick={() => openHistory("", "")}
            className={
              view === "history"
                ? "text-[var(--accent)]"
                : "hover:text-[var(--text)]"
            }
          >
            riwayat
          </button>{" "}
          <button
            onClick={() => setView("daily")}
            className={
              view === "daily"
                ? "text-[var(--accent)]"
                : "hover:text-[var(--text)]"
            }
          >
            harian
          </button>{" "}
          <button
            onClick={() => setView("keys")}
            className={
              view === "keys"
                ? "text-[var(--accent)]"
                : "hover:text-[var(--text)]"
            }
          >
            keys
          </button>
        </h1>
        {view === "live" && (
          <span className="font-mono-nums text-xs text-[var(--text-dim)]">
            {!ADMIN_TOKEN
              ? "VITE_SANMON_ADMIN_TOKEN belum diset"
              : connected
                ? "live"
                : "menyambung..."}
          </span>
        )}
      </header>

      <main className="flex-1 px-6 py-4">
        {view === "keys" ? (
          <KeysPage />
        ) : view === "daily" ? (
          <DailyPage onPickDay={(day) => openHistory(day, day)} />
        ) : view === "history" ? (
          <HistoryPage
            key={`${histDates.since}|${histDates.until}`}
            initialSince={histDates.since}
            initialUntil={histDates.until}
          />
        ) : selected ? (
          <DetailPanel ev={selected} onClose={closeDetail} />
        ) : (
          <table className="w-full border-collapse text-sm">
            <thead>
              <tr className="text-left text-[var(--text-dim)] border-b border-[var(--border)]">
                <th className="py-2 font-normal">waktu</th>
                <th className="py-2 font-normal">model</th>
                <th className="py-2 font-normal text-right">stream</th>
                <th className="py-2 font-normal text-right">latency</th>
                <th className="py-2 font-normal text-right">status</th>
              </tr>
            </thead>
            <tbody className="font-mono-nums">
              {events.map((ev) => (
                <Row
                  key={ev.id}
                  ev={ev}
                  animatedIds={animatedIds}
                  onSelect={() => selectRow(ev.id)}
                />
              ))}
            </tbody>
          </table>
        )}
      </main>
    </div>
  );
}

export default App;
