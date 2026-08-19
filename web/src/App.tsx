type FakeRequest = {
  id: number;
  time: string;
  model: string;
  tokensIn: number;
  tokensOut: number;
  costMicroUsd: number;
  latencyMs: number;
  status: "ok" | "error" | "partial";
};

const FAKE_REQUESTS: FakeRequest[] = [
  {
    id: 1042,
    time: "07:41:12",
    model: "gemini-flash",
    tokensIn: 812,
    tokensOut: 204,
    costMicroUsd: 143200,
    latencyMs: 891,
    status: "ok",
  },
  {
    id: 1041,
    time: "07:40:58",
    model: "gemini-flash",
    tokensIn: 340,
    tokensOut: 88,
    costMicroUsd: 61900,
    latencyMs: 412,
    status: "ok",
  },
  {
    id: 1040,
    time: "07:40:31",
    model: "gemini-pro",
    tokensIn: 2103,
    tokensOut: 512,
    costMicroUsd: 981000,
    latencyMs: 2310,
    status: "partial",
  },
  {
    id: 1039,
    time: "07:39:47",
    model: "gemini-flash",
    tokensIn: 90,
    tokensOut: 0,
    costMicroUsd: 0,
    latencyMs: 120,
    status: "error",
  },
  {
    id: 1038,
    time: "07:39:02",
    model: "gemini-flash",
    tokensIn: 1204,
    tokensOut: 340,
    costMicroUsd: 211400,
    latencyMs: 1042,
    status: "ok",
  },
];

function formatCost(microUsd: number): string {
  return `$${(microUsd / 1_000_000).toFixed(4)}`;
}

function statusLabel(status: FakeRequest["status"]): string {
  if (status === "ok") return "OK";
  if (status === "error") return "ERR";
  return "PARTIAL";
}

function App() {
  return (
    <div className="min-h-svh flex flex-col">
      <header className="border-b border-[var(--border)] px-6 py-4 flex items-baseline justify-between">
        <h1 className="text-sm tracking-widest text-[var(--text-dim)] uppercase">
          Sanmon <span className="text-[var(--accent)]">/</span> live feed
        </h1>
        <span className="font-mono-nums text-xs text-[var(--text-dim)]">
          data palsu — M0
        </span>
      </header>

      <main className="flex-1 px-6 py-4">
        <table className="w-full border-collapse text-sm">
          <thead>
            <tr className="text-left text-[var(--text-dim)] border-b border-[var(--border)]">
              <th className="py-2 font-normal">waktu</th>
              <th className="py-2 font-normal">model</th>
              <th className="py-2 font-normal text-right">token in</th>
              <th className="py-2 font-normal text-right">token out</th>
              <th className="py-2 font-normal text-right">biaya</th>
              <th className="py-2 font-normal text-right">latency</th>
              <th className="py-2 font-normal text-right">status</th>
            </tr>
          </thead>
          <tbody className="font-mono-nums">
            {FAKE_REQUESTS.map((req) => (
              <tr key={req.id} className="border-b border-[var(--border)]">
                <td className="py-2 text-[var(--text-dim)]">{req.time}</td>
                <td className="py-2">{req.model}</td>
                <td className="py-2 text-right">{req.tokensIn}</td>
                <td className="py-2 text-right">{req.tokensOut}</td>
                <td className="py-2 text-right">
                  {formatCost(req.costMicroUsd)}
                </td>
                <td className="py-2 text-right">{req.latencyMs}ms</td>
                <td
                  className={`py-2 text-right ${
                    req.status === "ok"
                      ? "text-[var(--text-dim)]"
                      : "text-[var(--accent)]"
                  }`}
                >
                  {statusLabel(req.status)}
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
