import { useEffect, useRef, useState } from "react";

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

function App() {
  const [events, setEvents] = useState<LiveEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const nextId = useRef(0);

  useEffect(() => {
    if (!ADMIN_TOKEN) return;

    const source = new EventSource(
      `${GATEWAY_URL}/admin/stream?token=${encodeURIComponent(ADMIN_TOKEN)}`,
    );
    source.onopen = () => setConnected(true);
    source.onerror = () => setConnected(false);
    source.onmessage = (msg) => {
      const ev = JSON.parse(msg.data) as Omit<LiveEvent, "id">;
      setEvents((prev) => [
        { ...ev, id: nextId.current++ },
        ...prev.slice(0, MAX_ROWS - 1),
      ]);
    };

    return () => source.close();
  }, []);

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
              <tr key={ev.id} className="border-b border-[var(--border)]">
                <td className="py-2 text-[var(--text-dim)]">
                  {formatTime(ev.time)}
                </td>
                <td className="py-2">{ev.model}</td>
                <td className="py-2 text-right">{ev.stream ? "ya" : "-"}</td>
                <td className="py-2 text-right">{ev.latency_ms}ms</td>
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
            ))}
          </tbody>
        </table>
      </main>
    </div>
  );
}

export default App;
