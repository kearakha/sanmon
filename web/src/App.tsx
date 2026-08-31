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

const GATEWAY_URL = "http://localhost:8777";
const ADMIN_TOKEN = import.meta.env.VITE_SANMON_ADMIN_TOKEN as
  string | undefined;
const MAX_ROWS = 50;

function statusLabel(ev: LiveEvent): "OK" | "ERR" | "PARTIAL" {
  if (ev.partial) return "PARTIAL";
  if (ev.error || ev.status_code >= 400) return "ERR";
  return "OK";
}

function formatTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleTimeString("id-ID", { hour12: false });
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

function App() {
  const [events, setEvents] = useState<LiveEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const nextId = useRef(0);
  const animatedIds = useRef<Set<number>>(new Set());

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
          Sanmon <span className="text-[var(--accent)]">/</span> live feed
        </h1>
        <span className="font-mono-nums text-xs text-[var(--text-dim)]">
          {!ADMIN_TOKEN
            ? "VITE_SANMON_ADMIN_TOKEN belum diset"
            : connected
              ? "live"
              : "menyambung..."}
        </span>
      </header>

      <main className="flex-1 px-6 py-4">
        {selected ? (
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
