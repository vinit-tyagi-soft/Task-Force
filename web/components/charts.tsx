import { fmtDate, statusLabel, weekday } from "@/lib/format";
import type { Status } from "@/lib/types";

export function LineChart({ points, asOf }: { points: { date: string; planned: number; completed: number }[]; asOf: string }) {
  const max = Math.max(4, ...points.map((p) => p.planned), ...points.map((p) => p.completed));
  const x = (i: number) => 30 + i * 55;
  const y = (n: number) => 150 - (n / max) * 115;
  const path = (ns: number[]) => ns.map((n, i) => `${i ? "L" : "M"}${x(i)},${y(n)}`).join(" ");
  const done = points.map((p) => p.completed);
  const planned = points.map((p) => p.planned);
  return (
    <>
      <svg className="chart-svg" viewBox="0 0 390 185" role="img" aria-label="Completed tasks and deadlines across seven days">
        {[0, 1, 2, 3, 4].map((n) => (
          <g key={n}>
            <line className="gridline" x1="28" x2="366" y1={y((n * max) / 4)} y2={y((n * max) / 4)} />
            <text x="5" y={y((n * max) / 4) + 3}>{Math.round((n * max) / 4)}</text>
          </g>
        ))}
        <path d={`${path(done)} L360,150 L30,150 Z`} fill="var(--primary)" opacity=".06" />
        <path d={path(planned)} fill="none" stroke="#cec7e4" strokeWidth="2" strokeDasharray="4 4" />
        <path d={path(done)} fill="none" stroke="var(--primary)" strokeWidth="2.2" />
        {done.map((n, i) => (
          <circle key={i} cx={x(i)} cy={y(n)} r="3" fill="var(--surface)" stroke="var(--primary)" strokeWidth="2" />
        ))}
        {points.map((p, i) => (
          <text key={p.date} x={x(i) - 14} y="177">{weekday(p.date)}</text>
        ))}
      </svg>
      <div className="legend">
        <span><i className="legend-dot" style={{ background: "var(--primary)" }} />Completed</span>
        <span><i className="legend-dot" style={{ background: "#cec7e4" }} />Task deadlines</span>
        <span className="spacer" />
        <span>7 days ending {fmtDate(asOf)}</span>
      </div>
    </>
  );
}

const donutParts: [Status, string][] = [
  ["DONE", "var(--primary)"],
  ["IN_PROGRESS", "#b7a8de"],
  ["IN_REVIEW", "#d8cee9"],
  ["TODO", "#eee9f6"],
  ["BLOCKED", "#e5b2bc"],
];

export function Donut({ statuses }: { statuses: Status[] }) {
  const eligible = statuses.filter((s) => s !== "CANCELLED");
  const total = eligible.length;
  const circ = 251.33;
  const count = (s: Status) => eligible.filter((x) => x === s).length;
  const segs = donutParts.map(([s, color]) => ({ s, color, seg: total ? (count(s) / total) * circ : 0 }));
  const offsets = segs.map((_, i) => segs.slice(0, i).reduce((sum, x) => sum + x.seg, 0));
  return (
    <>
      <div className="donut-wrap">
        <svg className="donut" viewBox="0 0 100 100" role="img" aria-label="Task status distribution">
          <circle cx="50" cy="50" r="40" stroke="var(--line)" />
          {segs.map(({ s, color, seg }, i) => (
            <circle key={s} cx="50" cy="50" r="40" stroke={color} strokeDasharray={`${seg} ${circ - seg}`} strokeDashoffset={-offsets[i]} />
          ))}
        </svg>
        <div className="donut-center">
          <strong>{total}</strong>
          <span>Total tasks</span>
        </div>
      </div>
      {donutParts
        .filter(([s]) => count(s) > 0)
        .map(([s, color]) => (
          <div className="status-row" key={s}>
            <i className="legend-dot" style={{ background: color }} />
            {statusLabel[s]}
            <span className="spacer" />
            {count(s)}
          </div>
        ))}
    </>
  );
}
